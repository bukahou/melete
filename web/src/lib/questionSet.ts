/**
 * 题目列表的两条规则（P9 #44）—— 列表页（历年 / 分组、按考纲域点进来那一页）与「自选条件」共用。
 * 这个文件不 import 服务端模块：客户端组件（PickBrowser）也要用。
 */

/**
 * 一行「这题考什么」：考点名（AWS = 服务，IPA = 中分類）按权重用「 · 」连起来；没有考点退回题干开头。
 * ⭐ 为什么不用题干：AWS 的题干几乎都以「一家公司…」开头，一列看下来认不出哪道是哪道。
 * topicName 只认考点 —— 考纲域等别的标签返回 undefined，自然被滤掉。
 */
export function questionLabel(
  tagIds: readonly number[] | undefined,
  topicName: (id: number) => string | undefined,
  stem: string,
): string {
  const names = (tagIds ?? []).map(topicName).filter((x): x is string => Boolean(x));
  return names.length > 0 ? names.join(" · ") : stem.replace(/\s+/g, " ").trim().slice(0, 90);
}

/**
 * 分段：按题号顺序走一遍，每个考纲域都连成一片（且不止一片）⇒ 按考纲域分段；否则整组一段。
 * ⭐ IPA 的考卷就是按分野排的（问 1–35 ストラテジ …），分段 = 它本来的结构；
 *   AWS 的考纲域在题号上是穿插的，硬分段会把一组拆成几十截 —— 那时一段就好。
 * ⛔ 不按题库名判断：规则只看数据本身。
 */
export function bandsByDomain<T>(rows: readonly T[], domainOf: (row: T) => number | undefined): Array<{ domain?: number; rows: T[] }> {
  const runs: Array<{ domain?: number; rows: T[] }> = [];
  for (const row of rows) {
    const d = domainOf(row);
    const last = runs.at(-1);
    if (last && last.domain === d) last.rows.push(row);
    else runs.push({ domain: d, rows: [row] });
  }
  const contiguous = runs.length > 1
    && runs.every((r) => r.domain != null)
    && new Set(runs.map((r) => r.domain)).size === runs.length;
  return contiguous ? runs : [{ rows: [...rows] }];
}

/** 一段（或整组）的「已答 / 掌握率」：已答 = 做过的题，掌握率 = 其中最近一次答对的（#9 的口径）。 */
export function bandStats(lasts: ReadonlyArray<boolean | null | undefined>): { total: number; answered: number; rate: number | null } {
  const answered = lasts.filter((x) => x != null).length;
  const correct = lasts.filter((x) => x === true).length;
  return { total: lasts.length, answered, rate: answered > 0 ? Math.round((correct / answered) * 100) : null };
}
