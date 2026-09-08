import { describe, expect, it } from "vitest";
import {
  REFRESH_LEEWAY_S,
  decideProxyAction,
  decodeExp,
  isExpiringSoon,
  safeReturnPath,
} from "./session-core";

/**
 * 这一层此前【零测试】。每组都同时断言正反两面 ——
 * 一个从没拒绝过任何东西的门禁等于没有门禁。
 */

function b64url(s: string): string {
  return Buffer.from(s).toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
/** 造一个形状合法、⛔ 签名无效的 JWT —— 本层只读 payload，不验签。 */
function jwtWithExp(exp: unknown): string {
  return `${b64url(JSON.stringify({ alg: "HS256", typ: "JWT" }))}.${b64url(JSON.stringify({ sub: "x", exp }))}.sig`;
}

describe("decodeExp", () => {
  it("读出 payload 里的 exp（阳性对照）", () => {
    expect(decodeExp(jwtWithExp(1700000000))).toBe(1700000000);
  });
  it("base64url 去 padding 的 payload 也能读", () => {
    // 长度取模不同的几种 payload，覆盖 0/1/2 个 '=' 的补齐分支
    for (const exp of [1, 12, 123, 1234, 12345, 123456]) expect(decodeExp(jwtWithExp(exp))).toBe(exp);
  });
  it("不是三段 → null", () => {
    expect(decodeExp("not-a-jwt")).toBeNull();
    expect(decodeExp("a.b")).toBeNull();
    expect(decodeExp("")).toBeNull();
  });
  it("payload 不是 JSON → null", () => {
    expect(decodeExp("x.!!!.y")).toBeNull();
  });
  it("exp 缺失或不是数字 → null", () => {
    expect(decodeExp(`${b64url("{}")}.${b64url("{}")}.s`)).toBeNull();
    expect(decodeExp(jwtWithExp("1700000000"))).toBeNull();
    expect(decodeExp(jwtWithExp(Number.NaN))).toBeNull();
  });
});

describe("isExpiringSoon", () => {
  const now = 1_000_000;
  it("远未过期 → false", () => {
    expect(isExpiringSoon(jwtWithExp(now + 3600), now)).toBe(false);
  });
  it("已过期 → true", () => {
    expect(isExpiringSoon(jwtWithExp(now - 1), now)).toBe(true);
  });
  it("落在 leeway 内 → true（边界：恰好等于 leeway 也算）", () => {
    expect(isExpiringSoon(jwtWithExp(now + REFRESH_LEEWAY_S), now)).toBe(true);
    expect(isExpiringSoon(jwtWithExp(now + REFRESH_LEEWAY_S + 1), now)).toBe(false);
  });
  it("解析失败视为已过期 —— 坏 cookie 该走刷新/登录，不能被当有效放行", () => {
    expect(isExpiringSoon("not-a-valid-token", now)).toBe(true);
  });
});

describe("safeReturnPath —— 开放重定向闸", () => {
  it("同源路径放行（阳性对照）", () => {
    expect(safeReturnPath("/")).toBe("/");
    expect(safeReturnPath("/questions/42?x=1")).toBe("/questions/42?x=1");
  });
  it("空 / 缺失 → fallback", () => {
    expect(safeReturnPath(null)).toBe("/");
    expect(safeReturnPath(undefined)).toBe("/");
    expect(safeReturnPath("")).toBe("/");
    expect(safeReturnPath("", "/me")).toBe("/me");
  });
  it("绝对 URL → 拒", () => {
    expect(safeReturnPath("https://evil.example/")).toBe("/");
    expect(safeReturnPath("evil.example")).toBe("/");
  });
  it("协议相对 URL（// 或 /\\ 开头）→ 拒", () => {
    expect(safeReturnPath("//evil.example/")).toBe("/");
    expect(safeReturnPath("/\\evil.example/")).toBe("/");
  });
  it("含控制字符（CRLF 注入）→ 拒", () => {
    expect(safeReturnPath("/ok\r\nSet-Cookie: x=1")).toBe("/");
  });
});

describe("decideProxyAction —— 决策表全覆盖", () => {
  const now = 1_000_000;
  const valid = jwtWithExp(now + 3600);
  const dying = jwtWithExp(now + 10); // 在 leeway 内
  const dead = jwtWithExp(now - 10);
  const bogus = "not-a-valid-token";

  it("access 有效 → pass（不看 refresh / 不看请求类型）", () => {
    expect(decideProxyAction({ access: valid, refresh: null, isDocument: true, nowS: now })).toBe("pass");
    expect(decideProxyAction({ access: valid, refresh: "rt", isDocument: false, nowS: now })).toBe("pass");
  });

  it("⭐ access 存在但已死 + 有 refresh + 文档导航 → refresh（2026-09-08 故障的修法）", () => {
    expect(decideProxyAction({ access: dead, refresh: "rt", isDocument: true, nowS: now })).toBe("refresh");
    expect(decideProxyAction({ access: bogus, refresh: "rt", isDocument: true, nowS: now })).toBe("refresh");
  });
  it("access 快过期（leeway 内）→ 提前 refresh", () => {
    expect(decideProxyAction({ access: dying, refresh: "rt", isDocument: true, nowS: now })).toBe("refresh");
  });
  it("access 缺失 + 有 refresh + 文档导航 → refresh", () => {
    expect(decideProxyAction({ access: null, refresh: "rt", isDocument: true, nowS: now })).toBe("refresh");
  });

  it("需要刷新但是 RSC/预取 → bounce（⛔ 不并发刷，退化成文档导航）", () => {
    expect(decideProxyAction({ access: null, refresh: "rt", isDocument: false, nowS: now })).toBe("bounce");
    expect(decideProxyAction({ access: dead, refresh: "rt", isDocument: false, nowS: now })).toBe("bounce");
  });

  it("没有 refresh 可用 → login（无论 access 是缺失还是已死）", () => {
    expect(decideProxyAction({ access: null, refresh: null, isDocument: true, nowS: now })).toBe("login");
    expect(decideProxyAction({ access: dead, refresh: null, isDocument: true, nowS: now })).toBe("login");
    expect(decideProxyAction({ access: dead, refresh: null, isDocument: false, nowS: now })).toBe("login");
  });
});
