#!/usr/bin/env node
/**
 * 用调试账号登录后给页面截图（P9 #38，2026-10-21 用户裁定收进仓库）—— 给 Claude 与开发者验证界面用。
 *
 *   node web/scripts/screenshot.mjs [选项] <路径>...
 *     --as      basic | advanced | admin | guest   以哪一档的调试账号看（guest = 不登录）  缺省 advanced
 *     --base    站点地址                                                           缺省 $MELETE_SHOT_BASE，再缺省 $MELETE_WEB_ORIGIN
 *     --out     截图目录                                                           缺省 <系统临时目录>/melete-shots
 *     --width   视口宽度（< 768 按手机模拟）                                        缺省 1280
 *     --theme   light | dark                                                       缺省 light
 *     --locale  zh | ja                                                            缺省 zh
 *     --wait    页面 load 之后再等多少毫秒（字体 / 客户端渲染）                         缺省 1200
 *
 *   例：node web/scripts/screenshot.mjs --as basic --width 390 / /me
 *
 * ⭐ 登录走网页真实的那条路：POST /auth/password（调试账号登录页的表单路由）→ 拿到 Set-Cookie。
 *   所以目标站点必须打开了调试账号登录（MELETE_WEB_PASSWORD_LOGIN=on）；没打开时会得到 404 并直接报错。
 *   截完图会登出，吊销这次的会话 —— 不在账号的「登录设备」里越积越多。
 * ⭐ 凭证只从环境变量读（MELETE_DEBUG_<档位>_USER / _PASS），⛔ 不写进任何文件、不打印。
 * ⭐ 不装任何东西：浏览器用本机已有的 Chromium（Playwright 缓存目录里自动找，或 MELETE_SHOT_CHROME 指定），
 *   驱动直接走 DevTools 协议（Node 22 自带 WebSocket）。
 */
import { spawn } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { parseArgs } from "node:util";

const { values: opt, positionals: paths } = parseArgs({
  allowPositionals: true,
  options: {
    as: { type: "string", default: "advanced" },
    base: { type: "string" },
    out: { type: "string", default: join(tmpdir(), "melete-shots") },
    width: { type: "string", default: "1280" },
    theme: { type: "string", default: "light" },
    locale: { type: "string", default: "zh" },
    wait: { type: "string", default: "1200" },
  },
});

const fail = (msg) => {
  console.error(`✗ ${msg}`);
  process.exit(1);
};
const TIERS = ["basic", "advanced", "admin", "guest"];
if (!TIERS.includes(opt.as)) fail(`--as 只能是 ${TIERS.join(" / ")}`);
if (paths.length === 0) fail("至少给一个路径，例如 /");
const base = (opt.base ?? process.env.MELETE_SHOT_BASE ?? process.env.MELETE_WEB_ORIGIN ?? "").replace(/\/$/, "");
if (!base) fail("不知道截哪个站点：给 --base，或设 MELETE_SHOT_BASE / MELETE_WEB_ORIGIN");
const width = Number(opt.width);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/** 本机已有的 Chromium：显式指定 > Playwright 缓存里的 headless shell > 完整版。 */
function findChrome() {
  if (process.env.MELETE_SHOT_CHROME) return process.env.MELETE_SHOT_CHROME;
  const cache = join(homedir(), ".cache", "ms-playwright");
  if (!existsSync(cache)) return null;
  const candidates = [
    ["chromium_headless_shell-", "chrome-headless-shell-linux64/chrome-headless-shell"],
    ["chromium-", "chrome-linux64/chrome"],
  ];
  for (const [prefix, bin] of candidates) {
    const dirs = readdirSync(cache).filter((d) => d.startsWith(prefix)).sort().reverse();
    for (const d of dirs) if (existsSync(join(cache, d, bin))) return join(cache, d, bin);
  }
  return null;
}

/** 用调试账号登录，返回会话 cookie（name/value 对）。guest 返回空。 */
async function login() {
  if (opt.as === "guest") return [];
  const tier = opt.as.toUpperCase();
  const username = process.env[`MELETE_DEBUG_${tier}_USER`];
  const password = process.env[`MELETE_DEBUG_${tier}_PASS`];
  if (!username || !password) fail(`缺少 MELETE_DEBUG_${tier}_USER / _PASS（在 config 私有仓的 melete.env 里）`);
  const res = await fetch(`${base}/auth/password`, {
    method: "POST",
    // 设备名：万一会话没吊销掉，在「登录设备」里一眼认得出是这个脚本留下的
    headers: { "user-agent": "melete-screenshot" },
    body: new URLSearchParams({ username, password, return: "/" }),
    redirect: "manual",
  });
  if (res.status === 404) fail("目标站点没有打开调试账号登录（MELETE_WEB_PASSWORD_LOGIN）");
  const location = res.headers.get("location") ?? "";
  if (res.status !== 303 || location.includes("/auth/login/password")) {
    fail(`登录失败：HTTP ${res.status}${location.includes("error=") ? `（${new URL(location, base).searchParams.get("error")}）` : ""}`);
  }
  return res.headers.getSetCookie().map((c) => {
    const [pair] = c.split(";");
    const i = pair.indexOf("=");
    return { name: pair.slice(0, i), value: pair.slice(i + 1) };
  });
}

