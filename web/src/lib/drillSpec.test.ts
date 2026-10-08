import { describe, expect, it } from "vitest";
import type { DrillContext } from "./claims";
import { contextFromSearch, drillHref, isShrinking, listParams } from "./drillSpec";

/** drillHref 产出的 URL 再解析回来 —— 首页「继续」就是走这一圈。 */
const roundTrip = (c: DrillContext) => {
  const url = new URL(drillHref("x", c, { i: 3 }), "http://h");
  const sp: Record<string, string> = {};
  url.searchParams.forEach((v, k) => (sp[k] = v));
  return contextFromSearch(sp);
};

describe("URL ⇄ 出处 往返不丢东西", () => {
  it.each<[string, DrillContext]>([
    ["4.1 一套卷子", { mode: "year", session: "2026r08" }],
    ["4.1 单套题库的空串卷子 + 题号段", { mode: "year", session: "", noFrom: 101, noTo: 200 }],
    ["4.2 标签并集", { mode: "domain", tagIds: [3, 4, 5] }],
    ["4.3 错题 ∧ 标签 ∧ 卷子", { mode: "pick", status: "wrong", tagIds: [7], session: "s1" }],
    ["4.3 全部（status 缺省回 all）", { mode: "pick", status: "all" }],
    ["4.4 随机", { mode: "random", seed: 12345, count: 10 }],
    ["旧：标签", { mode: "tag", tagId: 9 }],
    ["旧：有分歧", { mode: "contested" }],
    ["旧：没做过", { mode: "unseen" }],
    ["旧：全部", { mode: "all" }],
  ])("%s", (_, c) => {
    expect(roundTrip(c)).toEqual(c);
  });
});

describe("认不出的 URL 不报错，回到整个题库", () => {
  it.each([
    [{ m: "year" }, "year 没有卷子也没有题号段"],
    [{ m: "domain" }, "domain 没有标签"],
    [{ m: "random", seed: "1" }, "random 缺题数"],
    [{ m: "random", seed: "-1", n: "10" }, "负种子"],
    [{ m: "nope" }, "未知入口"],
    [{ t: "abc" }, "脏标签"],
  ] as Array<[Record<string, string>, string]>)("%j（%s）", (sp) => {
    expect(contextFromSearch(sp)).toEqual({ mode: "all" });
  });
});

describe("出处 → 取题参数（与后端 drillQueryFromContext 同表）", () => {
  it("pick 的状态分别落到 mode / bookmarked / contested", () => {
    expect(listParams({ mode: "pick", status: "wrong" }).mode).toBe("wrong");
    expect(listParams({ mode: "pick", status: "unseen" }).mode).toBe("unseen");
    expect(listParams({ mode: "pick", status: "bookmarked" }).bookmarked).toBe(true);
    expect(listParams({ mode: "pick", status: "contested" }).contested).toBe(true);
  });
  it("domain 用并集（anyTag），旧 tag 用交集（tags）—— 两个参数不能混", () => {
    expect(listParams({ mode: "domain", tagIds: [1, 2] })).toEqual({ anyTag: [1, 2] });
    expect(listParams({ mode: "tag", tagId: 1 })).toEqual({ tags: [1] });
  });
  it("random = 种子 + 前 N 题", () => {
    expect(listParams({ mode: "random", seed: 5, count: 20 })).toEqual({ seed: 5, take: 20 });
  });
});

describe("会缩短的集合", () => {
  it.each<[DrillContext, boolean]>([
    [{ mode: "pick", status: "wrong" }, true],
    [{ mode: "pick", status: "unseen" }, true],
    [{ mode: "unseen" }, true],
    [{ mode: "due" }, true],
    [{ mode: "pick", status: "bookmarked" }, false],
    [{ mode: "year", session: "s" }, false],
    [{ mode: "random", seed: 1, count: 10 }, false],
  ])("%j → %s", (c, want) => {
    expect(isShrinking(c)).toBe(want);
  });
});
