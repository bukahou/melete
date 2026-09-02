import Link from "next/link";
import Image from "next/image";
import logo from "../../icon.png";
import { KeyRound } from "lucide-react";

/**
 * 登录页 —— 全站唯一在墙外的页面。
 * 双入口：本地密码（melete 自持的账号体系）+ Akasha 联邦。
 * Akasha 按其定案不做密码认证，密码归各接入应用自持，与 geass-v3 同模式。
 */
export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<{ error?: string; return?: string }>;
}) {
  const sp = await searchParams;
  const returnTo = sp.return ?? "/";

  return (
    <div className="mx-auto flex max-w-sm flex-col items-center pt-14">
      <Image src={logo} alt="" width={56} height={56} priority />
      <h1 className="display mt-5 text-2xl tracking-wide">Melete</h1>
      <p className="mt-2 text-xs tracking-wide text-muted">Μελέτη — 练习与修习</p>

      {/* 产品理念只在这里出现：登录后的人不需要每天读一遍 */}
      <p className="mt-8 max-w-[19rem] text-center text-[0.8rem] leading-[1.9] text-muted">
        被动阅读不产生学习，<em className="mark-em text-ink">主动回忆</em>才产生。
        每道题并列保存题库标注、社区投票与 AI 裁决三方答案 —— 分歧本身就是学习材料。
      </p>

      <form method="POST" action="/auth/password" className="mt-10 w-full space-y-3">
        <input type="hidden" name="return" value={returnTo} />
        <input
          name="username"
          required
          autoComplete="username"
          placeholder="用户名"
          className="w-full rounded-md border border-line bg-raise px-3.5 py-2.5 text-sm outline-none transition-colors placeholder:text-muted focus:border-muted"
        />
        <input
          name="password"
          type="password"
          required
          autoComplete="current-password"
          placeholder="密码"
          className="w-full rounded-md border border-line bg-raise px-3.5 py-2.5 text-sm outline-none transition-colors placeholder:text-muted focus:border-muted"
        />
        {sp.error && (
          <p className="text-xs" style={{ color: "var(--color-warn)" }}>
            用户名或密码错误
          </p>
        )}
        <button
          type="submit"
          className="w-full rounded-md py-2.5 text-sm font-medium"
          style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
        >
          登录
        </button>
      </form>

      <div className="my-7 flex w-full items-center gap-4 text-xs text-muted">
        <span className="h-px flex-1 bg-line" />
        或
        <span className="h-px flex-1 bg-line" />
      </div>

      <Link
        href={`/auth/akasha?return=${encodeURIComponent(returnTo)}`}
        className="inline-flex w-full items-center justify-center gap-2 rounded-md border border-line bg-raise py-2.5 text-sm transition-colors hover:border-muted"
      >
        <KeyRound size={14} className="text-src-ai" />
        用 Akasha 登录
      </Link>

      <p className="mt-10 max-w-[17rem] text-center text-xs leading-relaxed text-muted">
        题库内容涉及版权，仅限持有账号的学习者访问。
      </p>
    </div>
  );
}
