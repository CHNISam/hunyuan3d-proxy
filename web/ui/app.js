(() => {
  const $ = (id) => document.getElementById(id);

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

  // 同源自连接：控制台始终由代理自身托管，无需手动填 Proxy 地址。
  function base() {
    return publicOrigin();
  }

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
  let assetTimer = null;
  let assetSource = "all";
  let assetStatus = "all";
  let assetOffset = 0;
  const assetLimit = 24;
  const assetHistory = new Map(); // id -> item (local merge, keeps history)
  let assetServerTotal = 0;

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
    // 纹理（贴图）产物 —— 有这一组才是"带材质的成品"
    "texturePaintingRsp.glbUrl": "★贴图GLB",
    "texturePaintingRsp.fbxUrl": "贴图FBX",
    "texturePaintingRsp.imageUrl": "贴图预览",
    "texturePaintingRsp.pbrImageUrl": "PBR基础色",
    "texturePaintingRsp.pbrNormalImageUrl": "PBR法线",
    "texturePaintingRsp.pbrMetallicImageUrl": "PBR金属",
    "texturePaintingRsp.pbrRoughnessImageUrl": "PBR粗糙",
    "texturePaintingRsp.quadsInfo": "UV信息",
  };
  const MODEL_KEYS = [
    "textureGlb", "glb", "geometryGlb", "obj", "obj_url",
    "geometryGenerationRsp.glbUrl", "geometryGenerationRsp.fbxUrl",
    "texturePaintingRsp.glbUrl", "texturePaintingRsp.fbxUrl",
  ];
  // 有序展示：成品模型排最前，中间产物在后
  const ASSET_ORDER = [
    "texturePaintingRsp.glbUrl", "texturePaintingRsp.fbxUrl",
    "geometryGenerationRsp.glbUrl", "geometryGenerationRsp.fbxUrl",
    "texturePaintingRsp.pbrImageUrl", "texturePaintingRsp.pbrNormalImageUrl",
    "texturePaintingRsp.pbrMetallicImageUrl", "texturePaintingRsp.pbrRoughnessImageUrl",
    "texturePaintingRsp.imageUrl", "geometryGenerationRsp.imageUrl",
    "singleImage", "texturePaintingRsp.quadsInfo",
  ];

  // ---------- Sensitive display masking ----------
  function maskId(s) {
    s = String(s || "");
    if (!s) return "";
    if (s.length <= 8) return "***";
    return s.slice(0, 6) + "…" + s.slice(-2);
  }
  function scrubLogText(s) {
    return String(s).replace(
      /([?&](?:ticket|session|nonce|code|token)=)[^&\s"]+/gi,
      "$1***"
    );
  }

  function log(msg, obj) {
    const el = $("logOut");
    if (!el) return;
    const ts = new Date().toLocaleTimeString();
    const line = scrubLogText(`[${ts}] ${msg}`);
    const extra = obj !== undefined ? "\n" + scrubLogText(JSON.stringify(obj, null, 2)) : "";
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
    $("qrTicket").textContent = qrTicket ? maskId(qrTicket) : "—";
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
        hint: "官方登录页出码",
        okMsg: (r) => `ticket=${maskId(r.data?.data?.ticket || "")} · 请扫码`,
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

  function renderTopCredits(res) {
    const d = res.data?.data || {};
    const main = d.main?.remainQuota ?? d.main?.quotainfo?.body?.remainQuota ?? "—";
    const concept =
      d.concept?.get_remaining_times?.body?.data?.remainingTimes ??
      d.concept?.remainingTimes ??
      "—";
    const m = $("topCreditMain");
    const c = $("topCreditConcept");
    if (m) m.textContent = `main ${main}`;
    if (c) c.textContent = `concept ${concept}`;
  }

  async function creditAll(silent) {
    return withToast(
      "积分查询",
      async () => {
        const res = await api("/api/credit/all");
        renderTopCredits(res);
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

  function handleOAuthReturn() {
    const params = new URLSearchParams(window.location.search);
    if (params.get("login") !== "1" && !params.get("ticket") && !params.get("code")) return;
    const ticket = params.get("ticket") || "";
    const provider = params.get("provider") || "";
    const status = params.get("status") || "";
    const banner = $("oauthBanner");
    banner.hidden = false;
    banner.textContent = `OAuth 返回：${provider || "?"} ticket=${maskId(ticket)} status=${status || "?"}`;
    toast("info", "OAuth 返回", `${provider || ""} ${status || ""}`);
    if (ticket) {
      qrTicket = ticket;
      $("qrTicket").textContent = maskId(ticket);
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
    // 两个上游的时间格式不同：main 是 Unix 秒（数字），concept 是 ISO-8601
    // 字符串。只认数字会让所有 concept 任务得 0 分而沉到 main 之后，
    // 刚生成的模型就永远不在列表顶部。
    const ts = (it) => {
      const v = it.createdAt;
      if (typeof v === "number") return v;
      if (typeof v === "string") {
        const trimmed = v.trim();
        if (/^\d+(\.\d+)?$/.test(trimmed)) {
          const n = parseFloat(trimmed);
          if (Number.isFinite(n)) return n;
        }
        const parsed = Date.parse(trimmed);
        if (Number.isFinite(parsed)) return parsed / 1000;
      }
      return 0;
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
      // 成品模型排最前，其余中间产物按固定优先级；未知的排在最后。
      const rank = (k) => {
        const i = ASSET_ORDER.indexOf(k);
        return i === -1 ? ASSET_ORDER.length + 1 : i;
      };
      const entries = Object.entries(assets)
        .filter(([k, v]) => v && String(v).startsWith("http"))
        .filter(([k]) => !k.includes("invisible_wall") && k !== "air_wall")
        .sort((a, b) => rank(a[0]) - rank(b[0]));
      for (const [k, v] of entries) {
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
          ${
            it.upstreamRequest || it.modelChain
              ? `<div class="asset-upstream" title="该任务实际调用的上游接口与模型链路">` +
                `<span class="up-label">上游接口</span>` +
                `<code title="上游记录的 reqPath（注意：它记录的是任务最后一步，可能不是实际提交的入口）">${it.upstreamRequest || "—"}</code>` +
                (it.modelChain
                  ? `<span class="up-chain" title="实际执行的模型链路：${it.modelChain}">` +
                    `${String(it.modelChain).split(";").filter(Boolean).slice(-2).join(" → ")}</span>`
                  : "") +
                `</div>`
              : ""
          }
          ${it.error ? `<div class="asset-err">${it.error}</div>` : ""}
          <div class="asset-links">${links.slice(0, 12).join("") || '<span class="hint">暂无文件 URL</span>'}</div>
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

  $("btnQRStart").onclick = () => startLogin("wechat");
  $("btnQQStart").onclick = () => {
    // QQ 扫码已废弃（redirect 校验收紧 + ptqrlogin 拒绝非浏览器客户端）→ Cookie 导入
    toast("info", "QQ 扫码已废弃", "请使用 Cookie 导入登录");
    const tab = document.querySelector('#loginTabs .chip[data-tab="cookie"]');
    if (tab) tab.click();
    const msg = $("qrMessage");
    if (msg)
      msg.textContent =
        "QQ 扫码登录已废弃：请在浏览器登录 3d.hunyuan.tencent.com，F12 → Application → Cookies 复制 hy_user / hy_token，粘贴到下方「Cookie 导入」。";
  };
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
      refreshHealth(true).then(() =>
        refreshLogin(true).then(() => {
          if (qrTicket) doPoll(true);
          creditAll(true);
          refreshAssets(true);
          refreshMcpPanel(true);
        })
      );
    }
  });


  // ================= MCP 面板 =================
  // 展示内置 MCP 服务：连接状态、端点、令牌、工具目录（含 schema）、接入配置。
  // 数据来自服务端 GET /api/mcp/panel（只读，不含任何生成副作用）。

  const MCP_CREDIT_TOOLS = ["hunyuan_generate_concept", "hunyuan_generate_main"];
  let mcpPanelCache = null;
  let mcpTokenVisible = false;

  function mcpSetStatus(el, status, text) {
    if (!el) return;
    el.setAttribute("data-status", status);
    el.textContent = text;
  }

  function mcpEsc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  // 渲染一个工具的 inputSchema：属性表 + 必填标记 + 类型/枚举/默认值
  function mcpSchemaTable(schema) {
    const props = (schema && schema.properties) || {};
    const required = (schema && schema.required) || [];
    const names = Object.keys(props);
    if (!names.length) {
      return `<div class="hint">无参数</div>`;
    }
    const rows = names
      .map((n) => {
        const p = props[n] || {};
        let type = p.type || "any";
        if (Array.isArray(p.enum) && p.enum.length) type += " · " + p.enum.join("|");
        if (p.default !== undefined) type += ` · 默认 ${JSON.stringify(p.default)}`;
        const isReq = required.indexOf(n) >= 0;
        return (
          `<tr><td class="pname">${mcpEsc(n)}${isReq ? '<span class="req">必填</span>' : ""}</td>` +
          `<td class="ptype">${mcpEsc(type)}</td>` +
          `<td>${mcpEsc(p.description || "")}</td></tr>`
        );
      })
      .join("");
    return (
      `<div class="mcp-params"><table>` +
      `<thead><tr><th>参数</th><th>类型</th><th>说明</th></tr></thead>` +
      `<tbody>${rows}</tbody></table></div>`
    );
  }

  // schema 可移植性检查（参考 MCP Inspector 的做法）：合法 JSON Schema 但
  // 真实客户端会拒绝/误处理的构造，在这里提前提示服务端作者。
  function mcpSchemaFindings(schema) {
    const findings = [];
    const props = (schema && schema.properties) || {};
    Object.keys(props).forEach((n) => {
      const p = props[n] || {};
      if (p.type === undefined && !p.enum && !p.anyOf && !p.oneOf) {
        findings.push(`参数 ${n} 未声明 type`);
      }
      if (Array.isArray(p.enum) && p.enum.some((v) => typeof v !== "string")) {
        findings.push(`参数 ${n} 的 enum 含非字符串值`);
      }
    });
    if (schema && schema.additionalProperties === false && !Object.keys(props).length) {
      findings.push("additionalProperties=false 但无 properties：客户端会拒绝任何入参");
    }
    return findings;
  }

  function renderMcpTools(tools) {
    const list = $("mcpToolList");
    if (!list) return;
    const onlyCredit = $("mcpOnlyCredit") && $("mcpOnlyCredit").checked;
    const shown = (tools || []).filter(
      (t) => !onlyCredit || MCP_CREDIT_TOOLS.indexOf(t.name) >= 0
    );
    list.setAttribute("data-tool-count", String(shown.length));
    if (!shown.length) {
      list.innerHTML = `<div class="hint">没有匹配的工具</div>`;
      if ($("mcpTabCount")) $("mcpTabCount").textContent = "0";
      return;
    }
    list.innerHTML = "";
    shown.forEach((t) => {
      const credit = MCP_CREDIT_TOOLS.indexOf(t.name) >= 0;
      const findings = mcpSchemaFindings(t.inputSchema);
      const el = document.createElement("div");
      el.className = "mcp-tool";
      el.innerHTML =
        `<div class="mcp-tool-head">` +
        `<span class="mcp-tool-caret">▶</span>` +
        `<span class="mcp-tool-name">${mcpEsc(t.name)}</span>` +
        `<span class="mcp-flag ${credit ? "credit" : "readonly"}">${credit ? "消耗积分" : "只读"}</span>` +
        `<span class="mcp-tool-desc">${mcpEsc(t.description)}</span>` +
        `<span class="mcp-call-status" data-status="idle"></span>` +
        `</div>` +
        `<div class="mcp-tool-body">` +
        `<div class="mcp-sub">入参</div>` +
        mcpSchemaTable(t.inputSchema) +
        (findings.length
          ? `<div class="mcp-schema-warn">schema 提示：${findings.map(mcpEsc).join("；")}</div>`
          : "") +
        `</div>`;
      el.querySelector(".mcp-tool-head").addEventListener("click", () => {
        el.classList.toggle("open");
      });
      list.appendChild(el);
    });
    if ($("mcpTabCount")) $("mcpTabCount").textContent = String(shown.length);
  }

  function renderMcpConnect(p) {
    const authOn = p.authRequired === true && p.token;
    const http =
      `// MCP Streamable HTTP${authOn ? "" : "（当前无需鉴权）"}\n` +
      `POST ${p.endpoint}\n` +
      (authOn ? `Authorization: Bearer ${p.token}\n` : "") +
      `Content-Type: application/json\n\n` +
      `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`;
    const curl =
      `curl -s ${p.endpoint} \\\n` +
      (authOn ? `  -H 'Authorization: Bearer ${p.token}' \\\n` : "") +
      `  -H 'Content-Type: application/json' \\\n` +
      `  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"${p.protocol}","capabilities":{}}}'`;
    // 工具清单：可直接喂给理解 MCP 的客户端
    const toolsJson = JSON.stringify(
      {
        endpoint: p.endpoint,
        authRequired: p.authRequired === true,
        tools: (p.tools || []).map((t) => ({
          name: t.name,
          description: t.description,
          inputSchema: t.inputSchema,
        })),
      },
      null,
      2
    );
    if ($("mcpCfgHttp")) $("mcpCfgHttp").textContent = http;
    if ($("mcpCfgTools")) $("mcpCfgTools").textContent = toolsJson;
    if ($("mcpCfgCurl")) $("mcpCfgCurl").textContent = curl;
  }

  async function refreshMcpPanel(silent) {
    const badge = $("mcpStatusBadge");
    mcpSetStatus(badge, "connecting", "连接中…");
    const errBox = $("mcpError");
    if (errBox) errBox.hidden = true;
    try {
      const res = await fetch(base() + "/api/mcp/panel", { cache: "no-store" });
      const body = await res.json();
      if (!res.ok || !body || body.ok !== true) {
        throw new Error((body && body.error) || "HTTP " + res.status);
      }
      const p = body.data;
      mcpPanelCache = p;

      mcpSetStatus(badge, "connected", "已启用");
      if ($("mcpTransport")) $("mcpTransport").textContent = p.transport;
      if ($("mcpProtocol")) $("mcpProtocol").textContent = p.protocol;
      if ($("mcpEndpoint")) {
        $("mcpEndpoint").textContent = p.endpoint;
        $("mcpEndpoint").title = p.endpoint;
      }
      if ($("mcpToolCount")) $("mcpToolCount").textContent = String(p.toolCount);
      const st = p.stats || {};
      if ($("mcpCalls")) {
        $("mcpCalls").textContent =
          `调用 ${st.toolCalls || 0} / 请求 ${st.requests || 0}` +
          (st.errors ? ` · 错误 ${st.errors}` : "");
      }
      if ($("mcpLast")) {
        $("mcpLast").textContent = st.lastMethod
          ? `${st.lastMethod}${st.lastTool ? " " + st.lastTool : ""}${
              st.lastAt ? " · " + String(st.lastAt).replace("T", " ").slice(11, 19) : ""
            }`
          : "尚无调用";
      }
      const authOn = p.authRequired === true && p.token;
      if ($("mcpTokenRow")) $("mcpTokenRow").hidden = !authOn;
      if ($("mcpNoAuthRow")) $("mcpNoAuthRow").hidden = !!authOn;
      if (authOn && $("mcpToken")) {
        $("mcpToken").textContent = mcpTokenVisible ? p.token : "•".repeat(16);
      }
      if ($("mcpToolHint")) {
        $("mcpToolHint").textContent =
          `共 ${p.toolCount} 个工具 · 消耗积分：${(p.spendsCredit || []).join("、") || "无"}`;
      }
      const warnBox = $("mcpWarning");
      if (warnBox) {
        if (!authOn && p.warning) {
          warnBox.hidden = false;
          warnBox.textContent = "⚠ " + p.warning;
        } else {
          warnBox.hidden = true;
        }
      }
      renderMcpTools(p.tools);
      renderMcpConnect(p);
      if (!silent) toast("ok", "MCP 已就绪", `${p.toolCount} 个工具 · ${p.transport}`);
    } catch (e) {
      mcpSetStatus(badge, "error", "不可用");
      if (errBox) {
        errBox.hidden = false;
        errBox.textContent = "MCP 面板加载失败：" + (e && e.message ? e.message : e);
      }
      if ($("mcpToolList")) {
        $("mcpToolList").innerHTML = `<div class="hint">无法加载工具目录</div>`;
      }
      if (!silent) toast("err", "MCP 面板失败", e && e.message ? e.message : String(e));
    }
  }

  function mcpCopy(text, label) {
    const done = () => toast("ok", "已复制", label);
    const fail = (e) => toast("err", "复制失败", e && e.message ? e.message : String(e));
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done).catch(fail);
      return;
    }
    try {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      document.execCommand("copy");
      ta.remove();
      done();
    } catch (e) {
      fail(e);
    }
  }

  // 校验令牌是否真的能通过 /mcp（用 GET 探测，不消耗积分）
  async function mcpProbe() {
    const badge = $("mcpStatusBadge");
    const rej = $("mcpRejected");
    if (rej) rej.hidden = true;
    try {
      const headers = {};
      if (mcpPanelCache && mcpPanelCache.authRequired && mcpPanelCache.token) {
        headers.Authorization = "Bearer " + mcpPanelCache.token;
      }
      const res = await fetch(base() + "/mcp", { cache: "no-store", headers });
      const body = await res.json().catch(() => null);
      if (res.status === 200 && body && body.ok) {
        mcpSetStatus(badge, "connected", "已启用 · 鉴权通过");
        if (!rej) toast("ok", "MCP 探测成功", body.data.tools + " 个工具");
        return;
      }
      if (res.status === 401) {
        mcpSetStatus(badge, "error", "鉴权失败");
        if (rej) {
          rej.hidden = false;
          rej.textContent = "鉴权被拒绝（401）：令牌不匹配，请刷新页面重取。";
        }
        return;
      }
      mcpSetStatus(badge, "error", "响应异常");
      if (rej) {
        rej.hidden = false;
        rej.textContent = "探测返回异常：HTTP " + res.status;
      }
    } catch (e) {
      mcpSetStatus(badge, "error", "不可达");
      if (rej) {
        rej.hidden = false;
        rej.textContent = "探测失败：" + (e && e.message ? e.message : e);
      }
    }
  }

  function bindMcpPanel() {
    const sec = $("mcpSection");
    if (!sec) return;

    if ($("btnMcpRefresh")) {
      $("btnMcpRefresh").addEventListener("click", () => {
        refreshMcpPanel(false).then(mcpProbe);
      });
    }
    if ($("btnMcpToggleToken")) {
      $("btnMcpToggleToken").addEventListener("click", () => {
        mcpTokenVisible = !mcpTokenVisible;
        $("btnMcpToggleToken").textContent = mcpTokenVisible ? "隐藏" : "显示";
        if (mcpPanelCache && $("mcpToken")) {
          $("mcpToken").textContent = mcpTokenVisible ? mcpPanelCache.token : "•".repeat(16);
        }
      });
    }
    if ($("btnMcpCopyConfig")) {
      $("btnMcpCopyConfig").addEventListener("click", () => {
        if (!mcpPanelCache) return toast("warn", "尚未加载", "请先刷新 MCP 面板");
        mcpCopy($("mcpCfgCurl") ? $("mcpCfgCurl").textContent : "", "调用示例");
      });
    }
    if ($("btnMcpCopyCurl")) {
      $("btnMcpCopyCurl").addEventListener("click", () => {
        if (!mcpPanelCache) return toast("warn", "尚未加载", "请先刷新 MCP 面板");
        mcpCopy($("mcpCfgCurl") ? $("mcpCfgCurl").textContent : "", "curl 调用示例");
      });
    }
    if ($("mcpOnlyCredit")) {
      $("mcpOnlyCredit").addEventListener("change", () => {
        if (mcpPanelCache) renderMcpTools(mcpPanelCache.tools);
      });
    }
    const tabs = $("mcpTabs");
    if (tabs) {
      tabs.querySelectorAll(".chip").forEach((btn) => {
        btn.addEventListener("click", () => {
          tabs.querySelectorAll(".chip").forEach((b) => b.classList.remove("active"));
          btn.classList.add("active");
          const which = btn.getAttribute("data-tab");
          $("tab-mcp-tools").hidden = which !== "tools";
          $("tab-mcp-connect").hidden = which !== "connect";
        });
      });
    }
  }

  bindMcpPanel();

  handleOAuthReturn();
  (async () => {
    toast("info", "自动连接", base());
    await refreshHealth(true);
    await refreshLogin(true);
    await creditAll(true);
    await refreshAssets(true);
    // load an extra page of history on first load
    await refreshAssets(true, true);
    await refreshMcpPanel(true);
    toast("ok", "就绪", "所有操作都会有 Toast 提示");
  })();
})();
