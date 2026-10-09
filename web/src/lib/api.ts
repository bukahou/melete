import "server-only";

/**
 * 内网 API 客户端 —— **仅服务端**。
 *
 * `server-only` 让任何客户端组件 import 它时**构建期直接失败**，
 * 而不是等到内网地址已经进了浏览器 bundle 才发现。
 *
 * 架构前提（见 docs/design/active/deployment.md §1①）：
 * 浏览器只跟 web 说话，web 在集群内经 ClusterIP 调 api。
 * 因此 api 不对公网暴露、不需要 CORS，也不该有任何客户端代码知道它的地址。
 */

// 类型与展示辅助统一由 claims.ts 定义，此处再导出 ——
// 服务端组件 import 一处即可，不必关心哪些能给客户端用。
// 类型必须用 `export type` 显式列出：isolatedModules 下 `export *` 不透传类型。
export type {
  Bank, BankDetail, Tag, QuestionSummary, QuestionDetail, QuestionPage,
  AnswerClaim, Choice, Reference, AttemptResult, ScheduleResult, DrillMode,
  Progress, TagStat, Resume, FocusCursor, DrillContext, Overview, StudySession,
  SessionInfo, MyAccess, MyProfile, AdminUser, AdminUserPage, Tier, CurrentBank, SetSummary, DrillCursor, BankSession, TermSummary, TermDetail,
} from "./claims";
export { voteDistribution, hasDisagreement, DRILL_MODES, parseDrillMode } from "./claims";

import type {
  Bank, BankDetail, Tag, QuestionDetail, QuestionPage, AttemptResult, DrillMode,
  Progress, TagStat, Resume, DrillContext, Overview, StudySession,
  SessionInfo, MyAccess, MyProfile, AdminUserPage, CurrentBank, SetSummary, TermSummary, TermDetail,
} from "./claims";

import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { hasRenewMark, logAuth, readAccessToken, safeReturnPath } from "./session";
import { resolveLocale } from "@/i18n/resolve";
import type { ListParams } from "./drillSpec";

const BASE = process.env.MELETE_API_BASE ?? "http://localhost:8899/api/v1";

/**
 * 出站请求带 access token。account id 由 API 从 token 的 sub 取 ——
 * web 不参与身份声明，因此也无从冒充他人。
 */
async function authHeaders(): Promise<Record<string, string>> {
  const t = await readAccessToken();
  return t ? { Authorization: `Bearer ${t}` } : {};
}

/**
 * 出站请求带上界面语言 —— 题目正文、选项跟着界面走。
 *
 * ⭐ 用 Accept-Language 而不是给每个函数加 locale 参数：一处加，全部端点受益，
 * ⛔ 而且不会出现「某个端点忘了传」这种只在特定页面显形的漏。
 *
 * ⚠️ 它同时进入 Next 的 fetch 缓存键（缓存键含 headers）——
 * 这正是需要的：中文与日文的响应⛔不得互相复用。
 */
async function localeHeaders(): Promise<Record<string, string>> {
  return { "Accept-Language": await resolveLocale() };
}

async function requestHeaders(): Promise<Record<string, string>> {
  return { ...(await authHeaders()), ...(await localeHeaders()) };
}

export class ApiError extends Error {
  constructor(readonly status: number, message: string) {
    super(message);
  }
}

/**
 * 反应式续期 —— geass 客户端拦截器「401 → refresh → 重试」的服务端等价物。
 *
 * proxy 是预判（读 exp 提前刷）；这里是兜底：时钟偏差、API 密钥轮换、会话被吊销……
 * 任何 proxy 猜错的情况，API 的 401 都在这里被接住，⛔ 不再变成 500 错误页
 * （2026-09-08 digest 2012285127 正是这条路缺失的表现）。
 *
 * 只用于 Server Component 的渲染路径（get 系列）。Route Handler 返回状态码，⛔ 别在那里调。
 * `redirect()` 靠抛出 NEXT_REDIRECT 生效 —— 调用方⛔不得 try/catch 包住 get()。
 */
async function recoverFrom401(path: string): Promise<never> {
  const back = safeReturnPath((await headers()).get("x-pathname"));
  if (await hasRenewMark()) {
    // 刚 renew 过（30s 内）仍 401 ⇒ 换到新 token 也不被接受（账号停用 / 密钥轮换…）
    // ⇒ 熔断：登出，别在 renew ↔ 401 之间转圈
    logAuth("api.401.loop", { path, back });
    redirect("/auth/logout");
  }
  logAuth("api.401", { path, back });
  redirect(`/auth/renew?return=${encodeURIComponent(back)}`);
}

