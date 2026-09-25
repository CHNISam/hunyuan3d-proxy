/**
 * Hunyuan3D login worker — Playwright session manager.
 * Exposes HTTP API for the Go proxy:
 *   GET  /health
 *   POST /session/start   {provider:"wechat"|"qq"}
 *   GET  /session/:id     status + qr image url + authorize url
 *   GET  /session/:id/qr  PNG bytes of official WeChat QR
 *   POST /session/:id/export  → cookies array when logged in
 *   POST /session/:id/close
 *
 * Official WeChat embed (verified via Playwright):
 *   redirect_uri = https://3d.hunyuan.tencent.com/scan?nonce=<nonce>
 *   state=wechat_login login_type=jssdk self_redirect=false
 *   QR image = https://open.weixin.qq.com/connect/qrcode/<uuid>
 */
const http = require("http");
const fs = require("fs");
const path = require("path");
const { chromium } = require("playwright-core");

const EXEC =
  process.env.PLAYWRIGHT_CHROMIUM ||
  "C:/Users/Administrator/AppData/Local/ms-playwright/chromium-1243/chrome-win64/chrome.exe";
const PORT = Number(process.env.LOGIN_WORKER_PORT || 8793);
const DATA_DIR = process.env.DATA_DIR || "D:/lose/hunyuan3d-proxy/data";
const WX_APPID = "wx01cc05677df34280";
const QQ_APPID = "102788748";
const UPSTREAM = "https://3d.hunyuan.tencent.com";

const sessions = new Map();
let browser = null;

function nonce(n = 16) {
  const s = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let o = "";
  for (let i = 0; i < n; i++) o += s[(Math.random() * s.length) | 0];
  return o;
}

async function getBrowser() {
  if (browser && browser.isConnected()) return browser;
  browser = await chromium.launch({
    headless: true,
    executablePath: EXEC,
    args: ["--disable-blink-features=AutomationControlled"],
  });
  return browser;
}

function wxAuthorizeUrl(n, state) {
  const redirect = `${UPSTREAM}/scan?nonce=${encodeURIComponent(n)}`;
  // Match the live WxLogin iframe parameters discovered on the official login page.
  return (
    "https://open.weixin.qq.com/connect/qrconnect?appid=" +
    WX_APPID +
    "&scope=snsapi_login&redirect_uri=" +
    encodeURIComponent(redirect) +
    "&state=" +
    encodeURIComponent(state || "wechat_login") +
    "&login_type=jssdk&self_redirect=false"
  );
}

function qqAuthorizeUrl(n) {
  // Keep official-site callback (QQ whitelist). nonce is included as SDK does.
  const redirect = `${UPSTREAM}/login`;
  return (
    "https://graph.qq.com/oauth2.0/authorize?response_type=code&client_id=" +
    QQ_APPID +
    "&redirect_uri=" +
    encodeURIComponent(redirect) +
    "&state=qq_login&nonce=" +
    encodeURIComponent(n)
  );
}

