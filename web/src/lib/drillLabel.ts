import type { BankMeta, DrillContext, Tag } from "./claims";
import { tagName } from "./claims";
import { modeLabel } from "@/i18n/modeLabel";

/**
 * 出处 → 一句人能读的话（首页「继续」、刷题页页头共用）。
 *
 * ⚠️ 出处来自【数据】：标签可能已被重跑富化删掉、卷子名可能没配。
 *   ⛔ 任何一处查不到都不能让整页报错 —— 退回 id / 原文 / 后端给的 mode。
 */
export type LabelKit = {
  meta: BankMeta;
  tags: Tag[];
  locale: string;
  t: (key: string, values?: Record<string, string | number>) => string;
  mode: (key: never) => string;
};

export function sessionLabel(meta: BankMeta, session: string, locale: string): string {
  const names = meta.sessionLabels?.[session];
  return names?.[locale] ?? names?.ja ?? names?.zh ?? session;
}

/** 一组标签 id 的名字：恰好是考纲树里的一个分组就用分组名（选大分類 = 展开成它的中分類）。 */
export function tagSetLabel(ids: number[], k: LabelKit): string {
  const byId = new Map(k.tags.map((t) => [t.id, t]));
  const topicByValue = new Map(k.tags.filter((t) => t.type === "topic").map((t) => [t.value, t.id]));
  const want = [...ids].sort((a, b) => a - b).join(",");
  for (const d of k.meta.topicTree ?? []) {
    for (const g of d.groups) {
      const gids = g.topics.map((v) => topicByValue.get(v)).filter((x): x is number => x != null);
      if (gids.length > 0 && [...gids].sort((a, b) => a - b).join(",") === want) return g.name;
    }
  }
  const names = ids.map((id) => {
    const tag = byId.get(id);
    return tag ? tagName(tag, k.locale) : `#${id}`;
  });
  return names.length > 3 ? k.t("tagsMore", { first: names.slice(0, 2).join("・"), n: names.length }) : names.join("・");
}

export function describeContext(c: DrillContext, k: LabelKit): string {
  const range = () => {
    const parts: string[] = [];
    if (c.session) parts.push(sessionLabel(k.meta, c.session, k.locale));
    if (c.noFrom || c.noTo) parts.push(k.t("rangeLabel", { from: c.noFrom ?? 1, to: c.noTo ?? "…" }));
    return parts.join(" · ");
  };
  switch (c.mode) {
    case "year":
      return range() || k.t("allQuestions");
    case "domain":
      return tagSetLabel(c.tagIds ?? [], k);
    case "pick": {
      const parts = [k.t(`status_${c.status ?? "all"}`)];
      if (c.tagIds?.length) parts.push(tagSetLabel(c.tagIds, k));
      const r = range();
      if (r) parts.push(r);
      return parts.join(" · ");
    }
    case "random":
      return k.t("randomLabel", { n: c.count ?? 0 });
    case "tag": {
      const tag = k.tags.find((t) => t.id === c.tagId);
      return tag ? tagName(tag, k.locale) : modeLabel(k.mode as never, "tag", "tag");
    }
    default:
      return modeLabel(k.mode as never, c.mode, c.mode);
  }
}
