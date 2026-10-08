"use client";

/**
 * 最后一道边界 —— 任何没被 proxy / api.ts 接住的渲染异常落到这里。
 *
 * 正常情况下认证类的 401 到不了这里（api.ts 会把它转成去 /auth/renew 的重定向）；
 * 这里兜的是其它异常，以及万一那条路也断了的情况。
 * 与 Next 默认错误页的区别：给用户两条可走的路（重试 / 重新登录），
 * 而不是只留一个 digest 编号让人去翻服务器日志。
 */
import { useTranslations } from "next-intl";

export default function Error({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  const t = useTranslations("error");
  return (
    <div className="mx-auto max-w-md py-24 text-center">
      <h1 className="display text-2xl tracking-wide">{t("title")}</h1>
      <p className="mt-3 text-sm text-muted">{t("body")}</p>
      <div className="mt-8 flex justify-center gap-3">
        <button
          type="button"
          onClick={reset}
          className="rounded border border-line px-4 py-2 text-sm transition-colors hover:text-ink"
        >
          {t("retry")}
        </button>
        <a href="/auth/logout" className="rounded border border-line px-4 py-2 text-sm transition-colors hover:text-ink">
          {t("relogin")}
        </a>
      </div>
      {error.digest && (
        <p className="mt-10 font-mono text-xs text-muted opacity-60" title={t("digestTitle")}>
          digest {error.digest}
        </p>
      )}
    </div>
  );
}
