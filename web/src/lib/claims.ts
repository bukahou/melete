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
// 账号设置用（阶段 5 的端点，2026-09-07 接前端）
export type SessionInfo = components["schemas"]["SessionInfo"];
export type PasswordChanged = components["schemas"]["PasswordChanged"];
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
export type ScheduleResult = components["schemas"]["ScheduleResult"];

// ⚠️ 合法值与类型出自同一份数组 —— 曾经是「类型一处、运行时白名单另一处」，
// 加 due 时类型改了而白名单没改，结果是 mode=due 被静默丢弃、悄悄退回全部题目，
// 不报错也没有任何症状。现在漏改一处会编译失败。
export const DRILL_MODES = ["due", "wrong", "unsure", "unseen"] as const;
export type DrillMode = (typeof DRILL_MODES)[number];

/** parseDrillMode 把 URL 参数收敛成合法模式；非法值一律当作「没有模式」。 */
export function parseDrillMode(raw: string | undefined): DrillMode | undefined {
  return (DRILL_MODES as readonly string[]).includes(raw ?? "") ? (raw as DrillMode) : undefined;
}
export type Progress = components["schemas"]["Progress"];
export type TagStat = components["schemas"]["TagStat"];
export type Resume = components["schemas"]["Resume"];
export type FocusCursor = components["schemas"]["FocusCursor"];
export type DrillContext = components["schemas"]["DrillContext"];
export type Overview = components["schemas"]["Overview"];
export type StudySession = components["schemas"]["StudySession"];

// ⚠️ 来源标签（题库标注 / 社区投票 …）已移入 messages 的 `source.*`。
// ⛔ 别在这里放中文常量表 —— 本模块是同构的，客户端组件也 import 它，
// 而 useTranslations 在两侧都能用，没有必要再造一份。

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
// ⚠️ 这张表【不】进 messages，是有意的：它与 tag.i18n / meta.tagTypes[].label
// 属于同一类东西 —— 按 locale 取值的**数据**，而不是界面文案。
// 放进 messages 会让「meta 没给名字」这条回退路径横跨两个体系，查起来更难。
const ROLE_FALLBACK: Record<TagType, Record<string, string>> = {
  domain: { zh: "考纲", ja: "出題分野", en: "Domain" },
  topic: { zh: "主题", ja: "トピック", en: "Topic" },
  concept: { zh: "概念", ja: "概念", en: "Concept" },
};

// ⚠️ locale 【没有】默认值，是有意的：默认成 "zh" 会让漏传的调用点静默显示中文，
// 而那正是 i18n 最难查的一类 bug（页面大半是日文，某一处永远是中文）。
// 现在漏传 = 编译失败 —— 与 DRILL_MODES「漏改一处会编译失败」同一条纪律。
export function tagTypeLabel(meta: BankMeta | undefined, type: TagType, locale: string): string {
  const l = meta?.tagTypes?.[type]?.label;
  return l?.[locale] ?? l?.en ?? ROLE_FALLBACK[type][locale] ?? ROLE_FALLBACK[type].en;
}

/** 某标签的官方权重（百分比）。值以「type-」为前缀存储（如 domain-1），权重表按去前缀的键查。 */
export function tagWeight(meta: BankMeta | undefined, tag: { type: TagType; value: string }): number | undefined {
  const key = tag.value.startsWith(`${tag.type}-`) ? tag.value.slice(tag.type.length + 1) : tag.value;
  return meta?.tagTypes?.[tag.type]?.weights?.[key];
}

/** 标签显示名：优先本地化名（考纲域有 zh/en），否则用 value（服务名本来就是英文短名）。 */
export function tagName(tag: { value: string; i18n?: Record<string, string> | null }, locale: string): string {
  return tag.i18n?.[locale] ?? tag.i18n?.en ?? tag.value;
}
