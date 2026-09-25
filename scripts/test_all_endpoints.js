/**
 * Comprehensive endpoint smoke test for hunyuan3d-proxy.
 * Usage: node tools/test_all_endpoints.js [baseUrl]
 */
const fs = require("fs");

const BASE = (process.argv[2] || "http://127.0.0.1:8803").replace(/\/$/, "");
const REPORT = [];
const PROMPT =
  "cute cartoon robot toy, soft rounded volumes, cel-shaded, white background, no text";

function log(line) {
  console.log(line);
}

async function call(name, method, path, body, headers) {
  const url = BASE + path;
  const h = Object.assign({ Accept: "application/json" }, headers || {});
  const init = { method, headers: h };
  if (body != null && method !== "GET" && method !== "HEAD") {
    h["Content-Type"] = h["Content-Type"] || "application/json";
    init.body = typeof body === "string" ? body : JSON.stringify(body);
  }
  const t0 = Date.now();
  const row = { name, method, path, url };
  try {
    const res = await fetch(url, init);
    const text = await res.text();
    const ms = Date.now() - t0;
    let json = null;
    try {
      json = JSON.parse(text);
    } catch (_) {}
    row.status = res.status;
    row.ms = ms;
    row.ok = res.status < 400;
    row.contentType = res.headers.get("content-type");
    row.snippet = text.slice(0, 240).replace(/\s+/g, " ");
    // extract useful fields
    if (json) {
      if (json.ok !== undefined) row.envelopeOk = json.ok;
      if (json.data && typeof json.data === "object") {
        const d = json.data;
        if (d.status) row.dataStatus = d.status;
        if (d.mode) row.mode = d.mode;
        if (d.remainQuota !== undefined) row.mainRemain = d.remainQuota;
        if (d.signKey) row.signKey = d.signKey;
        if (d.worksId || d.text2image || d.text2geometry) {
          row.worksId =
            d.worksId ||
            d.text2image?.body?.worksId ||
            d.text2geometry?.body?.worksId;
        }
        if (d.count !== undefined) row.count = d.count;
        if (d.providers) row.providers = d.providers.length;
        if (d.actions) row.cloudActions = Object.keys(d.actions).length;
        if (d.ticket) row.ticket = d.ticket;
        if (d.loggedIn !== undefined) row.loggedIn = d.loggedIn;
        if (d.get_remaining_times?.body?.data?.remainingTimes !== undefined) {
          row.conceptTimes = d.get_remaining_times.body.data.remainingTimes;
        }
        if (d.main?.remainQuota !== undefined) row.mainRemain = d.main.remainQuota;
        if (d.concept?.get_remaining_times?.body?.data?.remainingTimes !== undefined) {
          row.conceptTimes = d.concept.get_remaining_times.body.data.remainingTimes;
        }
      }
      // raw non-envelope
      if (json.remainQuota !== undefined) row.mainRemain = json.remainQuota;
      if (json.errNo !== undefined) row.errNo = json.errNo;
      if (json.remainingTimes !== undefined) row.conceptTimes = json.remainingTimes;
    } else if (res.headers.get("content-type")?.includes("image")) {
      row.ok = true;
      row.binary = text.length;
    }
  } catch (e) {
    row.status = 0;
    row.ok = false;
    row.error = e.message;
    row.ms = Date.now() - t0;
  }
  REPORT.push(row);
  const flag = row.ok ? "OK " : "ERR";
  log(
    `${flag} ${String(row.status).padStart(3)} ${String(row.ms).padStart(5)}ms  ${method} ${path}` +
      (row.snippet ? `  | ${row.snippet.slice(0, 80)}` : "") +
      (row.error ? `  | ${row.error}` : "")
  );
  return row;
}

