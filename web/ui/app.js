(() => {
  const $ = (id) => document.getElementById(id);
  const baseInput = $("baseUrl");

  function publicOrigin() {
    if (
      window.location &&
      window.location.origin &&
      window.location.origin.startsWith("http") &&
      window.location.origin !== "null"
    ) {
      return window.location.origin.replace(/\/$/, "");
    }
    return "http://127.0.0.1:8811";
  }

  function base() {
    const v = (baseInput.value || "").trim();
    return v ? v.replace(/\/$/, "") : publicOrigin();
  }
  baseInput.value = publicOrigin();

  // ---------- Toast ----------
  function toast(kind, title, msg, ms) {
    const root = $("toastRoot");
    if (!root) return;
    const el = document.createElement("div");
    el.className = "toast " + (kind || "info");
    el.innerHTML =
      `<div><div class="t-title"></div>` +
      (msg ? `<div class="t-msg"></div>` : "") +
      (kind === "loading" ? `<div class="busy-bar"></div>` : "") +
      `</div>`;
    el.querySelector(".t-title").textContent = title || "";
    if (msg) el.querySelector(".t-msg").textContent = String(msg).slice(0, 220);
    root.appendChild(el);
    const life = ms ?? (kind === "loading" ? 0 : kind === "err" ? 6000 : 3200);
    const close = () => {
      el.classList.add("out");
      setTimeout(() => el.remove(), 220);
    };
    if (life > 0) setTimeout(close, life);
    return { close, el };
  }

  async function withToast(title, fn, opts) {
    const loading = toast("loading", opts?.loading || title, opts?.hint || "请求中…");
    try {
      const res = await fn();
      if (loading) loading.close();
      if (res && res.ok === false) {
        const errMsg =
          res.data?.error ||
          res.data?.message ||
          (typeof res.data === "string" ? res.data : "") ||
          `HTTP ${res.status}`;
        toast("err", opts?.fail || title + "失败", errMsg);
      } else if (opts?.success !== false) {
        const extra = opts?.okMsg ? opts.okMsg(res) : opts?.done || "";
        toast("ok", opts?.ok || title + "完成", extra);
      }
      return res;
    } catch (e) {
      if (loading) loading.close();
      toast("err", opts?.fail || title + "异常", e.message);
      throw e;
    }
  }

  // ---------- State ----------
  let qrTicket = "";
  let pollTimer = null;
  let apiScope = "main";
  let assetTimer = null;
  let assetSource = "all";
  let assetStatus = "all";
  let assetOffset = 0;
  const assetLimit = 24;
  const assetHistory = new Map(); // id -> item (local merge, keeps history)
  let assetServerTotal = 0;

  const presets = {
    main: [
      { label: "config", method: "GET", path: "/api/3d/config", body: null },
      { label: "quotainfo", method: "POST", path: "/api/3d/quotainfo", body: {} },
      { label: "planCredit", method: "GET", path: "/api/3d/planCreditInfo", body: null },
      { label: "generations", method: "POST", path: "/api/3d/creations/generations", body: { prompt: "一只小猫" } },
    ],
    concept: [
      { label: "userinfo", method: "GET", path: "/3d/getuserinfo", body: null },
      { label: "remaining", method: "POST", path: "/game3d/general_info/get_remaining_times", body: {} },
      { label: "works", method: "POST", path: "/game3d/general_info/get_works_list", body: {} },
      { label: "text2geo", method: "POST", path: "/game3d/geometry_generation/text2geometry", body: { prompt: "robot" } },
    ],
    cloud: [
      { label: "cloud list", method: "GET", path: "/api/cloud", body: null },
      { label: "submit-pro", method: "POST", path: "/api/cloud/submit-pro", body: { Prompt: "一只小猫", Model: "3.0" } },
    ],
  };

  const ASSET_LABELS = {
    image_url: "预览图",
    pbrImage: "PBR图",
    textureGlb: "贴图GLB",
    glb: "GLB",
    geometryGlb: "几何GLB",
    obj: "OBJ包",
    obj_url: "OBJ",
    mtl: "MTL",
    conceptImage: "概念图",
    singleImage: "输入图",
    "geometryGenerationRsp.glbUrl": "几何GLB",
    "geometryGenerationRsp.fbxUrl": "几何FBX",
    "geometryGenerationRsp.imageUrl": "几何预览",
  };
  const MODEL_KEYS = [
    "textureGlb", "glb", "geometryGlb", "obj", "obj_url",
    "geometryGenerationRsp.glbUrl", "geometryGenerationRsp.fbxUrl",
  ];

  function log(msg, obj) {
    const el = $("logOut");
    if (!el) return;
    const ts = new Date().toLocaleTimeString();
    const line = `[${ts}] ${msg}`;
    const extra = obj !== undefined ? "\n" + JSON.stringify(obj, null, 2) : "";
    el.textContent = (el.textContent === "—" ? "" : el.textContent + "\n") + line + extra;
    el.scrollTop = el.scrollHeight;
  }

  async function api(path, options = {}) {
    const url = base() + path;
    const method = options.method || "GET";
    const headers = Object.assign({ Accept: "application/json" }, options.headers || {});
    if (options.body != null)
      headers["Content-Type"] = headers["Content-Type"] || "application/json";
    log(`${method} ${url}`);
    try {
      const res = await fetch(url, {
        method,
        headers,
        body:
          options.body == null
            ? undefined
            : typeof options.body === "string"
              ? options.body
              : JSON.stringify(options.body),
      });
      const text = await res.text();
      let data;
      try {
        data = JSON.parse(text);
      } catch {
        data = text;
      }
      log(`← ${res.status}`, typeof data === "object" ? data : String(data).slice(0, 400));
      return { ok: res.ok, status: res.status, data };
    } catch (e) {
      log("!! " + e.message);
      return { ok: false, status: 0, data: { error: e.message } };
    }
  }

  function setBadge(ok, text) {
    const b = $("healthBadge");
    b.textContent = text;
    b.className = "badge " + (ok === true ? "ok" : ok === false ? "err" : "");
  }

  function renderHealth(d) {
    $("statStatus").textContent = d.status || "—";
    $("statUpstream").textContent = d.upstream || "—";
    $("statMode").textContent = d.mode || "—";
    $("statCookies").textContent = String(d.cookieCount ?? "—");
    $("statSession").textContent = d.sessionLike == null ? "—" : d.sessionLike ? "是" : "否";
    $("statSignKey").textContent = d.signKey || "—";
  }

  function renderLogin(st) {
    const d = st || {};
    $("lgIn").textContent = d.loggedIn ? "已登录" : "未登录";
    $("lgIn").style.color = d.loggedIn ? "var(--ok)" : "var(--muted)";
    $("lgUser").textContent = d.hyUserPresent ? "有" : "无";
    $("lgToken").textContent = d.hyTokenPresent ? "有" : "无";
    $("lgUid").textContent = d.innerUserId || "—";
  }

  function showQrImage(proxyPath, imageUrl, authorizeUrl) {
    const wrap = $("qrCanvasWrap");
    const canvas = $("qrCanvas");
    const img = $("qrImg");
    const caption = $("qrCaption");
    function fallbackEncode() {
      if (img) {
        img.hidden = true;
        img.removeAttribute("src");
      }
      canvas.style.display = "block";
      const text = authorizeUrl && authorizeUrl !== "—" ? authorizeUrl : "";
      if (!text) {
        wrap.classList.add("empty");
        caption.textContent = "尚无授权链接";
        return;
      }
      const ok = window.HyQR && window.HyQR.draw(canvas, text, { scale: 5, margin: 2 });
      caption.textContent = ok ? "本地编码二维码" : "二维码生成失败，请打开链接";
    }
    const src = proxyPath ? base() + proxyPath : imageUrl;
    if (src && img) {
      canvas.style.display = "none";
      img.hidden = false;
      img.onload = () => {
        wrap.classList.remove("empty");
        caption.textContent = "官方微信二维码（可直接扫）";
      };
      img.onerror = () => {
        if (imageUrl) {
          img.onload = () => {
            caption.textContent = "官方二维码（远程图）";
            wrap.classList.remove("empty");
          };
          img.onerror = fallbackEncode;
          img.src = imageUrl;
        } else fallbackEncode();
      };
      img.src = src;
      return;
    }
    fallbackEncode();
  }

  function renderQR(payload) {
    const d = payload && payload.data ? payload.data : payload || {};
    const inner = d.ticket || d.url || d.qrProxyUrl ? d : d.data || d;
    qrTicket = inner.ticket || inner.id || "";
    $("qrTicket").textContent = qrTicket || "—";
    $("qrProvider").textContent = inner.provider || "—";
    $("qrStatus").textContent = inner.status || "—";
    const redirect = inner.redirectUri || inner.payload?.redirectUri || inner.callback || "—";
    $("qrCallback").textContent = redirect;
    const authorize = inner.url || inner.payload?.authorizeUrl || inner.authorizeUrl || "—";
    const a = $("qrUrl");
    a.textContent = authorize;
    a.href = authorize === "—" ? "#" : authorize;
    const qrProxy =
      inner.qrProxyUrl ||
      inner.payload?.qrProxyUrl ||
      (qrTicket ? "/api/login/qr/image?session=" + encodeURIComponent(qrTicket) : "");
    const qrImage = inner.qrImageUrl || inner.payload?.qrImageUrl || "";
    $("qrMessage").textContent =
      inner.payload?.note || "扫码成功后状态会自动刷新；也可导入 Cookie。";
    if (inner.login) renderLogin(inner.login);
    showQrImage(qrProxy, qrImage, authorize);
  }

  async function refreshHealth(silent) {
    const res = await api("/api/health");
    if (res.ok) {
      renderHealth(res.data.data || res.data);
      setBadge(true, "已连接");
      if (!silent) toast("ok", "连接正常", `mode=${res.data?.data?.mode || "?"} cookies=${res.data?.data?.cookieCount}`);
    } else {
      setBadge(false, "连接失败");
      if (!silent) toast("err", "连接失败", "请检查 Proxy 地址与服务是否启动");
    }
    return res;
  }

  async function refreshLogin(silent) {
    const res = await api("/api/login/status");
    renderLogin(res.data?.data || {});
    return res;
  }

  async function startLogin(provider) {
    return withToast(
      provider === "qq" ? "QQ授权" : "微信扫码",
      async () => {
        const res = await api("/api/login/qr/start?provider=" + provider);
        renderQR(res.data);
        if (res.ok) {
          qrTicket = res.data?.data?.ticket || qrTicket;
          startPolling();
        }
        return res;
      },
      {
        loading: "正在创建扫码会话…",
        hint: "Playwright 打开官方登录页并抓取二维码",
        okMsg: (r) => `ticket=${r.data?.data?.ticket || "?"} · 请扫码`,
      }
    );
  }

  async function doPoll(silent) {
    if (!qrTicket) {
      if (!silent) toast("warn", "尚无 ticket", "请先点「微信扫码」或「QQ授权」");
      return;
    }
    const res = await api("/api/login/qr/status?ticket=" + encodeURIComponent(qrTicket));
    const d = res.data?.data || {};
    $("qrStatus").textContent = d.status || d.worker?.status || "—";
    if (d.message || d.worker?.message) $("qrMessage").textContent = d.message || d.worker?.message;
    if (d.login) renderLogin(d.login);
    if (d.worker?.login) renderLogin(d.worker.login);
    const st = (d.status || d.worker?.status || "").toLowerCase();
    if (st === "success") {
      stopPolling();
      setBadge(true, "登录成功");
      if (!silent) toast("ok", "扫码登录成功", "Cookie 已写入会话");
      await creditAll(true);
      await refreshAssets(true);
    } else if (!silent) {
      toast("info", "登录状态", st || "pending");
    }
    return res;
  }

  function startPolling() {
    stopPolling();
    pollTimer = setInterval(() => doPoll(true), 4000);
  }
  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  async function doImport() {
    const cookies = {};
    const u = $("ckUser").value.trim();
    const t = $("ckToken").value.trim();
    const raw = $("ckRaw").value.trim();
    if (u) cookies.hy_user = u;
    if (t) cookies.hy_token = t;
    const body = { cookies };
    if (raw) body.raw = raw;
    if (!u && !t && !raw) {
      toast("warn", "Cookie 为空", "请填写 hy_user / hy_token 或原始 Cookie 头");
      return;
    }
    return withToast(
      "导入 Cookie",
      async () => {
        const res = await api("/api/login/cookies", { method: "POST", body });
        $("apiOut").textContent = JSON.stringify(res.data, null, 2);
        await refreshLogin(true);
        if (res.ok) {
          const names = res.data?.data?.cookieNames || [];
          return { ...res, _names: names };
        }
        return res;
      },
      { okMsg: (r) => `已导入 ${r._names?.length || Object.keys(cookies).length} 项` }
    );
  }

  function extractCreditSide(res, side) {
    const root = res.data?.data || res.data || res || {};
    if (root.main || root.concept) return root[side] || {};
    if (root.backend === side) return root;
    if (side === "main" && (root.quotainfo || root.planCreditInfo)) return root;
    if (side === "concept" && (root.get_remaining_times || root.getuserinfo)) return root;
    return root;
  }

  function renderCreditMain(res) {
    const d = extractCreditSide(res, "main");
    const q = d.quotainfo || {};
    const p = d.planCreditInfo || {};
    const qBody = q.body || {};
    $("outCreditMain").textContent = JSON.stringify(
      {
        backend: "main",
        independent: true,
        remainQuota: d.remainQuota ?? qBody.remainQuota ?? null,
        totalQuota: d.totalQuota ?? qBody.totalQuota ?? null,
        consumeQuota: qBody.consumeQuota ?? null,
        date: qBody.date ?? null,
        quotainfo: { path: q.path, status: q.status, ok: q.ok, body: q.body, error: q.error },
        planCreditInfo: { path: p.path, status: p.status, ok: p.ok, body: p.body, error: p.error },
      },
      null,
      2
    );
  }

  function renderCreditConcept(res) {
    const d = extractCreditSide(res, "concept");
    const r = d.get_remaining_times || {};
    const u = d.getuserinfo || {};
    $("outCreditConcept").textContent = JSON.stringify(
      {
        backend: "concept",
        independent: true,
        remainingTimes:
          d.remainingTimes ?? r.body?.data?.remainingTimes ?? r.body?.data ?? null,
        get_remaining_times: { path: r.path, status: r.status, ok: r.ok, body: r.body, error: r.error },
        user: {
          nickname: u.body?.nickname || u.body?.userName,
          userId: u.body?.userId,
        },
      },
      null,
      2
    );
  }

  async function creditMain(silent) {
    return withToast(
      "主站积分",
      async () => {
        const res = await api("/api/credit/main");
        renderCreditMain(res);
        return res;
      },
      {
        silent,
        okMsg: (r) => `remainQuota=${r.data?.data?.remainQuota ?? "?"}`,
      }
    );
  }
  async function creditConcept(silent) {
    return withToast(
      "概念站积分",
      async () => {
        const res = await api("/api/credit/concept");
        renderCreditConcept(res);
        return res;
      },
      {
        silent,
        okMsg: (r) =>
          `remainingTimes=${r.data?.data?.get_remaining_times?.body?.data?.remainingTimes ?? "?"}`,
      }
    );
  }
  async function creditAll(silent) {
    return withToast(
      "积分查询",
      async () => {
        const res = await api("/api/credit/all");
        renderCreditMain(res);
        renderCreditConcept(res);
        $("apiOut").textContent = JSON.stringify(res.data, null, 2);
        return res;
      },
      {
        silent,
        okMsg: (r) => {
          const m = r.data?.data?.main?.remainQuota;
          const c = r.data?.data?.concept?.get_remaining_times?.body?.data?.remainingTimes;
          return `main=${m ?? "—"} · concept=${c ?? "—"}`;
        },
      }
    );
  }

  function renderPresets() {
    const row = $("presetRow");
    row.innerHTML = "";
    const list = presets[apiScope] || [];
    const dl = $("apiPaths");
    dl.innerHTML = "";
    list.forEach((p) => {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "preset";
      b.textContent = p.label;
      b.onclick = () => {
        $("apiMethod").value = p.method;
        $("apiPath").value = p.path;
        $("apiBody").value = p.body == null ? "{}" : JSON.stringify(p.body, null, 2);
        toast("info", "已填入预设", p.label);
      };
      row.appendChild(b);
      const opt = document.createElement("option");
      opt.value = p.path;
      dl.appendChild(opt);
    });
    if (list[0]) {
      $("apiMethod").value = list[0].method;
      $("apiPath").value = list[0].path;
      $("apiBody").value = list[0].body == null ? "{}" : JSON.stringify(list[0].body, null, 2);
    }
  }

  async function sendApi() {
    const method = $("apiMethod").value;
    let path = $("apiPath").value.trim();
    if (!path.startsWith("/")) path = "/" + path;
    let body;
    const raw = $("apiBody").value.trim();
    if (method !== "GET" && raw && raw !== "{}") {
      try {
        body = JSON.parse(raw);
      } catch (e) {
        toast("err", "JSON 无效", e.message);
        return;
      }
    } else if (method !== "GET") body = {};
    return withToast(
      `${method} ${path}`,
      async () => {
        const res = await api(path, { method, body });
        $("apiOut").textContent = JSON.stringify(res.data, null, 2);
        return res;
      },
      { okMsg: (r) => `HTTP ${r.status}` }
    );
  }

  function handleOAuthReturn() {
    const params = new URLSearchParams(window.location.search);
    if (params.get("login") !== "1" && !params.get("ticket") && !params.get("code")) return;
    const ticket = params.get("ticket") || "";
    const provider = params.get("provider") || "";
    const status = params.get("status") || "";
    const banner = $("oauthBanner");
    banner.hidden = false;
    banner.textContent = `OAuth 返回：${provider || "?"} ticket=${ticket || "?"} status=${status || "?"}`;
    toast("info", "OAuth 返回", `${provider || ""} ${status || ""}`);
    if (ticket) {
      qrTicket = ticket;
      $("qrTicket").textContent = ticket;
      $("qrProvider").textContent = provider || "—";
      $("qrStatus").textContent = status || "—";
      startPolling();
    }
    window.history.replaceState({}, "", window.location.pathname);
  }

  // ---------- Asset board with history ----------
  function assetLabel(key) {
    return ASSET_LABELS[key] || key.replace(/^.*\./, "").replace(/Url$/, "") || key;
  }
  function isModelKey(key) {
    return MODEL_KEYS.includes(key) || /glb|fbx|obj|mtl/i.test(key);
  }

  function mergeAssets(items) {
    for (const it of items || []) {
      if (!it || !it.id) continue;
      const prev = assetHistory.get(it.id) || {};
      assetHistory.set(it.id, { ...prev, ...it, assets: { ...(prev.assets || {}), ...(it.assets || {}) } });
    }
  }

  function assetListForRender() {
    let list = [...assetHistory.values()];
    if (assetSource !== "all") list = list.filter((x) => x.source === assetSource);
    if (assetStatus !== "all")
      list = list.filter((x) => (x.status || "").toLowerCase() === assetStatus);
    const ts = (it) => {
      const v = it.createdAt;
      if (typeof v === "number") return v;
      const n = parseFloat(v);
      return Number.isFinite(n) ? n : 0;
    };
    list.sort((a, b) => ts(b) - ts(a));
    return list;
  }

  function renderAssetBoard(root) {
    const items = assetListForRender();
    const c = root?.credits || {};
    const main = c.main?.remainQuota ?? c.main?.quotainfo?.body?.remainQuota;
    const concept =
      c.concept?.get_remaining_times?.body?.data?.remainingTimes ?? c.concept?.remainingTimes;
    $("assetCredits").textContent =
      `积分独立 · main remainQuota=${main ?? "—"} · concept remainingTimes=${concept ?? "—"} · ` +
      `本地历史 ${assetHistory.size} · 本页显示 ${items.length} · 上游 total=${root?.totalCount ?? "—"}`;

    const grid = $("assetGrid");
    if (!items.length) {
      grid.innerHTML = `<div class="hint">暂无任务。点「刷新」或「加载更多历史」</div>`;
      $("assetFooter").textContent = "";
      return;
    }
    grid.innerHTML = "";
    for (const it of items) {
      const card = document.createElement("div");
      card.className = "asset-card";
      const st = (it.status || "unknown").toLowerCase();
      const src = it.source || "?";
      const preview = it.previewUrl || "";
      const assets = it.assets || {};
      const links = [];
      for (const [k, v] of Object.entries(assets)) {
        if (!v || !String(v).startsWith("http")) continue;
        if (k.includes("invisible_wall") || k === "air_wall") continue;
        const cls = isModelKey(k) ? "model" : "";
        links.push(
          `<a class="${cls}" href="${v}" target="_blank" rel="noreferrer" title="${k}">${assetLabel(k)}</a>`
        );
      }
      if (it.primaryModelUrl) {
        links.unshift(
          `<a class="model" href="${it.primaryModelUrl}" target="_blank" rel="noreferrer">主模型</a>`
        );
      }
      const notes = (it.notes || []).filter(Boolean).join(" · ");
      const title = it.title || it.prompt || it.id || "未命名";
      const created =
        typeof it.createdAt === "number"
          ? new Date(it.createdAt * 1000).toLocaleString()
          : it.createdAt
            ? String(it.createdAt).replace("T", " ").slice(0, 19)
            : "";
      card.innerHTML = `
        <div class="thumb ${preview ? "" : "empty"}" ${
          preview ? `style="background-image:url('${preview}')"` : ""
        }></div>
        <div class="body">
          <div class="title" title="${title}">${title}</div>
          <div class="asset-meta">
            <span class="badge-src ${src}">${src}</span>
            <span class="badge-st ${st}">${st}</span>
            ${it.modelType ? `<span>${it.modelType}</span>` : ""}
            ${it.style ? `<span>${it.style}</span>` : ""}
            ${it.progress != null && it.progress !== "" ? `<span>${it.progress}%</span>` : ""}
            ${created ? `<span>${created}</span>` : ""}
          </div>
          ${notes ? `<div class="asset-notes">${notes}</div>` : ""}
          ${it.error ? `<div class="asset-err">${it.error}</div>` : ""}
          <div class="asset-links">${links.slice(0, 10).join("") || '<span class="hint">暂无文件 URL</span>'}</div>
          <div class="asset-notes">${(it.id || "").slice(-20)}</div>
        </div>
      `;
      grid.appendChild(card);
    }
    $("assetFooter").textContent = `已合并本地历史，刷新不会丢掉旧任务。可「加载更多历史」翻页 offset=${assetOffset}`;
  }

  async function refreshAssets(silent, loadMore) {
    if (loadMore) assetOffset += assetLimit;
    const url =
      `/api/text3d/assets?source=${assetSource}&limit=${assetLimit}&offset=${assetOffset}&status=all`;
    return withToast(
      loadMore ? "加载历史" : "刷新看板",
      async () => {
        const res = await api(url);
        const root = res.data?.data || {};
        mergeAssets(root.items);
        if (root.totalCount != null) assetServerTotal = root.totalCount;
        renderAssetBoard(root);
        return res;
      },
      {
        silent,
        loading: loadMore ? "正在拉取历史任务…" : "正在刷新资源看板…",
        okMsg: (r) => {
          const d = r.data?.data || {};
          return `本页 ${d.count ?? 0} · 本地历史 ${assetHistory.size} · 上游 ${d.totalCount ?? "—"}`;
        },
      }
    );
  }

  function startAssetPolling() {
    stopAssetPolling();
    if ($("assetAuto")?.checked) {
      assetTimer = setInterval(() => refreshAssets(true), 8000);
      toast("info", "自动刷新已开启", "每 8 秒静默刷新看板");
    }
  }
  function stopAssetPolling() {
    if (assetTimer) {
      clearInterval(assetTimer);
      assetTimer = null;
    }
  }

  // ---------- Wire events ----------
  document.querySelectorAll("#loginTabs .chip").forEach((btn) => {
    btn.onclick = () => {
      const tab = btn.dataset.tab;
      document.querySelectorAll("#loginTabs .chip").forEach((b) =>
        b.classList.toggle("active", b === btn)
      );
      ["cookie", "email", "phone"].forEach((t) => {
        const el = document.getElementById("tab-" + t);
        if (el) el.hidden = t !== tab;
      });
      toast("info", "登录方式", tab);
    };
  });

  $("btnHealth").onclick = () => refreshHealth(false);
  $("btnQRStart").onclick = () => startLogin("wechat");
  $("btnQQStart").onclick = () => startLogin("qq");
  $("btnQRPoll").onclick = async () => {
    await withToast("刷新登录状态", async () => {
      await refreshLogin(true);
      return doPoll(true);
    });
  };
  $("btnLogout").onclick = () =>
    withToast(
      "登出",
      async () => {
        const res = await api("/api/login/logout", { method: "POST", body: {} });
        stopPolling();
        await refreshLogin(true);
        return res;
      },
      { okMsg: () => "会话已清空" }
    );
  $("btnImport").onclick = doImport;
  $("btnCreditMain").onclick = () => creditMain(false);
  $("btnCreditConcept").onclick = () => creditConcept(false);
  $("btnCreditAll").onclick = () => creditAll(false);
  $("btnSend").onclick = sendApi;
  $("btnSign").onclick = () =>
    withToast("签名调试", async () => {
      const res = await api("/api/sign/debug");
      $("signOut").textContent = JSON.stringify(res.data, null, 2);
      return res;
    }, { okMsg: (r) => `key=${r.data?.data?.key_string || "?"}` });
  $("btnClearLog").onclick = () => {
    $("logOut").textContent = "—";
    toast("info", "日志已清空");
  };

  $("btnEmailCode").onclick = () =>
    withToast("邮箱验证码", async () => {
      const email = $("emailInput").value.trim();
      if (!email) {
        toast("warn", "请填写邮箱");
        return { ok: false, status: 400, data: { error: "email required" } };
      }
      const res = await api("/api/login/email/send-code", {
        method: "POST",
        body: { email, captchaTicket: "", captchaRandstr: "" },
      });
      $("apiOut").textContent = JSON.stringify(res.data, null, 2);
      return res;
    });
  $("btnEmailVerify").onclick = () =>
    withToast("邮箱登录", async () => {
      const email = $("emailInput").value.trim();
      const verificationCode = $("emailCode").value.trim();
      const res = await api("/api/login/email/verify", {
        method: "POST",
        body: { email, verificationCode },
      });
      $("apiOut").textContent = JSON.stringify(res.data, null, 2);
      await refreshLogin(true);
      return res;
    });
  $("btnPhoneCode").onclick = () =>
    withToast("短信验证码", async () => {
      const phone = $("phoneInput").value.trim();
      const res = await api("/api/login/phone/send-code", {
        method: "POST",
        body: { phone, captchaTicket: "", captchaRandstr: "" },
      });
      $("apiOut").textContent = JSON.stringify(res.data, null, 2);
      return res;
    });
  $("btnPhoneVerify").onclick = () =>
    withToast("手机登录", async () => {
      const phone = $("phoneInput").value.trim();
      const verificationCode = $("phoneCode").value.trim();
      const res = await api("/api/login/phone/verify", {
        method: "POST",
        body: { phone, verificationCode },
      });
      $("apiOut").textContent = JSON.stringify(res.data, null, 2);
      await refreshLogin(true);
      return res;
    });

  document.querySelectorAll("#modeSwitch .chip").forEach((b) => {
    b.onclick = () =>
      withToast(
        "切换模式",
        async () => {
          document.querySelectorAll("#modeSwitch .chip").forEach((x) =>
            x.classList.toggle("active", x === b)
          );
          const res = await api("/api/mode?mode=" + b.dataset.mode, {
            method: "POST",
            body: { mode: b.dataset.mode },
          });
          await refreshHealth(true);
          return res;
        },
        { okMsg: () => `mode=${b.dataset.mode}` }
      );
  });

  document.querySelectorAll("#apiModeSwitch .chip").forEach((b) => {
    b.onclick = () => {
      apiScope = b.dataset.apiscope;
      document.querySelectorAll("#apiModeSwitch .chip").forEach((x) =>
        x.classList.toggle("active", x === b)
      );
      renderPresets();
      toast("info", "端点分组", apiScope);
    };
  });

  document.querySelectorAll("#assetSourceSwitch .chip").forEach((b) => {
    b.onclick = () => {
      assetSource = b.dataset.src || "all";
      document.querySelectorAll("#assetSourceSwitch .chip").forEach((x) =>
        x.classList.toggle("active", x === b)
      );
      renderAssetBoard({});
      refreshAssets(false);
    };
  });
  document.querySelectorAll("#assetStatusSwitch .chip").forEach((b) => {
    b.onclick = () => {
      assetStatus = b.dataset.st || "all";
      document.querySelectorAll("#assetStatusSwitch .chip").forEach((x) =>
        x.classList.toggle("active", x === b)
      );
      renderAssetBoard({});
      toast("info", "状态筛选", assetStatus);
    };
  });

  $("btnRefreshAssets").onclick = () => refreshAssets(false);
  $("btnLoadMoreAssets").onclick = () => refreshAssets(false, true);
  $("assetAuto").onchange = () => {
    if ($("assetAuto").checked) startAssetPolling();
    else {
      stopAssetPolling();
      toast("info", "自动刷新已关闭");
    }
  };

  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") {
      refreshLogin(true).then(() => {
        if (qrTicket) doPoll(true);
        refreshAssets(true);
      });
    }
  });

  renderPresets();
  handleOAuthReturn();
  (async () => {
    toast("info", "控制台加载中", base());
    await refreshHealth(true);
    await refreshLogin(true);
    await refreshAssets(true);
    // load an extra page of history on first load
    await refreshAssets(true, true);
    toast("ok", "就绪", "所有操作都会有 Toast 提示");
  })();
})();
