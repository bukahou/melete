import type { DrillContext } from "./claims";
import { parseDrillMode } from "./claims";

/**
 * 出题入口的唯一翻译层（P9 第 4 步）：URL ⇄ DrillContext ⇄ 出题条件。
 *
 * ⭐ DrillContext 就是「一个题目集合」本身 —— 它随作答落库，首页「继续」再从它重建入口
 *   （P9 #13：不存游标表）。所以刷题页、首页、各入口页都只经过这里：
 *   · contextFromSearch  URL → 出处（刷题页）
 *   · drillHref          出处 → URL（入口页 / 继续 / 翻页）
 *   · listParams         出处 → 取题参数（与后端 drillQueryFromContext 是同一张表）
 * ⛔ 任何一处自己拼 URL，「继续」就可能回到别的集合上，而且不报错。
 *
 * 这个文件不 import 服务端模块 —— 客户端组件（DrillCard）也要用它。
 */

/** URL 上的短键。⚠️ i / done 是位置，不属于集合 —— 不进 DrillContext。 */
type Search = Record<string, string | string[] | undefined>;

const one = (v: string | string[] | undefined) => (Array.isArray(v) ? v[0] : v);
const int = (v: string | string[] | undefined) => {
  const n = Number(one(v));
  return Number.isInteger(n) && n > 0 ? n : undefined;
};
const ids = (v: string | string[] | undefined) =>
  (Array.isArray(v) ? v : v ? [v] : [])
    .flatMap((s) => s.split(","))
    .map(Number)
    .filter((n) => Number.isInteger(n) && n > 0);

const STATUSES = ["all", "wrong", "unseen", "bookmarked", "contested"] as const;
type Status = (typeof STATUSES)[number];

/**
 * URL → 出处。认不出的组合一律回到 `{mode:"all"}`（整个题库按题号），⛔ 不报错。
 * 旧链接（`mode=` / `tag=` / `contested=true`）照常解析 —— 学习台还在用它们。
 */
export function contextFromSearch(sp: Search): DrillContext {
  const m = one(sp.m);
  const session = one(sp.s);
  const noFrom = int(sp.from);
  const noTo = int(sp.to);
  const range = {
    ...(session != null ? { session } : {}),
    ...(noFrom ? { noFrom } : {}),
    ...(noTo ? { noTo } : {}),
  };
  const tagIds = ids(sp.t);

  switch (m) {
    case "year":
      if (Object.keys(range).length > 0) return { mode: "year", ...range };
      break;
    case "domain":
      if (tagIds.length > 0) return { mode: "domain", tagIds };
      break;
    case "pick": {
      const st = one(sp.st);
      const status: Status = (STATUSES as readonly string[]).includes(st ?? "") ? (st as Status) : "all";
      return { mode: "pick", status, ...(tagIds.length ? { tagIds } : {}), ...range };
    }
    case "random": {
      const seed = Number(one(sp.seed));
      const count = int(sp.n);
      if (Number.isInteger(seed) && seed >= 0 && count) return { mode: "random", seed, count };
      break;
    }
  }
  // ---- 旧链接 ----
  const legacy = parseDrillMode(one(sp.mode));
  if (legacy) return { mode: legacy };
  const tag = ids(sp.tag)[0];
  if (tag) return { mode: "tag", tagId: tag };
  if (one(sp.contested) === "true") return { mode: "contested" };
  return { mode: "all" };
}

/** 出处 → 刷题页 URL。pos 是位置（i = offset，done = 会缩短的集合里已做几题）。 */
export function drillHref(slug: string, c: DrillContext, pos: { i?: number; done?: number } = {}): string {
  const q = new URLSearchParams();
  switch (c.mode) {
    case "year":
    case "domain":
    case "pick":
    case "random":
      q.set("m", c.mode);
      if (c.session != null) q.set("s", c.session);
      if (c.noFrom) q.set("from", String(c.noFrom));
      if (c.noTo) q.set("to", String(c.noTo));
      if (c.tagIds?.length) q.set("t", c.tagIds.join(","));
      if (c.mode === "pick" && c.status && c.status !== "all") q.set("st", c.status);
      if (c.seed != null) q.set("seed", String(c.seed));
      if (c.count) q.set("n", String(c.count));
      break;
    case "tag":
      if (c.tagId != null) q.set("tag", String(c.tagId));
      break;
    case "contested":
      q.set("contested", "true");
      break;
    case "all":
      break;
    default:
      q.set("mode", c.mode);
  }
  if (pos.i) q.set("i", String(pos.i));
  if (pos.done) q.set("done", String(pos.done));
  const qs = q.toString();
  return `/banks/${slug}/drill${qs ? `?${qs}` : ""}`;
}

/** 出处 → 取题参数。⚠️ 与后端 httpapi/drill.go 的 drillQueryFromContext 是同一张表，两边一起改。 */
export type ListParams = {
  tags?: number[];
  anyTag?: number[];
  contested?: boolean;
  bookmarked?: boolean;
  mode?: "wrong" | "unsure" | "unseen" | "due";
  session?: string;
  noFrom?: number;
  noTo?: number;
  seed?: number;
  take?: number;
};

export function listParams(c: DrillContext): ListParams {
  const range = { session: c.session ?? undefined, noFrom: c.noFrom ?? undefined, noTo: c.noTo ?? undefined };
  switch (c.mode) {
    case "year":
      return range;
    case "domain":
      return { anyTag: c.tagIds ?? [] };
    case "pick": {
      const p: ListParams = { ...range, anyTag: c.tagIds?.length ? c.tagIds : undefined };
      if (c.status === "wrong" || c.status === "unseen") p.mode = c.status;
      if (c.status === "bookmarked") p.bookmarked = true;
      if (c.status === "contested") p.contested = true;
      return p;
    }
    case "random":
      return { seed: c.seed ?? undefined, take: c.count ?? undefined };
    case "tag":
      return { tags: c.tagId != null ? [c.tagId] : [] };
    case "contested":
      return { contested: true };
    case "wrong":
    case "unsure":
    case "unseen":
    case "due":
      return { mode: c.mode };
    default:
      return {};
  }
}

/**
 * 会缩短的集合：做完的题离开集合，答完之后回 offset 0（见 DrillCard 的 nav 注释）。
 * ⭐ 其余集合（按题号 / 按种子）是「一轮」：做到最后给本轮小结（P9 #19）。
 */
export function isShrinking(c: DrillContext): boolean {
  const m = listParams(c).mode;
  return m != null;
}
