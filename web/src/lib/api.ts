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
} from "./claims";
export { SOURCE_LABEL, voteDistribution, hasDisagreement, DRILL_MODES, parseDrillMode } from "./claims";

import type {
  Bank, BankDetail, Tag, QuestionDetail, QuestionPage, AttemptResult, DrillMode,
  Progress, TagStat, Resume, DrillContext, Overview, StudySession,
} from "./claims";

import { accessToken } from "./auth";

const BASE = process.env.MELETE_API_BASE ?? "http://localhost:8899/api/v1";

/**
 * 出站请求带 access token。account id 由 API 从 token 的 sub 取 ——
 * web 不参与身份声明，因此也无从冒充他人。
 */
async function authHeaders(): Promise<Record<string, string>> {
  const t = await accessToken();
  return t ? { Authorization: `Bearer ${t}` } : {};
}

export class ApiError extends Error {
  constructor(readonly status: number, message: string) {
    super(message);
  }
}

async function get<T>(path: string, revalidate = 60, personalized = false): Promise<T> {
  // 个人化数据绝不进共享缓存；且带 Authorization 的请求本就不该被缓存复用
  const cache = personalized ? { cache: "no-store" as const } : { next: { revalidate } };
  const res = await fetch(`${BASE}${path}`, { ...cache, headers: await authHeaders() });
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

/** 服务端调用：记录一次作答（web route handler 专用，带账号头）。 */
export async function recordAttempt(
  body: { questionId: number; chosen: string; rating: number; durationMs?: number; context?: DrillContext },
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
