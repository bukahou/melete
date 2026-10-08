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
    <div className="mx-auto flex max-w-sm flex-col items-center pt-14">
      <Image src={logo} alt="" width={56} height={56} priority />
      <h1 className="display mt-5 text-2xl tracking-wide">Melete</h1>
      <p className="mt-2 text-xs tracking-wide text-muted">{t("tagline")}</p>

      {/* 产品理念只在这里出现：登录后的人不需要每天读一遍 */}
      <p className="mt-8 max-w-[19rem] text-center text-[0.8rem] leading-[1.9] text-muted">
        {t.rich("creed", { em: (c) => <em className="mark-em text-ink">{c}</em> })}
      </p>

      {oidcMessage && (
        <p className="mb-3 mt-10 w-full text-xs" style={{ color: "var(--color-warn)" }}>
          {oidcMessage}
        </p>
      )}
      <Link
        href={`/auth/akasha?return=${encodeURIComponent(returnTo)}`}
        className={`inline-flex w-full items-center justify-center gap-2 rounded-md py-2.5 text-sm font-medium transition-opacity hover:opacity-90 ${oidcMessage ? "" : "mt-10"}`}
        style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
      >
        <KeyRound size={14} />
        {t("akasha")}
      </Link>

      <p className="mt-10 max-w-[17rem] text-center text-xs leading-relaxed text-muted">
        {t("copyright")}
      </p>
    </div>
  );
}
