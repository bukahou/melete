/**
 * 题目与答案主张的**类型与展示辅助** —— 同构模块，服务端与客户端都可 import。
 *
 * 与 api.ts 的分工是刻意的：api.ts 带 `server-only`，含内网 API 地址与 fetch；
 * 本模块只有类型和纯函数。客户端组件（DrillCard / QuestionBody）只能碰这一个，
 * 于是「内网地址泄漏进浏览器 bundle」在结构上就不可能发生 ——
 * 而不是依赖打包器 tree-shaking 的侥幸。
 */
import type { components } from "./api.gen";

// 类型直接来自 OpenAPI 契约（npm run gen:api 重新生成）。
// 后端改了契约、前端类型立刻跟着变 —— 这是 Spec-first 的实际收益。
export type Bank = components["schemas"]["Bank"];
export type BankDetail = components["schemas"]["BankDetail"];
export type Tag = components["schemas"]["Tag"];
export type QuestionSummary = components["schemas"]["QuestionSummary"];
export type QuestionDetail = components["schemas"]["QuestionDetail"];
export type QuestionPage = components["schemas"]["QuestionPage"];
export type AnswerClaim = components["schemas"]["AnswerClaim"];
export type Choice = components["schemas"]["Choice"];
export type Reference = components["schemas"]["Reference"];
export type AttemptResult = components["schemas"]["AttemptResult"];

export type DrillMode = "wrong" | "unsure" | "unseen";
export type Progress = components["schemas"]["Progress"];
export type TagStat = components["schemas"]["TagStat"];
export type Resume = components["schemas"]["Resume"];

/** 答案主张的来源标签。顺序与后端返回一致：题库 → 社区 → AI → 用户。 */
export const SOURCE_LABEL: Record<AnswerClaim["source"], string> = {
  bank_label: "题库标注",
  community_vote: "社区投票",
  ai_verdict: "AI 裁决",
  user_note: "我的判断",
};

/** 从社区投票的 meta 里取出票数分布。 */
export function voteDistribution(claim: AnswerClaim): Array<[string, number]> {
  const dist = (claim.meta as { distribution?: Record<string, number> } | undefined)?.distribution;
  return dist ? Object.entries(dist).sort((a, b) => b[1] - a[1]) : [];
}

/**
 * 判断各方主张是否存在分歧。
 * 不做「取最可信的那个」之类的合并 —— 分歧本身就是要呈现给学习者的内容。
 */
export function hasDisagreement(claims: AnswerClaim[]): boolean {
  const answers = new Set(claims.map((c) => c.answer));
  return answers.size > 1;
}

