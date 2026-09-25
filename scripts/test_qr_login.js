const { chromium } = require("playwright-core");
const fs = require("fs");

const OUT_DIR = "D:/lose/output/playwright/login-test";
fs.mkdirSync(OUT_DIR, { recursive: true });

const QR_URL =
  "https://open.weixin.qq.com/connect/qrconnect?appid=wx01cc05677df34280&redirect_uri=https%3A%2F%2F3d.hunyuan.tencent.com%2Flogin&response_type=code&scope=snsapi_login&state=kHcTpWV2gCGfbq8jvI9ADAdr#wechat_redirect";
const LOGIN_URL = "https://3d.hunyuan.tencent.com/login";
const PROXY = "http://127.0.0.1:8790";

(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    userAgent:
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    viewport: { width: 1280, height: 900 },
  });
  const page = await context.newPage();
  const captured = [];
  const interesting = new RegExp(
    "login|oauth|qrcode|qrconnect|jointappid|auth|getuserinfo|code|hy_user|hy_token|openauth",
    "i"
  );

  page.on("request", (req) => {
    const url = req.url();
    if (interesting.test(url)) {
      captured.push({
        type: "req",
        method: req.method(),
        url,
        headers: req.headers(),
        postData: req.postData() ? req.postData().slice(0, 800) : null,
      });
    }
  });
  page.on("response", async (res) => {
    const url = res.url();
    if (interesting.test(url)) {
      let bodyPreview = null;
      try {
        const ct = res.headers()["content-type"] || "";
        if (ct.includes("json") || ct.includes("text") || ct.includes("html")) {
          bodyPreview = (await res.text()).slice(0, 800);
        }
      } catch (_) {}
      captured.push({
        type: "res",
        status: res.status(),
        url,
        setCookie: res.headers()["set-cookie"],
        bodyPreview,
      });
    }
  });

  console.log("OPEN WeChat QR page");
  try {
    await page.goto(QR_URL, { waitUntil: "domcontentloaded", timeout: 40000 });
  } catch (e) {
    console.log("qr goto warn", e.message);
  }
  await page.waitForTimeout(4000);
  await page.screenshot({ path: OUT_DIR + "/01-wechat-qr.png", fullPage: true });
  const qrText = await page.evaluate(() => document.body.innerText.slice(0, 2000));
  const qrHTML = await page.content();
  fs.writeFileSync(OUT_DIR + "/01-wechat-qr.txt", qrText);
  fs.writeFileSync(OUT_DIR + "/01-wechat-qr.html", qrHTML.slice(0, 200000));
  console.log("QR page text:\n", qrText);

  console.log("OPEN Hunyuan login page");
  try {
    await page.goto(LOGIN_URL, { waitUntil: "domcontentloaded", timeout: 40000 });
  } catch (e) {
    console.log("login goto warn", e.message);
  }
  await page.waitForTimeout(6000);
  await page.screenshot({ path: OUT_DIR + "/02-hunyuan-login.png", fullPage: true });
  const loginText = await page.evaluate(() => document.body.innerText.slice(0, 2500));
  const loginHTML = await page.content();
  fs.writeFileSync(OUT_DIR + "/02-hunyuan-login.txt", loginText);
  fs.writeFileSync(OUT_DIR + "/02-hunyuan-login.html", loginHTML.slice(0, 250000));
  console.log("Login page text:\n", loginText);

  // cookies after visiting login page
  const cookies = await context.cookies();
  fs.writeFileSync(OUT_DIR + "/browser-cookies.json", JSON.stringify(cookies, null, 2));
  console.log("browser cookies", cookies.map((c) => c.name + "@" + c.domain).join(", "));

  fs.writeFileSync(OUT_DIR + "/network.json", JSON.stringify(captured, null, 2));
  console.log("captured", captured.length);

  await browser.close();
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
