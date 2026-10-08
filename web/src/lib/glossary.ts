import type { BankMeta, TermSummary } from "@/lib/claims";
import { localized } from "@/lib/claims";

/**
 * 用语集的目录结构（P9 第 6 步，2026-10-08 用户裁定：照 it-pass，分类成独立页面）。
 *
 *   目录页   大类标题 › 分类行（N 个术语）      /banks/<slug>/glossary
 *   分类页   该分类下的术语                     /banks/<slug>/glossary?c=<分类>
 *   详情页   释义 + 出题历史                    /banks/<slug>/glossary/<id>
 *
 * ⭐ 位置全部在 URL 里 —— 浏览器返回、面包屑、刷新都回到原处（旧版的展开状态只在内存里，返回即丢）。
 * 分类用查询参数而非路径段：AWS 服务名含「/」（VM Import/Export），放进路径段要过一层编码歧义。
 *
 * 大类从哪来：IPA = 考纲树的大分類（topicTree）；AWS = 官方服务类别（topicFamilies）。
 * 都没有 ⇒ 一个无标题的组。不在任何大类里的分类收进「其他」，⛔ 不让它们消失。
 */
export type GlossaryRow = { category: string; label: string; count: number };
export type GlossarySection = { heading?: string; rows: GlossaryRow[] };

export const GENERAL = "General";

/** 术语行的副标题：IPA 给读音；AWS 的显示名与正式名不同时给正式名（同名不重复）。 */
export const termSub = (x: TermSummary, name: string) => x.reading ?? (name !== x.slug ? x.slug : undefined);

export function categoryHref(slug: string, category: string) {
  return `/banks/${slug}/glossary?c=${encodeURIComponent(category)}`;
}

export function categoryLabel(category: string, generalLabel: string) {
  return category === GENERAL ? generalLabel : category;
}

export function glossarySections(meta: BankMeta, terms: TermSummary[], locale: string, labels: { general: string; other: string }): GlossarySection[] {
  const count = new Map<string, number>();
  for (const t of terms) count.set(t.category, (count.get(t.category) ?? 0) + 1);
  const used = new Set<string>();
  const rowsOf = (cats: string[], sort: boolean): GlossaryRow[] => {
    const rows = cats
      .filter((c) => count.has(c) && !used.has(c))
      .map((c) => (used.add(c), { category: c, label: categoryLabel(c, labels.general), count: count.get(c)! }));
    return sort ? rows.sort((a, b) => a.label.localeCompare(b.label, "en")) : rows;
  };

  const sections: GlossarySection[] = [];
  const tree = meta.topicTree ?? [];
  const families = meta.topicFamilies ?? [];
  if (tree.length > 0) {
    for (const d of tree) for (const g of d.groups) sections.push({ heading: g.name, rows: rowsOf(g.topics, false) });
  } else if (families.length > 0) {
    for (const f of families) sections.push({ heading: localized(f.name, locale), rows: rowsOf(f.topics, true) });
  }
  const rest = rowsOf([...count.keys()], true);
  if (rest.length > 0) sections.push({ heading: sections.length > 0 ? labels.other : undefined, rows: rest });
  return sections.filter((s) => s.rows.length > 0);
}
