import Link from "next/link";
import Image from "next/image";
import logo from "../../icon.png";
import { KeyRound } from "lucide-react";
import { getTranslations } from "next-intl/server";

/**
 * 登录页 —— 全站唯一在墙外的页面。
 *
 * ⭐ 2026-10-08 用户裁定：登录只用第三方（Akasha）。首次登录自动注册，之后账号信息不可改，
 *   只能登出再登录 / 换账号。
 *   ⚠️ 撤下的只是【网页上】的密码表单：后端 /auth/password 仍在 —— iOS 调试账号与 dev 测试账号用它，
 *   待确认 iOS 不用后与注册 / 找回 / 改邮箱等端点一起删（见 tracker）。
 */
export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<{ oidc_error?: string; return?: string }>;
}) {
  const sp = await searchParams;
  const t = await getTranslations("login");
  const returnTo = sp.return ?? "/";
  // 原因码由 api 给，文案在这里按白名单映射 —— ⛔ 不回显任何来自 URL 的文字。
  // 白名单换成了消息键，性质不变：URL 里的字符串永远只用来【查表】。
  const OIDC_ERROR: Record<string, string> = {
    cancelled: "oidcCancelled",
    state: "oidcState",
    upstream: "oidcUpstream",
    application: "oidcApplication",
  };
  const oidcMessage = sp.oidc_error
    ? t(OIDC_ERROR[sp.oidc_error] ?? OIDC_ERROR.application)
    : null;

  return (
    // 弹窗：背后的应用外壳由 layout 垫（GuestShell）。⛔ 没有关闭按钮 —— 登录仍是必须的（用户裁定）
    <section role="dialog" aria-modal="true" aria-labelledby="login-title"
             className="w-full max-w-[440px] rounded-2xl bg-raise px-7 pb-7 pt-6 shadow-[0_24px_60px_-20px_rgba(0,0,0,0.35)]">
      <h1 id="login-title" className="text-[1.15rem] font-semibold">{t("modalTitle")}</h1>

      <div className="mt-6 flex items-center gap-3">
        <Image src={logo} alt="" width={40} height={40} priority />
        <div>
          <p className="display text-[1.15rem] leading-tight">Melete</p>
          <p className="text-[0.72rem] text-muted">{t("tagline")}</p>
        </div>
      </div>

      {/* 产品理念只在这里出现：登录后的人不需要每天读一遍 */}
      <p className="mt-4 text-[0.82rem] leading-[1.85] text-muted">
        {t.rich("creed", { em: (c) => <em className="mark-em text-ink">{c}</em> })}
      </p>

      {oidcMessage && (
        <p className="mt-5 text-xs" style={{ color: "var(--color-warn)" }}>{oidcMessage}</p>
      )}
      <Link
        href={`/auth/akasha?return=${encodeURIComponent(returnTo)}`}
        className="mt-6 inline-flex w-full items-center justify-center gap-2 rounded-lg py-3 text-[0.92rem] font-semibold transition-opacity hover:opacity-90"
        style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
      >
        <KeyRound size={15} />
        {t("akasha")}
      </Link>
      <p className="mt-3 text-center text-[0.74rem] text-muted">{t("autoRegister")}</p>

      <p className="mt-6 border-t border-line pt-4 text-center text-[0.72rem] leading-relaxed text-muted">
        {t("copyright")}
      </p>
    </section>
  );
}
