import { chromium, firefox } from "playwright";

const BASE = process.env.UI_BASE || "http://127.0.0.1:8812";
const BROWSER = process.env.BROWSER || "chromium";
const results = [];
let failed = 0;
function check(name, cond, extra = "") {
  const ok = !!cond;
  if (!ok) failed++;
  results.push(`${ok ? "PASS" : "FAIL"}  ${name}${extra ? " — " + extra : ""}`);
}

const browser = await (BROWSER === "firefox" ? firefox : chromium).launch({
  headless: true,
});
const page = await browser.newPage();
const pageErrors = [];
const consoleErrors = [];
page.on("pageerror", (e) => pageErrors.push(e.message));
page.on("console", (m) => {
  if (m.type() === "error") consoleErrors.push(m.text());
});

// 1. 控制台加载（无 JS 异常）
await page.goto(BASE + "/", { waitUntil: "networkidle", timeout: 20000 });
check("控制台加载无 JS 异常", pageErrors.length === 0, pageErrors.join("; "));

// 2. 自动连接徽章
await page
  .waitForFunction(() => {
    const b = document.getElementById("healthBadge");
    return b && (b.textContent.includes("已连接") || b.textContent.includes("失败"));
  }, { timeout: 10000 })
  .catch(() => {});
const badge = (await page.textContent("#healthBadge")) || "";
check("自动连接（徽章更新为已连接）", badge.includes("已连接"), badge.trim());

// 3. Proxy 输入与检测按钮已删除
check("Proxy 输入框已删除", (await page.locator("#baseUrl").count()) === 0);
check("「检测连接」按钮已删除", (await page.locator("#btnHealth").count()) === 0);
check("页面无「检测连接」文案", !(await page.content()).includes("检测连接"));

// 4. 资源看板自动加载
await page
  .waitForFunction(() => {
    const g = document.getElementById("assetGrid");
    return g && g.querySelectorAll(".asset-card").length > 0;
  }, { timeout: 25000 })
  .catch(() => {});
const cards = await page.locator(".asset-card").count();
check("资源看板自动加载", cards > 0, `${cards} 张任务卡`);

// 5. 敏感字段脱敏（服务端）
const st = await page.evaluate(() =>
  fetch("/api/login/status").then((r) => r.json())
);
const uid = st?.data?.innerUserId ?? "";
check(
  "innerUserId 已脱敏",
  uid === "" || uid === "***" || uid.includes("…"),
  JSON.stringify(uid)
);

const cr = await page.evaluate(() =>
  fetch("/api/credit/concept").then((r) => r.json())
);
const body = cr?.data?.getuserinfo?.body || {};
const phone = body.phoneNumber ?? "";
check("phoneNumber 已脱敏", phone === "" || phone.includes("…"), JSON.stringify(phone));
const enc = body.encryptCompletePhoneNumber ?? "";
check(
  "加密手机号已脱敏",
  enc === "" || enc === "***" || enc.includes("…"),
  JSON.stringify(enc.slice(0, 24))
);
check("敏感 flag 保留（hyTokenPresent 等）", typeof st?.data?.hyTokenPresent === "boolean");

// 6. 已删除的测试器不复存在
check("API 测试器已废弃（无 btnSend）", (await page.locator("#btnSend").count()) === 0);

const mainPageConsoleErrors = consoleErrors.length;

// 7. OpenAPI / Swagger UI
const oa = await page.evaluate(() =>
  fetch("/openapi.json").then((r) => r.json())
);
const pathCount = Object.keys(oa?.paths || {}).length;
check("openapi.json 可用", pathCount >= 30, `${pathCount} paths`);

const docsResp = await page.goto(BASE + "/docs", { waitUntil: "domcontentloaded", timeout: 15000 });
check("/docs HTTP 200", docsResp.status() === 200);
check("Swagger UI 容器存在", (await page.locator("#swagger-ui").count()) === 1);

check(
  "主页面无 console error",
  mainPageConsoleErrors === 0,
  consoleErrors.slice(0, 3).join(" | ")
);

await browser.close();
console.log(results.join("\n"));
console.log(failed ? `\n${failed} FAILED` : "\nALL PASS");
process.exit(failed ? 1 : 0);
