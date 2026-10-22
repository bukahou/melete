import type { Metadata } from "next";
import Link from "next/link";
import Image from "next/image";
import { notFound } from "next/navigation";
import { connection } from "next/server";
import { getTranslations } from "next-intl/server";
import logo from "../../../icon.png";
import { passwordLogin } from "@/lib/auth";
import { safeReturnPath } from "@/lib/session";

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations("login");
  // 不让搜索引擎收录：这一页不是给公众的
  return { title: t("passwordTitle"), robots: { index: false, follow: false } };
}

/**
 * 调试账号登录（P9 #35–#38，2026-10-21 用户裁定）—— ⛔ 不在任何地方链接的独立页面。
 *
 * 用户侧只有一种登录方式（#25）：登录弹窗一个字不变，真实用户碰不到这一页。
 * 用它的是调试账号（Claude 与开发者自己）：知道地址的人直接打开。
 * ⭐ 「藏」只为不打扰真实用户，⛔ 不是安全措施 —— 理由见 auth.ts 的 passwordLogin。
 * 开关关着时这一页与表单路由（/auth/password）都是 404，与「看不到 = 404」同一条规则。
 */
const ERROR: Record<string, string> = {
  credentials: "badCredentials",
  rate: "rateLimited",
};

export default async function PasswordLoginPage({
  searchParams,
}: {
  searchParams: Promise<{ error?: string; return?: string }>;
}) {
  // ⭐ 先 connection() 再读开关：开关是运行时的环境变量（auth.ts），
  //   ⛔ 不得被构建期预渲染定死成「永远 404」或「永远开着」（Next 16 的写法，取代旧的 dynamic = "force-dynamic"）
  await connection();
  if (!passwordLogin.enabled) notFound();
  const sp = await searchParams;
  const t = await getTranslations("login");
  const returnTo = safeReturnPath(sp.return);
  // 错误码只用来【查表】，⛔ 不回显 URL 里的文字；不认识的码按「用户名或密码错误」
  const error = sp.error ? t(ERROR[sp.error] ?? ERROR.credentials) : null;
  const field =
    "w-full rounded-md border border-line bg-raise px-3.5 py-2.5 text-sm outline-none transition-colors placeholder:text-muted focus:border-muted";

  return (
    // 与登录弹窗同一个外壳（背后的应用外壳由 layout 垫）
    <section role="dialog" aria-modal="true" aria-labelledby="password-login-title"
             className="w-full max-w-[440px] rounded-2xl bg-raise px-7 pb-7 pt-6 shadow-[0_24px_60px_-20px_rgba(0,0,0,0.35)]">
      <h1 id="password-login-title" className="text-[1.15rem] font-semibold">{t("passwordTitle")}</h1>

      <div className="mt-6 flex items-center gap-3">
        <Image src={logo} alt="" width={40} height={40} priority />
        <p className="display text-[1.15rem] leading-tight">Melete</p>
      </div>

      <form method="POST" action="/auth/password" className="mt-6 grid gap-3">
        <input type="hidden" name="return" value={returnTo} />
        <label className="grid gap-1.5 text-[0.8rem] text-muted">
          {t("username")}
          <input name="username" required autoComplete="username" className={field} />
        </label>
        <label className="grid gap-1.5 text-[0.8rem] text-muted">
          {t("password")}
          <input name="password" type="password" required autoComplete="current-password" className={field} />
        </label>
        {error && (
          <p role="alert" className="text-xs" style={{ color: "var(--color-warn)" }}>{error}</p>
        )}
        <button type="submit"
                className="mt-2 w-full rounded-lg py-3 text-[0.92rem] font-semibold transition-opacity hover:opacity-90"
                style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}>
          {t("submit")}
        </button>
      </form>

      <p className="mt-6 border-t border-line pt-4 text-center text-[0.74rem] text-muted">
        {t("debugOnly")}
        {" · "}
        <Link href={`/auth/login?return=${encodeURIComponent(returnTo)}`} className="text-accent-ink hover:underline">
          {t("backToThirdParty")}
        </Link>
      </p>
    </section>
  );
}