async function extractQrFromPage(page) {
  // Prefer official QR image from WeChat domain.
  const imgs = await page.evaluate(() =>
    Array.from(document.images).map((i) => ({
      src: i.src,
      w: i.naturalWidth,
      h: i.naturalHeight,
      cls: i.className || "",
    }))
  );
  const qr = imgs.find((i) => /open\.weixin\.qq\.com\/connect\/qrcode\//.test(i.src) && i.w >= 100);
  if (qr) return { qrImageUrl: qr.src, qrWidth: qr.w, qrHeight: qr.h, source: "wechat-cdn" };
  const any = imgs.find((i) => i.w >= 100 && i.h >= 100 && !/avatar|svg/i.test(i.src));
  return any ? { qrImageUrl: any.src, qrWidth: any.w, qrHeight: any.h, source: "img" } : null;
}

async function startSession(provider) {
  const id = nonce(20);
  const n = nonce(12);
  const b = await getBrowser();
  const context = await b.newContext({
    locale: "zh-CN",
    viewport: { width: 900, height: 700 },
    userAgent:
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
  });
  const page = await context.newPage();
  const sess = {
    id,
    provider: provider || "wechat",
    nonce: n,
    createdAt: Date.now(),
    status: "pending",
    qrImageUrl: null,
    authorizeUrl: null,
    context,
    page,
    cookies: null,
    message: "",
  };
  sessions.set(id, sess);

  try {
    if (sess.provider === "qq") {
      sess.authorizeUrl = qqAuthorizeUrl(n);
      await page.goto(sess.authorizeUrl, { waitUntil: "domcontentloaded", timeout: 45000 });
      await page.waitForTimeout(4000);
      await page.screenshot({ path: path.join(DATA_DIR, "qr-" + id + "-qq.png") }).catch(() => {});
      const text = await page.evaluate(() => document.body.innerText.slice(0, 800));
      sess.message = text.split("\n").slice(0, 4).join(" | ");
      if (/error|illegal|redirect/i.test(text)) {
        sess.status = "error";
        sess.message = "QQ redirect_uri may be rejected: " + sess.message;
      } else {
        sess.status = "waiting_scan";
      }
      // try extract any QR-ish image
      const info = await extractQrFromPage(page);
      if (info) Object.assign(sess, info);
    } else {
      // Open official login page and let the site build the legal WxLogin iframe.
      await page.goto(UPSTREAM + "/login", { waitUntil: "domcontentloaded", timeout: 45000 });
      await page.waitForTimeout(3500);
      try {
        await page.getByText("登录", { exact: true }).first().click({ timeout: 8000 });
      } catch (_) {}
      await page.waitForTimeout(4500);

      // Discover iframe authorize URL (official whitelist redirect).
      const iframes = await page.evaluate(() =>
        Array.from(document.querySelectorAll("iframe")).map((f) => f.src).filter(Boolean)
      );
      const wxIframe = iframes.find((s) => s.includes("open.weixin.qq.com/connect/qrconnect"));
      if (wxIframe) {
        sess.authorizeUrl = wxIframe;
        const m = wxIframe.match(/redirect_uri=([^&]+)/);
        if (m) sess.redirectUri = decodeURIComponent(m[1]);
        const nm = (sess.redirectUri || "").match(/nonce=([^&]+)/);
        if (nm) sess.nonce = decodeURIComponent(nm[1]);
      } else {
        sess.authorizeUrl = wxAuthorizeUrl(n);
        sess.redirectUri = `${UPSTREAM}/scan?nonce=${n}`;
        // fallback open authorize directly
        const p2 = await context.newPage();
        sess.wxPage = p2;
        await p2.goto(sess.authorizeUrl, { waitUntil: "domcontentloaded", timeout: 45000 });
        await p2.waitForTimeout(4000);
      }

      // Prefer QR from the WeChat iframe page if present
      let qrInfo = null;
      for (const fr of page.frames()) {
        if (!/open\.weixin\.qq\.com/.test(fr.url())) continue;
        try {
          const imgs = await fr.evaluate(() =>
            Array.from(document.images).map((i) => ({
              src: i.src,
              w: i.naturalWidth,
              h: i.naturalHeight,
              cls: i.className || "",
            }))
          );
          const hit = imgs.find((i) => /connect\/qrcode\//.test(i.src));
          if (hit) {
            qrInfo = { qrImageUrl: hit.src, qrWidth: hit.w, qrHeight: hit.h, source: "iframe-wechat" };
            break;
          }
        } catch (_) {}
      }
      if (!qrInfo && sess.wxPage) qrInfo = await extractQrFromPage(sess.wxPage);
      if (!qrInfo) qrInfo = await extractQrFromPage(page);
      if (qrInfo) Object.assign(sess, qrInfo);

      // Also save viewport screenshots for debugging/UI
      await page.screenshot({ path: path.join(DATA_DIR, "qr-" + id + "-login.png") }).catch(() => {});
      sess.status = "waiting_scan";
      sess.message = "official WxLogin session ready; scan the QR image";
    }
  } catch (e) {
    sess.status = "error";
    sess.message = e.message;
  }

  return sessPublic(sess);
}

function sessPublic(s) {
  return {
    id: s.id,
    provider: s.provider,
    status: s.status,
    message: s.message,
    nonce: s.nonce,
    authorizeUrl: s.authorizeUrl,
    redirectUri: s.redirectUri || null,
    qrImageUrl: s.qrImageUrl,
    qrProxyUrl: s.qrImageUrl ? `/api/login/qr/image?session=${s.id}` : null,
    hasCookies: !!(s.cookies && s.cookies.length),
    cookieNames: s.cookies ? s.cookies.map((c) => c.name) : [],
  };
}

async function refreshSession(id) {
  const s = sessions.get(id);
  if (!s) return null;
  try {
    const pages = [s.page, s.wxPage].filter(Boolean);
    for (const p of pages) {
      const url = p.url();
      if (/hunyuan\.tencent\.com\/(scan|login|studio)/.test(url) || /[?&]code=/.test(url)) {
        s.status = "redirected";
        s.message = "redirected to " + url;
      }
      const cookies = await s.context.cookies();
      const names = cookies.map((c) => c.name);
      const useful = cookies.filter((c) =>
        /hy_|skey|p_skey|uin|p_uin|openid|token|login|session|ptcz|qq/i.test(c.name)
      );
      if (useful.length || names.length > 2) {
        s.cookies = cookies;
        if (useful.length || /scan/.test(url)) {
          s.status = "success";
          s.message = "cookies captured (" + names.join(",") + ")";
        }
      }
    }
  } catch (e) {
    s.message = e.message;
  }
  return sessPublic(s);
}

async function exportSession(id) {
  const s = sessions.get(id);
  if (!s) return { ok: false, error: "no session" };
  await refreshSession(id);
  if (!s.cookies || !s.cookies.length) {
    return { ok: false, status: s.status, message: s.message || "not logged in yet" };
  }
  const jar = [];
  for (const c of s.cookies) {
    jar.push({
      name: c.name,
      value: c.value,
      domain: c.domain,
      path: c.path || "/",
      secure: !!c.secure,
      httpOnly: !!c.httpOnly,
    });
  }
  // write a sidecar file the Go proxy can also read
  try {
    fs.mkdirSync(DATA_DIR, { recursive: true });
    fs.writeFileSync(
      path.join(DATA_DIR, "playwright-session-" + id + ".json"),
      JSON.stringify({ id, provider: s.provider, cookies: jar, at: new Date().toISOString() }, null, 2)
    );
  } catch (_) {}
  return { ok: true, cookies: jar, provider: s.provider, count: jar.length };
}

async function closeSession(id) {
  const s = sessions.get(id);
  if (!s) return { ok: true };
  try {
    if (s.page) await s.page.close();
    if (s.wxPage) await s.wxPage.close();
    if (s.context) await s.context.close();
  } catch (_) {}
  sessions.delete(id);
  return { ok: true };
}

async function qrPng(id) {
  const s = sessions.get(id);
  if (!s) return null;
  if (s.qrImageUrl) {
    try {
      const res = await fetch(s.qrImageUrl);
      if (res.ok) {
        const buf = Buffer.from(await res.arrayBuffer());
        return { buf, type: res.headers.get("content-type") || "image/png" };
      }
    } catch (_) {}
  }
  // fallback screenshot
  try {
    const p = s.wxPage || s.page;
    if (p) {
      const buf = await p.screenshot({ type: "png" });
      return { buf, type: "image/png" };
    }
  } catch (_) {}
  return null;
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, "http://127.0.0.1");
  const p = url.pathname;
  const send = (code, obj) => {
    res.writeHead(code, { "Content-Type": "application/json; charset=utf-8", "Access-Control-Allow-Origin": "*" });
    res.end(JSON.stringify(obj));
  };

  if (p === "/health") {
    return send(200, { ok: true, sessions: sessions.size, browser: !!(browser && browser.isConnected()) });
  }
  if (p === "/session/start" && req.method === "POST") {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", async () => {
      let provider = "wechat";
      try {
        const j = JSON.parse(body || "{}");
        if (j.provider) provider = j.provider;
      } catch (_) {}
      try {
        const pub = await startSession(provider);
        send(200, { ok: true, data: pub });
      } catch (e) {
        send(500, { ok: false, error: e.message });
      }
    });
    return;
  }
  let m;
  if ((m = p.match(/^\/session\/([^/]+)\/qr$/))) {
    const out = await qrPng(m[1]);
    if (!out) return send(404, { ok: false, error: "no qr" });
    res.writeHead(200, { "Content-Type": out.type, "Access-Control-Allow-Origin": "*" });
    return res.end(out.buf);
  }
  if ((m = p.match(/^\/session\/([^/]+)\/export$/)) && req.method === "POST") {
    return send(200, await exportSession(m[1]));
  }
  if ((m = p.match(/^\/session\/([^/]+)\/close$/)) && req.method === "POST") {
    return send(200, await closeSession(m[1]));
  }
  if ((m = p.match(/^\/session\/([^/]+)$/))) {
    const pub = await refreshSession(m[1]);
    if (!pub) return send(404, { ok: false, error: "no session" });
    return send(200, { ok: true, data: pub });
  }
  send(404, { ok: false, error: "not found" });
});

server.listen(PORT, "127.0.0.1", () => {
  console.log("hy-login-worker listening on http://127.0.0.1:" + PORT);
});
