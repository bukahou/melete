import Link from "next/link";
import { ChevronLeft } from "lucide-react";

/**
 * 侧栏里「即将开放」的功能页（书签 / 用语集，用户裁定先摆出来）。
 * ⭐ 点进来要说清楚「这是什么、什么时候有」，⛔ 不给 404 —— 那会被当成坏了。
 */
export function ComingSoon({ title, body, soon, back }: { title: string; body: string; soon: string; back: string }) {
  return (
    <div className="mx-auto max-w-xl pt-10">
      <div className="card px-8 py-10 text-center">
        <span className="rounded-full bg-accent-soft px-3 py-1 text-[0.75rem] font-semibold text-accent-ink">{soon}</span>
        <h1 className="display mt-4 text-[1.4rem]">{title}</h1>
        <p className="mt-3 text-[0.9rem] leading-relaxed text-muted">{body}</p>
        <Link href="/" className="mt-6 inline-flex items-center gap-1 text-[0.85rem] text-accent-ink hover:underline">
          <ChevronLeft size={15} />{back}
        </Link>
      </div>
    </div>
  );
}