(async () => {
  log("BASE=" + BASE);
  log("\n======== META ========");

  await call("health", "GET", "/api/health");
  await call("mode-get", "GET", "/api/mode");
  await call("mode-set-main", "POST", "/api/mode?mode=main", { mode: "main" });
  await call("mode-set-concept", "POST", "/api/mode?mode=concept", { mode: "concept" });
  await call("mode-set-main2", "POST", "/api/mode?mode=main", { mode: "main" });
  await call("sign-debug", "GET", "/api/sign/debug");
  await call("root-json-or-ui", "GET", "/");
  await call("ui-index", "GET", "/ui/");
  await call("ui-css", "GET", "/ui/app.css");
  await call("ui-js", "GET", "/ui/app.js");
  await call("ui-qr-js", "GET", "/ui/qr.js");

  log("\n======== LOGIN ========");
  await call("login-providers", "GET", "/api/login/providers");
  await call("login-status", "GET", "/api/login/status");
  const qr = await call("login-qr-wechat", "GET", "/api/login/qr/start?provider=wechat");
  const qq = await call("login-qr-qq", "GET", "/api/login/qr/start?provider=qq");
  if (qr.ticket) {
    await call("login-qr-status", "GET", "/api/login/qr/status?ticket=" + encodeURIComponent(qr.ticket));
    await call(
      "login-qr-image",
      "GET",
      "/api/login/qr/image?session=" + encodeURIComponent(qr.ticket)
    );
  }
  await call("login-qr-status-bad", "GET", "/api/login/qr/status?ticket=not-exist");
  await call("login-cookies-import", "POST", "/api/login/cookies", {
    cookies: { hy_user: "smoke_test_uid", hy_token: "smoke_test_token" },
  });
  await call("login-status-after-import", "GET", "/api/login/status");
  await call("email-send-code-missing", "POST", "/api/login/email/send-code", {});
  await call(
    "email-send-code",
    "POST",
    "/api/login/email/send-code",
    { email: "smoke@example.com", captchaTicket: "", captchaRandstr: "" }
  );
  await call(
    "email-verify-missing-code",
    "POST",
    "/api/login/email/verify",
    { email: "smoke@example.com" }
  );
  await call(
    "phone-send-code",
    "POST",
    "/api/login/phone/send-code",
    { phone: "13800138000", captchaTicket: "", captchaRandstr: "" }
  );
  await call("worker-health-proxy", "GET", "/api/login/worker/health");
  // restore cookies from jar if import overwrote - re-import real jar
  try {
    const jar = JSON.parse(fs.readFileSync("D:/lose/hunyuan3d-proxy/data/cookies.json", "utf8"));
    const cookies = {};
    for (const c of jar) if (c.name && c.value) cookies[c.name] = c.value;
    await call("login-cookies-restore-jar", "POST", "/api/login/cookies", { cookies });
  } catch (e) {
    log("jar restore skip " + e.message);
  }

  log("\n======== CREDITS ========");
  await call("credit-main", "GET", "/api/credit/main");
  await call("credit-concept", "GET", "/api/credit/concept");
  await call("credit-all", "GET", "/api/credit/all");
  await call("credit-bad", "GET", "/api/credit/nope");

  log("\n======== CLOUD ai3d ========");
  await call("cloud-list", "GET", "/api/cloud");
  await call("cloud-actions", "GET", "/api/cloud/actions");
  await call("cloud-submit-pro-nocred", "POST", "/api/cloud/submit-pro", {
    Prompt: "a red apple",
    Model: "3.0",
    GenerateType: "Normal",
  });
  await call("cloud-query-pro-nocred", "POST", "/api/cloud/query-pro", { JobId: "1" });
  await call(
    "cloud-action-raw",
    "POST",
    "/api/cloud/action/SubmitHunyuanTo3DProJob",
    { Prompt: "a red apple" }
  );

  log("\n======== TEXT3D ========");
  await call("text3d-works-all", "GET", "/api/text3d/works");
  await call("text3d-works-p2", "GET", "/api/text3d/works?pipeline=2");
  await call("text3d-works-p1", "GET", "/api/text3d/works?pipeline=1");
  // generate geometry only (consumes concept credit) - one small task
  const gen = await call("text3d-generate-geometry", "POST", "/api/text3d/generate", {
    prompt: PROMPT,
    mode: "text-geometry",
    modelVersion: "v3.1",
    faceCount: 50000,
    style: "q_version",
  });

  log("\n======== MAIN PROXY PATHS ========");
  await call("main-config", "GET", "/api/3d/config");
  await call("main-quotainfo-empty", "POST", "/api/3d/quotainfo", {});
  await call(
    "main-quotainfo-scene",
    "POST",
    "/api/3d/quotainfo",
    { sceneType: "3dCreations" }
  );
  await call("main-planCredit", "GET", "/api/3d/planCreditInfo");
  await call("main-purchaseList", "GET", "/api/3d/purchaseList");
  await call("main-creations-count", "POST", "/api/3d/creations/count", {
    sceneType: "3dCreations",
  });
  await call("main-creations-generations", "POST", "/api/3d/creations/generations", {
    prompt: PROMPT,
    sceneType: "3dCreations",
  });
  await call("main-interaction-config", "GET", "/api/3d/interaction/config");
  await call("main-worldPlay-config", "GET", "/api/3d/worldPlay/config");
  await call("main-notice-list", "POST", "/api/3d/notice/list", {});
  await call("main-feedback-submit", "POST", "/api/3d/feedback/submit", {});
  await call("main-jointappid", "GET", "/api/jointappid");
  await call("main-open-auth", "GET", "/api/open/auth?appID=wx01cc05677df34280&code=x&signature=y");
  await call("main-auth-center-catch", "GET", "/api/auth-center/oauth2/authorize");
  await call("main-catchall-api-main", "GET", "/api/main/api/3d/config");
  await call("main-proxy-prefix", "GET", "/proxy/main/api/3d/config");

  log("\n======== CONCEPT PROXY PATHS ========");
  await call("concept-getuserinfo", "GET", "/3d/getuserinfo");
  await call("concept-getuserinfo-alias", "GET", "/api/concept/getuserinfo");
  await call(
    "concept-remaining",
    "POST",
    "/game3d/general_info/get_remaining_times",
    {}
  );
  await call(
    "concept-works-list",
    "POST",
    "/game3d/general_info/get_works_list",
    {}
  );
  await call(
    "concept-good-case",
    "POST",
    "/game3d/general_info/get_good_case",
    {}
  );
  await call(
    "concept-motion-case",
    "POST",
    "/game3d/general_info/get_motion_case",
    {}
  );
  await call("concept-share-detail", "GET", "/game3d/share/detail?shareToken=x");
  await call("concept-feedback-get", "POST", "/game3d/feedback/get", {});
  await call("concept-feedback-reasons", "POST", "/game3d/feedback/get_dislike_reasons", {});
  await call("concept-whitelist-query", "POST", "/new-portal/whitelist/query", {
    business: "3dgame",
    scene: "all",
  });
  await call(
    "concept-text2image",
    "POST",
    "/game3d/concept_design/text2image",
    {
      prompt: PROMPT,
      style: "q_version",
      apose: false,
      worksType: 0,
      modelVersion: "",
      model: "",
      requestId: "smoke" + Date.now().toString(36),
    }
  );
  await call(
    "concept-text2geometry",
    "POST",
    "/game3d/geometry_generation/text2geometry",
    {
      prompt: PROMPT,
      modelVersion: "v3.1",
      faceCount: 50000,
      apose: false,
      worksType: 0,
      requestId: "smokegeo" + Date.now().toString(36),
    }
  );
  await call(
    "concept-uv",
    "POST",
    "/game3d/uv_unwrapping/uv_unwrapping",
    { worksId: "nonexistent" }
  );
  await call(
    "concept-texture-text",
    "POST",
    "/game3d/texture_painting/text2texture",
    { prompt: PROMPT, worksId: "nonexistent" }
  );
  await call(
    "concept-comp-split",
    "POST",
    "/game3d/component_splitting/v2/pre_segmentation",
    { worksId: "nonexistent", mode: "full_segmentation" }
  );
  await call("concept-api-prefix-remaining", "POST", "/api/concept/game3d/general_info/get_remaining_times", {});
  await call("concept-proxy-prefix", "GET", "/proxy/concept/3d/getuserinfo");
  await call("concept-catchall-game3d", "POST", "/game3d/general_info/get_remaining_times", {});

  log("\n======== MISC ========");
  await call("unknown-path", "GET", "/no/such/route");
  await call("method-not-allowed", "DELETE", "/api/health");

  // summary
  const ok = REPORT.filter((r) => r.ok).length;
  const err = REPORT.filter((r) => !r.ok).length;
  log("\n======== SUMMARY ========");
  log(`total=${REPORT.length} ok=${ok} err=${err}`);
  log("\n--- failures ---");
  for (const r of REPORT.filter((x) => !x.ok)) {
    log(`FAIL ${r.method} ${r.path} status=${r.status} ${r.error || r.snippet}`);
  }
  log("\n--- key values ---");
  const health = REPORT.find((r) => r.name === "health");
  const credit = REPORT.find((r) => r.name === "credit-all");
  const genRow = REPORT.find((r) => r.name === "text3d-generate-geometry");
  log("signKey", health?.signKey);
  log("main remain", credit?.mainRemain);
  log("concept times", credit?.conceptTimes);
  log("gen worksId", genRow?.worksId);
  log("login loggedIn", REPORT.find((r) => r.name === "login-status")?.loggedIn);
  log("cloud actions", REPORT.find((r) => r.name === "cloud-list")?.cloudActions);

  const outPath = "D:/lose/output/playwright/endpoint-smoke-report.json";
  fs.mkdirSync("D:/lose/output/playwright", { recursive: true });
  fs.writeFileSync(outPath, JSON.stringify({ base: BASE, at: new Date().toISOString(), ok, err, report: REPORT }, null, 2));
  log("\nreport -> " + outPath);
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