/** 起一个无头 Chromium，返回 DevTools 会话。端口由浏览器自己挑（--remote-debugging-port=0），不会撞。 */
async function launch(bin) {
  const profile = mkdtempSync(join(tmpdir(), "melete-shot-"));
  const chrome = spawn(bin, [
    "--headless", "--no-sandbox", "--hide-scrollbars", "--remote-debugging-port=0",
    `--user-data-dir=${profile}`, "about:blank",
  ], { stdio: "ignore" });
  const portFile = join(profile, "DevToolsActivePort");
  for (let i = 0; i < 100 && !existsSync(portFile); i++) await sleep(100);
  if (!existsSync(portFile)) fail("浏览器没有起来");
  const [port] = readFileSync(portFile, "utf8").split("\n");
  const { webSocketDebuggerUrl } = await fetch(`http://127.0.0.1:${port}/json/version`).then((r) => r.json());

  const ws = new WebSocket(webSocketDebuggerUrl);
  await new Promise((r, j) => { ws.onopen = r; ws.onerror = j; });
  let seq = 0;
  const pending = new Map();
  const listeners = [];
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (msg.id && pending.has(msg.id)) {
      const { res, rej } = pending.get(msg.id);
      pending.delete(msg.id);
      msg.error ? rej(new Error(JSON.stringify(msg.error))) : res(msg.result);
    } else listeners.forEach((fn) => fn(msg));
  };
  const send = (method, params = {}, sessionId) => new Promise((res, rej) => {
    const id = ++seq;
    pending.set(id, { res, rej });
    ws.send(JSON.stringify({ id, method, params, sessionId }));
  });
  // ⚠️ 先等浏览器进程真的退出再删资料目录 —— kill 是异步的，它退出前还在往目录里写，
  //   立刻删会 ENOTEMPTY。删不掉也只是留下一个临时目录，⛔ 不该让整个脚本崩掉。
  const close = async () => {
    ws.close();
    const exited = new Promise((r) => chrome.once("exit", r));
    chrome.kill();
    await Promise.race([exited, sleep(5000)]);
    try {
      rmSync(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
    } catch {
      console.error(`  ⚠ 临时目录没删掉：${profile}`);
    }
  };
  return { send, on: (fn) => listeners.push(fn), close };
}

const bin = findChrome();
if (!bin) fail("找不到本机的 Chromium（Playwright 缓存里没有）—— 用 MELETE_SHOT_CHROME 指定；⛔ 本脚本不负责安装");

const session = await login();
const browser = await launch(bin);
const shots = [];
try {
  const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" });
  const { sessionId } = await browser.send("Target.attachToTarget", { targetId, flatten: true });
  const page = (method, params) => browser.send(method, params, sessionId);

  // 页面上的未捕获异常也报出来 —— 截图看着正常、控制台在报错的情况不少见
  browser.on((msg) => {
    if (msg.sessionId === sessionId && msg.method === "Runtime.exceptionThrown") {
      console.error(`  ⚠ 页面异常：${msg.params.exceptionDetails.exception?.description?.split("\n")[0] ?? msg.params.exceptionDetails.text}`);
    }
  });
  await page("Page.enable");
  await page("Runtime.enable");
  await page("Network.enable");
  await page("Network.setCookies", {
    cookies: [
      ...session,
      { name: "melete_theme", value: opt.theme },
      { name: "melete_locale", value: opt.locale },
    ].map((c) => ({ ...c, url: base })),
  });
  await page("Emulation.setDeviceMetricsOverride", { width, height: 900, deviceScaleFactor: 1, mobile: width < 768 });

  mkdirSync(opt.out, { recursive: true });
  for (const p of paths) {
    const loaded = new Promise((r) => {
      browser.on((msg) => { if (msg.sessionId === sessionId && msg.method === "Page.loadEventFired") r(); });
      setTimeout(r, 15000);
    });
    await page("Page.navigate", { url: base + p });
    await loaded;
    await sleep(Number(opt.wait));
    const metrics = await page("Page.getLayoutMetrics");
    const contentHeight = (metrics.cssContentSize ?? metrics.contentSize).height;
    const { data } = await page("Page.captureScreenshot", {
      format: "png",
      captureBeyondViewport: true,
      clip: { x: 0, y: 0, width, height: Math.min(Math.ceil(contentHeight), 4000), scale: 1 },
    });
    const slug = p.replace(/[^a-z0-9]+/gi, "_").replace(/^_|_$/g, "") || "home";
    const file = join(opt.out, `${opt.as}-${opt.theme}-${width}-${slug}.png`);
    writeFileSync(file, Buffer.from(data, "base64"));
    shots.push(file);
    console.log(file);
  }
} finally {
  // 登出放在最前、单独兜底：吊销这次的会话（只要 API 那一步；不跟去第三方账号中心）。
  // 后面关浏览器出什么错，都不该让会话留下来。
  if (session.length) {
    await fetch(`${base}/auth/logout`, {
      headers: { cookie: session.map((c) => `${c.name}=${c.value}`).join("; ") },
      redirect: "manual",
    }).catch(() => console.error("  ⚠ 登出失败：这次的会话留在了「登录设备」里"));
  }
  await browser.close();
}
