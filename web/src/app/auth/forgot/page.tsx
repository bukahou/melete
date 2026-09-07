import Link from "next/link";
import Image from "next/image";
import logo from "../../icon.png";

export const metadata = { title: "找回密码" };

/**
 * 找回密码 —— 与登录页同在墙外。
 *
 * ⛔⛔ 无论这个邮箱有没有账号，页面反馈【完全一样】。
 * ⚠️ 任何差别（文案、跳转、耗时）都会让这一页变成
 * 「这个邮箱在 melete 有账号吗」的查询接口。
 *
 * ⭐ 而且 Akasha 登录的账号在这里【自然落空】—— 他们没有本应用验证过的邮箱。
 * 案卷 §18.3.2：那是「不实现 = 安全」，⛔ 不是「忘记实现 = 洞」。
 */
export default async function ForgotPage({
  searchParams,
}: { searchParams: Promise<{ sent?: string; n?: string }> }) {
  const sp = await searchParams;
  const sent = sp.sent === "1";
  const bad = sp.n === "bad";
  const rate = sp.n === "rate";

  return (
    <div className="mx-auto flex max-w-sm flex-col items-center pt-14">
      <Image src={logo} alt="" width={48} height={48} priority />
      <h1 className="display mt-4 text-xl tracking-wide">找回密码</h1>

      {sent ? (
        <>
          {/* ⛔ 这段文案对「有账号」与「没账号」是同一份 —— 有意如此。 */}
          <p className="mt-8 max-w-[20rem] text-center text-[0.85rem] leading-[1.9] text-muted">
            如果这个邮箱在 Melete 有账号，验证码已经发过去了。
            <br />
            <span className="text-[0.78rem]">
              ⚠️ 没收到不一定是发错了 —— 用 Akasha 登录的账号没有本站验证过的邮箱，
              这条路对它们不适用。
            </span>
          </p>
          <form method="POST" action="/auth/recover" className="mt-8 w-full space-y-3">
            <input type="hidden" name="step" value="reset" />
            <input name="email" type="email" required placeholder="邮箱"
                   className="w-full rounded-md border border-line bg-surface px-3 py-2.5 text-sm outline-none focus:border-muted" />
            <input name="code" required inputMode="numeric" placeholder="6 位验证码"
                   className="w-full rounded-md border border-line bg-surface px-3 py-2.5 text-sm outline-none focus:border-muted" />
            <input name="password" type="password" required autoComplete="new-password" placeholder="新密码（至少 8 位）"
                   className="w-full rounded-md border border-line bg-surface px-3 py-2.5 text-sm outline-none focus:border-muted" />
            {bad && <p className="text-xs" style={{ color: "var(--color-warn)" }}>验证码无效或已过期，或新密码不合要求。</p>}
            <button type="submit" className="w-full rounded-md px-4 py-2.5 text-sm font-medium"
                    style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}>
              重置密码
            </button>
          </form>
        </>
      ) : (
        <form method="POST" action="/auth/recover" className="mt-10 w-full space-y-3">
          <input type="hidden" name="step" value="send" />
          <input name="email" type="email" required autoComplete="email" placeholder="邮箱"
                 className="w-full rounded-md border border-line bg-surface px-3 py-2.5 text-sm outline-none focus:border-muted" />
          {rate && <p className="text-xs" style={{ color: "var(--color-warn)" }}>发送过于频繁，请稍后再试。</p>}
          <button type="submit" className="w-full rounded-md px-4 py-2.5 text-sm font-medium"
                  style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}>
            发送验证码
          </button>
        </form>
      )}

      <p className="mt-8 text-xs text-muted">
        <Link href="/auth/login" className="underline underline-offset-4">← 回到登录</Link>
      </p>
    </div>
  );
}
