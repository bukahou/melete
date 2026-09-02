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
export type BankMeta = components["schemas"]["BankMeta"];
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
export type FocusCursor = components["schemas"]["FocusCursor"];
export type DrillContext = components["schemas"]["DrillContext"];

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


export type TagType = Tag["type"];

/**
 * 标签轴的**角色**是通用的（domain = 考纲 / topic = 知识对象 / concept = 原理），
 * **名字**不是：AWS 的 topic 叫「服务」，LPIC 叫「命令与工具」，Java 叫「API」。
 * 名字由题库 meta 给；这里只兜底 —— meta 缺失时用角色的通用名，
 * 绝不出现「服务」「AWS」这类题库词。换题库时页面代码零改动。
 */
const ROLE_FALLBACK: Record<TagType, Record<string, string>> = {
  domain: { zh: "考纲", en: "Domain" },
  topic: { zh: "主题", en: "Topic" },
  concept: { zh: "概念", en: "Concept" },
};

export function tagTypeLabel(meta: BankMeta | undefined, type: TagType, locale = "zh"): string {
  const l = meta?.tagTypes?.[type]?.label;
  return l?.[locale] ?? l?.en ?? ROLE_FALLBACK[type][locale] ?? ROLE_FALLBACK[type].en;
}

/** 某标签的官方权重（百分比）。值以「type-」为前缀存储（如 domain-1），权重表按去前缀的键查。 */
export function tagWeight(meta: BankMeta | undefined, tag: { type: TagType; value: string }): number | undefined {
  const key = tag.value.startsWith(`${tag.type}-`) ? tag.value.slice(tag.type.length + 1) : tag.value;
  return meta?.tagTypes?.[tag.type]?.weights?.[key];
}

/** 标签显示名：优先本地化名（考纲域有 zh/en），否则用 value（服务名本来就是英文短名）。 */
export function tagName(tag: { value: string; i18n?: Record<string, string> | null }, locale = "zh"): string {
  return tag.i18n?.[locale] ?? tag.i18n?.en ?? tag.value;
}