async function get<T>(path: string, revalidate = 60, personalized = false): Promise<T> {
  // 个人化数据绝不进共享缓存；且带 Authorization 的请求本就不该被缓存复用
  const cache = personalized ? { cache: "no-store" as const } : { next: { revalidate } };
  const res = await fetch(`${BASE}${path}`, { ...cache, headers: await requestHeaders() });
  if (res.status === 401) await recoverFrom401(path);
  if (!res.ok) {
    throw new ApiError(res.status, `GET ${path} → ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export const listBanks = () => get<Bank[]>("/banks");
export const getBank = (slug: string) => get<BankDetail>(`/banks/${slug}`);
export const getQuestion = (id: number) => get<QuestionDetail>(`/questions/${id}`, 300);
/** 用语集目录：内容侧、与用户无关 —— 可以缓存（术语只在重导时变）。 */
export const listTerms = (slug: string) => get<TermSummary[]>(`/banks/${slug}/terms`, 300);
export const getTerm = (id: number) => get<TermDetail>(`/terms/${id}`, 300);

export const listBankTags = (slug: string, type?: Tag["type"]) =>
  get<Tag[]>(`/banks/${slug}/tags${type ? `?type=${type}` : ""}`);

/**
 * 出题入口用的两轴标签（domain + topic）。
 * ⚠️ 不带 type 的 listBankTags 会连全局 concept 一起返回（AWS 题库数百个）——
 *   入口与刷题页只需要两轴，⛔ 别为一个标签名把几百行拉过来。
 */
export const listAxisTags = async (slug: string) =>
  (await Promise.all([listBankTags(slug, "domain"), listBankTags(slug, "topic")])).flat();

export function listQuestions(
  slug: string,
  opts: ListParams & { enriched?: boolean; limit?: number; offset?: number } = {},
) {
  const q = drillQuery(opts);
  if (opts.enriched) q.set("enriched", "true");
  if (opts.limit != null) q.set("limit", String(opts.limit));
  if (opts.offset != null) q.set("offset", String(opts.offset));
  // 个人化条件（错题本 / 收藏等）走 no-store：结果因人而异
  return get<QuestionPage>(`/banks/${slug}/questions?${q}`, 60, Boolean(opts.mode || opts.bookmarked));
}

/** 一个题目集合的本轮小结（P9 #19）：多大、做过几道、最近一次答对几道。条件与 listQuestions 同一套。 */
export const summarizeQuestions = (slug: string, opts: ListParams) =>
  get<SetSummary>(`/banks/${slug}/questions/summary?${drillQuery(opts)}`, 0, true);

/** ListParams → 查询串。⚠️ 两个标签参数：tag = 交集（旧入口），anyTag = 并集（P9）。 */
function drillQuery(o: ListParams): URLSearchParams {
  const q = new URLSearchParams();
  o.tags?.forEach((t) => q.append("tag", String(t)));
  o.anyTag?.forEach((t) => q.append("anyTag", String(t)));
  if (o.contested) q.set("contested", "true");
  if (o.bookmarked) q.set("bookmarked", "true");
  if (o.mode) q.set("mode", o.mode);
  if (o.session != null) q.set("session", o.session);
  if (o.noFrom) q.set("noFrom", String(o.noFrom));
  if (o.noTo) q.set("noTo", String(o.noTo));
  if (o.seed != null) q.set("seed", String(o.seed));
  if (o.take) q.set("take", String(o.take));
  return q;
}

/**
 * 服务端调用：记录一次作答（web route handler 专用，带账号头）。
 *
 * ⭐ 2026-09-08 起 rating 是可选的 —— 作答在【揭晓那一刻】就记录，不再等自评。
 * ⚠️ 2026-10-08 起界面不再补自评（P9 #15），后端按对错代打分喂给 FSRS。
 */
export async function recordAttempt(
  body: { questionId: number; chosen: string; rating?: number; durationMs?: number; context?: DrillContext },
): Promise<AttemptResult> {
  const res = await fetch(`${BASE}/attempts`, {
    method: "POST",
    headers: { "content-type": "application/json", ...(await authHeaders()) },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new ApiError(res.status, `POST /attempts → ${res.status}`);
  return res.json();
}




// ---- 个人统计（都是 no-store：因人而异，绝不进共享缓存）----

const bankQuery = (bank?: string) => (bank ? `?bank=${encodeURIComponent(bank)}` : "");
export const getMyProgress = (bank?: string) => get<Progress>(`/me/progress${bankQuery(bank)}`, 0, true);
export const getMyResume = (bank?: string) => get<Resume>(`/me/resume${bankQuery(bank)}`, 0, true);
export const getMyTagStats = (type: Tag["type"], minAttempts = 3, bank?: string) =>
  get<TagStat[]>(`/me/tag-stats?type=${type}&minAttempts=${minAttempts}${bank ? `&bank=${encodeURIComponent(bank)}` : ""}`, 0, true);
export const getMyOverview = () => get<Overview>("/me/overview", 0, true);
export const getMyRecent = (limit = 5) => get<StudySession[]>(`/me/recent?limit=${limit}`, 0, true);

/** 服务端调用：收藏 / 取消收藏（P9 #20）。两个方向都幂等。 */
export async function setBookmark(questionId: number, on: boolean): Promise<{ ok: boolean; status: number }> {
  const res = await fetch(`${BASE}/me/bookmarks/${questionId}`, {
    method: on ? "PUT" : "DELETE",
    headers: await authHeaders(),
  });
  return { ok: res.ok, status: res.status };
}

/** 当前题库（P9）：chosen 设置里选的 · recent 按最近作答推出 · none 都没有。 */
export const getMyBank = () => get<CurrentBank>("/me/bank", 0, true);

/**
 * 首页与各入口（用语集 / 学习履历 / 书签）用的「现在学哪个题库」：
 * 当前题库；没有（新用户 / 被降级后原来的看不到了）且看得到的题库只有一个 ⇒ 就是它；否则 undefined（引导去选）。
 * ⭐ App Store 来的陌生人注册完打开就该是公开题库（P9 #27）。
 */
export async function resolveStudyBank(): Promise<string | undefined> {
  const cur = await getMyBank();
  if (cur.source !== "none" && cur.bankSlug) return cur.bankSlug;
  const visible = await listBanks();
  return visible.length === 1 ? visible[0].slug : undefined;
}

/** 服务端调用：切换当前题库（设置页表单 → BFF 路由 → 这里）。 */
export async function chooseMyBank(bankSlug: string): Promise<{ ok: true } | { ok: false; status: number }> {
  const res = await fetch(`${BASE}/me/bank`, {
    method: "PUT",
    headers: { "content-type": "application/json", ...(await authHeaders()) },
    body: JSON.stringify({ bankSlug }),
  });
  return res.ok ? { ok: true } : { ok: false, status: res.status };
}

// ---- 档位与用户管理（P9 #27，设计见 docs/design/active/bank-access.md）----
//
// ⚠️ 全部 personalized=true（no-store）：档位因人而异，升降级要立刻生效。

/** 我的档位：普通 / 高级 / admin。界面只用它决定显不显示管理入口 —— 门在后端。 */
export const getMyAccess = () => get<MyAccess>("/me/access", 0, true);

/** 我的资料（「我的」页面、侧栏头像）：显示名 · 邮箱 · 头像 · 档位 · 注册 / 最后登录。来自第三方账号，Melete 里不能改。 */
export const getMyProfile = () => get<MyProfile>("/me/profile", 0, true);

/** admin：用户列表。非 admin 拿到 404。 */
export const listAdminUsers = (page = 1) => get<AdminUserPage>(`/admin/users?page=${page}&pageSize=50`, 0, true);

/** 服务端调用：升级 / 降级（admin 页面表单 → BFF 路由 → 这里）。409 = 目标是 admin。 */
export async function setUserTier(userId: string, tier: "basic" | "advanced"): Promise<{ ok: true } | { ok: false; status: number }> {
  const res = await fetch(`${BASE}/admin/users/${encodeURIComponent(userId)}/tier`, {
    method: "PUT",
    headers: { "content-type": "application/json", ...(await authHeaders()) },
    body: JSON.stringify({ tier }),
  });
  return res.ok ? { ok: true } : { ok: false, status: res.status };
}

// ---- 账号设置（阶段 5 的端点，2026-09-07 接前端）----
//
// ⭐ 2026-10-08 用户裁定：登录只用 Akasha，账号信息不再可改 ⇒ 改密 / 改邮箱 / 找回的封装已删，
//   只剩登录设备两项。后端端点暂留（待确认 iOS 不用后再删，见 tracker）。
//
// ⚠️ 全部 personalized=true（no-store）：因人而异，⛔ 绝不进共享缓存。
// 会话列表尤其如此 —— 缓存串号意味着看到别人的登录设备。

/** 我的登录设备。⛔ 返回值不含任何 token/hash，那是后端的类型层面保证。 */
export const getSessions = () => get<SessionInfo[]>("/auth/sessions", 0, true);

/** 服务端调用：登出其它设备。keepSessionID 由后端从 token 的 sid 取，⛔ 前端不传。 */
export async function revokeOtherSessions(): Promise<{ ok: boolean; revoked?: number; status: number }> {
  const res = await fetch(`${BASE}/auth/sessions/revoke-others`, {
    method: "POST",
    headers: { "content-type": "application/json", ...(await authHeaders()) },
  });
  if (!res.ok) return { ok: false, status: res.status };
  const body = (await res.json()) as { revoked: number };
  return { ok: true, revoked: body.revoked, status: res.status };
}
