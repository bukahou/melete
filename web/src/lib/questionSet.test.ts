import { describe, expect, it } from "vitest";
import { bandStats, bandsByDomain, questionLabel } from "./questionSet";

describe("questionLabel：一行「这题考什么」", () => {
  const topics = new Map([[11, "S3"], [12, "CloudFront"]]);
  const name = (id: number) => topics.get(id);

  it("考点按给的顺序（权重）用 · 连起来；考纲域等非考点标签被滤掉", () => {
    // 1 是考纲域：topicName 不认它
    expect(questionLabel([1, 12, 11], name, "一家公司……")).toBe("CloudFront · S3");
  });
  it("没有考点 → 退回题干开头（压掉空白）", () => {
    expect(questionLabel([1], name, "  一家公司\n收集  数据 ")).toBe("一家公司 收集 数据");
    expect(questionLabel(undefined, name, "题干")).toBe("题干");
  });
  it("题干开头最多 90 字", () => {
    expect(questionLabel([], name, "字".repeat(200))).toHaveLength(90);
  });
});

describe("bandsByDomain：每个考纲域连成一片才分段", () => {
  const rows = (ds: Array<number | undefined>) => ds.map((d, i) => ({ i, d }));
  const dom = (r: { d?: number }) => r.d;
  const shape = (bands: Array<{ domain?: number; rows: Array<{ i: number }> }>) =>
    bands.map((b) => [b.domain, b.rows.map((r) => r.i)]);

  it("IPA 的考卷：分野依次连成片 → 按分野分段", () => {
    expect(shape(bandsByDomain(rows([1, 1, 2, 2, 2, 3]), dom))).toEqual([[1, [0, 1]], [2, [2, 3, 4]], [3, [5]]]);
  });
  it("AWS：考纲域穿插 → 整组一段（⛔ 拆成几十截）", () => {
    expect(shape(bandsByDomain(rows([1, 2, 1, 3]), dom))).toEqual([[undefined, [0, 1, 2, 3]]]);
  });
  it("只有一个考纲域（按考纲域点进来）→ 一段", () => {
    expect(shape(bandsByDomain(rows([4, 4, 4]), dom))).toEqual([[undefined, [0, 1, 2]]]);
  });
  it("有题没有考纲域 → 不分段", () => {
    expect(shape(bandsByDomain(rows([1, 1, undefined]), dom))).toEqual([[undefined, [0, 1, 2]]]);
  });
  it("空列表 → 一个空段", () => {
    expect(bandsByDomain([], dom)).toEqual([{ rows: [] }]);
  });
});

describe("bandStats：已答 / 掌握率（每题最近一次）", () => {
  it("没做过的不算进掌握率的分母", () => {
    expect(bandStats([true, false, null, undefined, true])).toEqual({ total: 5, answered: 3, rate: 67 });
  });
  it("一题都没做 → 掌握率为空（界面写 —）", () => {
    expect(bandStats([null, null])).toEqual({ total: 2, answered: 0, rate: null });
  });
});
