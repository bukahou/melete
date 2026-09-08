"use client";

import { useTransition } from "react";
import { useRouter } from "next/navigation";
import { Languages } from "lucide-react";
import { LOCALES, LOCALE_LABEL, type Locale } from "@/i18n/locales";

/**
 * 语言切换 —— 顶栏一个下拉。
 *
 * ⚠️ 切换必须走【服务端】(POST /settings/language)，⛔ 不能只在浏览器写 cookie：
 * 页面是 SSR 的，文案在服务端就已经定死；只写 cookie 不刷新 = 什么都不变，
 * 而用户会以为坏了。这里写完 cookie 后 router.refresh() 重新取一遍 RSC。
 *
 * ⭐ 用 <select> 而不是自造下拉：键盘、读屏、移动端原生选择器全都白拿。
 */
export function LanguageSwitcher({ current }: { current: Locale }) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();

  function change(next: string) {
    if (next === current) return;
    startTransition(async () => {
      await fetch("/settings/language", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ locale: next }),
      });
      router.refresh();
    });
  }

  return (
    <label className="inline-flex items-center gap-1.5 text-muted transition-colors hover:text-ink">
      <Languages size={13} className="translate-y-px" aria-hidden />
      <select
        value={current}
        disabled={pending}
        onChange={(e) => change(e.target.value)}
        aria-label={LOCALE_LABEL[current]}
        className="cursor-pointer appearance-none bg-transparent text-sm outline-none"
      >
        {LOCALES.map((l) => (
          <option key={l} value={l}>
            {LOCALE_LABEL[l]}
          </option>
        ))}
      </select>
    </label>
  );
}
