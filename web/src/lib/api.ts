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
  SessionInfo, PasswordChanged,
} from "./claims";
export { SOURCE_LABEL, voteDistribution, hasDisagreement, DRILL_MODES, parseDrillMode } from "./claims";

import type {
  Bank, BankDetail, Tag, QuestionDetail, QuestionPage, AttemptResult, DrillMode,
  Progress, TagStat, Resume, DrillContext, Overview, StudySession,
  SessionInfo, PasswordChanged,
} from "./claims";

import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { hasRenewMark, logAuth, readAccessToken, safeReturnPath } from "./session";

const BASE = process.env.MELETE_API_BASE ?? "http://localhost:8899/api/v1";

/**
 * 出站请求带 access token。account id 由 API 从 token 的 sub 取 ——
 * web 不参与身份声明，因此也无从冒充他人。
 */
async function authHeaders(): Promise<Record<string, string>> {
  const t = await readAccessToken();
  return t ? { Authorization: `Bearer ${t}` } : {};
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
  const res = await fetch(`${BASE}${path}`, { ...cache, headers: await authHeaders() });
  if (res.status === 401) await recoverFrom401(path);
  if (!res.ok) {
    throw new ApiError(res.status, `GET ${path} → ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export const listBanks = () => get<Bank[]>("/banks");
export const getBank = (slug: string) => get<BankDetail>(`/banks/${slug}`);
export const getQuestion = (id: number) => get<QuestionDetail>(`/questions/${id}`, 300);

export const listBankTags = (slug: string, type?: Tag["type"]) =>
  get<Tag[]>(`/banks/${slug}/tags${type ? `?type=${type}` : ""}`);

export function listQuestions(
  slug: string,
  opts: {
    tags?: number[];
    contested?: boolean;
    enriched?: boolean;
    mode?: DrillMode;
    limit?: number;
    offset?: number;
  } = {},
) {
  const q = new URLSearchParams();
  opts.tags?.forEach((t) => q.append("tag", String(t)));
  if (opts.contested) q.set("contested", "true");
  if (opts.enriched) q.set("enriched", "true");
  if (opts.mode) q.set("mode", opts.mode);
  if (opts.limit != null) q.set("limit", String(opts.limit));
  if (opts.offset != null) q.set("offset", String(opts.offset));
  // 个人化模式（错题本等）走 no-store：结果因人而异
  return get<QuestionPage>(`/banks/${slug}/questions?${q}`, 60, Boolean(opts.mode));
}

/**
 * 服务端调用：记录一次作答（web route handler 专用，带账号头）。
 *
 * ⭐ 2026-09-08 起 rating 是可选的 —— 作答在【揭晓那一刻】就记录，不再等自评。
 * 返回的 attemptId 供随后 rateAttempt() 补自评。
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

/** 服务端调用：给一条已记录的作答补上自评（驱动 FSRS 卡片调度）。 */
export async function rateAttempt(attemptId: number, rating: number): Promise<AttemptResult> {
  const res = await fetch(`${BASE}/attempts/${attemptId}`, {
    method: "PATCH",
    headers: { "content-type": "application/json", ...(await authHeaders()) },
    body: JSON.stringify({ rating }),
  });
  if (!res.ok) throw new ApiError(res.status, `PATCH /attempts/${attemptId} → ${res.status}`);
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

// ---- 账号设置（阶段 5 的端点，2026-09-07 接前端）----
//
// ⚠️ 全部 personalized=true（no-store）：因人而异，⛔ 绝不进共享缓存。
// 会话列表尤其如此 —— 缓存串号意味着看到别人的登录设备。

/** 我的登录设备。⛔ 返回值不含任何 token/hash，那是后端的类型层面保证。 */
export const getSessions = () => get<SessionInfo[]>("/auth/sessions", 0, true);

/** 服务端调用：改密 / 首次设密。⚠️ oldPassword 留空 = 首次设密。 */
export async function changePassword(body: {
  oldPassword?: string;
  newPassword: string;
  deviceInfo?: string;
}): Promise<{ ok: true; data: PasswordChanged; pair?: unknown } | { ok: false; status: number; message: string }> {
  const res = await fetch(`${BASE}/auth/password/change`, {
    method: "POST",
    headers: { "content-type": "application/json", ...(await authHeaders()) },
    body: JSON.stringify(body),
  });
  if (res.ok) return { ok: true, data: (await res.json()) as PasswordChanged };
  // ⚠️ 401（旧密码错）与 400（新密码不合规）必须分开 —— 调用方已通过认证，
  // 混成一个会让他不知道该改什么。⛔ 与登录路径的「不可区分」不冲突：
  // 那里的调用方是未认证的。
  const msg = await res.text().catch(() => "");
  return { ok: false, status: res.status, message: msg };
}

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

/** 服务端调用：给新邮箱发验证码。 */
export async function sendEmailChangeCode(newEmail: string): Promise<number> {
  const res = await fetch(`${BASE}/auth/email/code`, {
    method: "POST",
    headers: { "content-type": "application/json", ...(await authHeaders()) },
    body: JSON.stringify({ newEmail }),
  });
  return res.status;
}

/** 服务端调用：确认改邮箱。 */
export async function confirmEmailChange(code: string, deviceInfo?: string): Promise<number> {
  const res = await fetch(`${BASE}/auth/email/confirm`, {
    method: "POST",
    headers: { "content-type": "application/json", ...(await authHeaders()) },
    body: JSON.stringify({ code, deviceInfo }),
  });
  return res.status;
}

/** 发找回码。⛔ 无论地址存不存在都返回 204 —— 前端不得据此判断账号是否存在。 */
export async function sendRecoveryCode(email: string): Promise<number> {
  const res = await fetch(`${BASE}/auth/recovery/code`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ email }),
  });
  return res.status;
}

/** 用验证码重置密码。 */
export async function completeRecovery(body: {
  email: string;
  code: string;
  newPassword: string;
}): Promise<number> {
  const res = await fetch(`${BASE}/auth/recovery/complete`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  return res.status;
}
