import { describe, expect, it } from "vitest";
import { arrangeCategory, FOLD_MIN_MAIN } from "./glossary";
import type { TermSummary } from "./claims";

const term = (id: number, slug: string, questionCount: number, lead = false): TermSummary =>
  ({ id, slug, names: {}, category: "EC2", lead, questionCount });
const nameOf = (x: TermSummary) => x.slug;

describe("arrangeCategory", () => {
  it("主条目置顶，其余按出题数降序，同数按名称", () => {
    const r = arrangeCategory([term(1, "b", 3), term(2, "Amazon EC2", 7, true), term(3, "a", 3), term(4, "c", 9)], nameOf, "en");
    expect(r.lead?.slug).toBe("Amazon EC2");
    expect(r.main.map((x) => x.slug)).toEqual(["c", "a", "b"]);
  });

  it("常考的够多时，只出现 1 次的折叠", () => {
    const many = Array.from({ length: FOLD_MIN_MAIN }, (_, i) => term(10 + i, `f${i}`, 2));
    const r = arrangeCategory([...many, term(1, "once", 1), term(2, "never", 0)], nameOf, "en");
    expect(r.main).toHaveLength(FOLD_MIN_MAIN);
    expect(r.rest.map((x) => x.slug)).toEqual(["once", "never"]);
  });

  it("常考的不够多（IPA 的常态）就不折叠，免得整页折没", () => {
    const r = arrangeCategory([term(1, "x", 2), term(2, "y", 1), term(3, "z", 1)], nameOf, "en");
    expect(r.rest).toEqual([]);
    expect(r.main.map((x) => x.slug)).toEqual(["x", "y", "z"]);
  });
});
