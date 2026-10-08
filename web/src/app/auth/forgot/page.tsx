import Link from "next/link";
import Image from "next/image";
import logo from "../../icon.png";
import { getTranslations } from "next-intl/server";

export async function generateMetadata() {
  return { title: (await getTranslations("forgot"))("title") };
}

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
  const t = await getTranslations("forgot");
  const sent = sp.sent === "1";
  const bad = sp.n === "bad";
  const rate = sp.n === "rate";

  return (
    <div className="mx-auto flex max-w-sm flex-col items-center pt-14">
      <Image src={logo} alt="" width={48} height={48} priority />
      <h1 className="display mt-4 text-xl tracking-wide">{t("title")}</h1>

      {sent ? (
        <>
          {/* ⛔ 这段文案对「有账号」与「没账号」是同一份 —— 有意如此。 */}
          <p className="mt-8 max-w-[20rem] text-center text-[0.85rem] leading-[1.9] text-muted">
            {t("sent")}
            <br />
            <span className="text-[0.78rem]">{t("sentNote")}</span>
          </p>
          <form method="POST" action="/auth/recover" className="mt-8 w-full space-y-3">
            <input type="hidden" name="step" value="reset" />
            <input name="email" type="email" required placeholder={t("email")}
                   className="w-full rounded-md border border-line bg-surface px-3 py-2.5 text-sm outline-none focus:border-muted" />
            <input name="code" required inputMode="numeric" placeholder={t("code")}
                   className="w-full rounded-md border border-line bg-surface px-3 py-2.5 text-sm outline-none focus:border-muted" />
            <input name="password" type="password" required autoComplete="new-password" placeholder={t("newPassword")}
                   className="w-full rounded-md border border-line bg-surface px-3 py-2.5 text-sm outline-none focus:border-muted" />
            {bad && <p className="text-xs" style={{ color: "var(--color-warn)" }}>{t("bad")}</p>}
            <button type="submit" className="w-full rounded-md px-4 py-2.5 text-sm font-medium"
                    style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}>
              {t("reset")}
            </button>
          </form>
        </>
      ) : (
        <form method="POST" action="/auth/recover" className="mt-10 w-full space-y-3">
          <input type="hidden" name="step" value="send" />
          <input name="email" type="email" required autoComplete="email" placeholder={t("email")}
                 className="w-full rounded-md border border-line bg-surface px-3 py-2.5 text-sm outline-none focus:border-muted" />
          {rate && <p className="text-xs" style={{ color: "var(--color-warn)" }}>{t("rate")}</p>}
          <button type="submit" className="w-full rounded-md px-4 py-2.5 text-sm font-medium"
                  style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}>
            {t("send")}
          </button>
        </form>
      )}

      <p className="mt-8 text-xs text-muted">
        <Link href="/auth/login" className="underline underline-offset-4">{t("backToLogin")}</Link>
      </p>
    </div>
  );
}
