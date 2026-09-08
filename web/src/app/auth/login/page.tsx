import Link from "next/link";
import Image from "next/image";
import logo from "../../icon.png";
import { KeyRound } from "lucide-react";
import { getTranslations } from "next-intl/server";

/**
 * 登录页 —— 全站唯一在墙外的页面。
 * 双入口：本地密码（melete 自持的账号体系）+ Akasha 联邦。
 * Akasha 按其定案不做密码认证，密码归各接入应用自持，与 geass-v3 同模式。
 */
export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<{ error?: string; oidc_error?: string; return?: string }>;
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

      <form method="POST" action="/auth/password" className="mt-10 w-full space-y-3">
        <input type="hidden" name="return" value={returnTo} />
        <input
          name="username"
          required
          autoComplete="username"
          placeholder={t("username")}
          className="w-full rounded-md border border-line bg-raise px-3.5 py-2.5 text-sm outline-none transition-colors placeholder:text-muted focus:border-muted"
        />
        <input
          name="password"
          type="password"
          required
          autoComplete="current-password"
          placeholder={t("password")}
          className="w-full rounded-md border border-line bg-raise px-3.5 py-2.5 text-sm outline-none transition-colors placeholder:text-muted focus:border-muted"
        />
        {sp.error && (
          <p className="text-xs" style={{ color: "var(--color-warn)" }}>
            {t("badCredentials")}
          </p>
        )}
        <button
          type="submit"
          className="w-full rounded-md py-2.5 text-sm font-medium"
          style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
        >
          {t("submit")}
        </button>
      </form>

      {/* ⛔ 找回密码入口【暂时撤下】(2026-09-07)：生产的发信通道尚是 log 型
          (验证码不经安全信道投递)，在有真实邮件通道之前，找回流程不对外开放
          —— 后端也已把 /auth/recovery/* 移出免认证白名单，两处同时改。
          ⭐ 恢复时：这里放回 <Link href="/auth/forgot">，并在 publicPaths 加回两个 recovery 端点。
      <p className="mt-4 w-full text-right text-xs">
        <Link href="/auth/forgot" className="text-muted underline underline-offset-4 transition-colors hover:text-ink">
          忘记密码？
        </Link>
      </p> */}

      <div className="my-7 flex w-full items-center gap-4 text-xs text-muted">
        <span className="h-px flex-1 bg-line" />
        {t("or")}
        <span className="h-px flex-1 bg-line" />
      </div>

      {oidcMessage && (
        <p className="mb-3 w-full text-xs" style={{ color: "var(--color-warn)" }}>
          {oidcMessage}
        </p>
      )}
      <Link
        href={`/auth/akasha?return=${encodeURIComponent(returnTo)}`}
        className="inline-flex w-full items-center justify-center gap-2 rounded-md border border-line bg-raise py-2.5 text-sm transition-colors hover:border-muted"
      >
        <KeyRound size={14} className="text-src-ai" />
        {t("akasha")}
      </Link>

      <p className="mt-10 max-w-[17rem] text-center text-xs leading-relaxed text-muted">
        {t("copyright")}
      </p>
    </div>
  );
}
