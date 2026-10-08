import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { LOCALES, isLocale, needsSourceNotice, pickLocale } from "./locales";

describe("pickLocale", () => {
  it.each([
    ["", null, "没有头 = 不猜，交给默认语言"],
    ["ja", "ja", "最简单的情形"],
    ["ja-JP", "ja", "地区子标签退到主语言"],
    ["ko", null, "白名单外的语言不接受"],
    ["ko,ja;q=0.8", "ja", "跳过不支持的，取次优"],
    ["zh;q=0.5,ja;q=0.9", "ja", "q 值决定顺序，⛔ 不是书写顺序"],
    ["*", null, "通配不算表达偏好"],
    ["JA", "ja", "大小写不敏感"],
    ["ja;q=oops", "ja", "q 解析失败按 1.0 —— 脏参数不该让整个语言消失"],
  ] as Array<[string, string | null, string]>)("Accept-Language %j → %j（%s）", (header, want) => {
    expect(pickLocale(header)).toBe(want);
  });

  // ⚠️ 与后端 internal/httplocale 的 pickLocale 是同一套规则的第二份实现。
  // 这些用例与那边的 TestPickLocale 刻意保持一致 —— 两边一起改，或者一起红。
  it("与后端同规则：zh-CN 落到 zh", () => {
    expect(pickLocale("zh-CN,zh;q=0.9,en;q=0.8")).toBe("zh");
  });
});

describe("messages 目录", () => {
  const load = (l: string) =>
    JSON.parse(readFileSync(new URL(`../messages/${l}.json`, import.meta.url), "utf8"));

  const keys = (o: Record<string, unknown>, p = ""): string[] =>
    Object.entries(o).flatMap(([k, v]) =>
      v && typeof v === "object" && !Array.isArray(v)
        ? keys(v as Record<string, unknown>, `${p}${k}.`)
        : [`${p}${k}`],
    );

  // ⭐ 这条测试的价值不在「翻译对不对」（那要人看），而在
  //   **加了一句中文却忘了加日文** —— 那种漏在界面上表现为「大半是日文，某处是中文」，
  //   而 request.ts 的兜底合并会让它【不报错】地过去。这里让它编译期之后立刻红。
  it("每种语言的键完全一致", () => {
    const base = keys(load("zh")).sort();
    for (const l of LOCALES) {
      expect(keys(load(l)).sort(), `${l}.json 的键与 zh.json 不一致`).toEqual(base);
    }
  });

  it("占位符也一致 —— 少一个 {n} 就是少一个数字", () => {
    const ph = (s: string) => (s.match(/\{[a-zA-Z]+\}/g) ?? []).sort().join(",");
    const flat = (l: string) => {
      const out: Record<string, string> = {};
      const walk = (o: Record<string, unknown>, p = "") => {
        for (const [k, v] of Object.entries(o)) {
          if (v && typeof v === "object") walk(v as Record<string, unknown>, `${p}${k}.`);
          else out[`${p}${k}`] = String(v);
        }
      };
      walk(load(l));
      return out;
    };
    const zh = flat("zh");
    for (const l of LOCALES) {
      if (l === "zh") continue;
      const other = flat(l);
      for (const k of Object.keys(zh)) {
        expect(ph(other[k]), `${l}.json 的 ${k} 占位符与 zh 不一致`).toBe(ph(zh[k]));
      }
    }
  });
});

describe("isLocale", () => {
  it("挡住任意字符串 —— locale 会进 SQL 参数与 cookie", () => {
    expect(isLocale("ja")).toBe(true);
    expect(isLocale("ja; DROP")).toBe(false);
    expect(isLocale("")).toBe(false);
    expect(isLocale(undefined)).toBe(false);
  });
});

describe("needsSourceNotice", () => {
  it("请求 ja、题库源语言 zh、没有译文 → 提示", () => {
    expect(needsSourceNotice("zh", "ja", false)).toBe(true);
  });
  it("有译文 → 不提示", () => {
    expect(needsSourceNotice("zh", "ja", true)).toBe(false);
  });
  // ⚠️ 这条是真正容易错的：请求的就是源语言时 localized 也是 false，
  // 只看它就会对着【正确的原文】喊「本题暂无该语言版本」。
  it("请求的就是源语言 → ⛔ 不提示", () => {
    expect(needsSourceNotice("zh", "zh", false)).toBe(false);
  });
  it("拿不到源语言 → 宁可漏提示，也不误报", () => {
    expect(needsSourceNotice(undefined, "ja", false)).toBe(false);
    expect(needsSourceNotice(null, "ja", undefined)).toBe(false);
  });
});
