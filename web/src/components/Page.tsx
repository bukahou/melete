import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { useTranslations } from "next-intl";

export type Crumb = { href?: string; label: string };

/**
 * 页面外框（P9 #41，2026-10-21 用户裁定，参照 it-pass）：浅青的框上一条顶栏（返回 + 标题）＋ 一整张近白的纸。
 *
 * ⭐ 浅青只做外框，内容一律放在纸上；纸面上分区用白条（Section / .band）与细线行，⛔ 不再放卡片。
 *   例外：首页是仪表盘，暂沿用「浅青底 + 卡片」（it-pass 的首页也是这样），不用本组件。
 * back：返回箭头去哪。缺省取面包屑里最后一个能点的（父页）；侧栏直达的顶级页没有面包屑，也就没有返回。
 *   false = 不要返回箭头 —— 从哪儿都能进来的页（题目详情）写死一个去处，就是在冒充「浏览器后退」。
 * width：纸面里内容列多宽 —— read = 阅读宽度（列表、表单）；wide = 宽（统计、题目）。
 * 手机：面包屑收起（顶栏的返回箭头就够了），纸铺满宽度、只留顶部圆角，底部给标签栏让位。
 */
export function Page({ title, crumbs, back, sub, actions, width = "read", children }: {
  title: React.ReactNode;
  crumbs?: Crumb[];
  back?: string | false;
  sub?: React.ReactNode;
  actions?: React.ReactNode;
  width?: "read" | "wide";
  children: React.ReactNode;
}) {
  const t = useTranslations("common");
  const backHref = back === false ? undefined : back ?? crumbs?.filter((c) => c.href).at(-1)?.href;
  return (
    <>
      <header className="flex h-[3.375rem] shrink-0 items-center gap-1.5 px-3 md:h-[3.75rem] md:px-5">
        {backHref && (
          <Link href={backHref} aria-label={t("back")}
                className="grid size-9 shrink-0 place-items-center rounded-full text-accent-ink transition-colors hover:bg-accent-soft">
            <ArrowLeft size={20} />
          </Link>
        )}
        <h1 className={`min-w-0 flex-1 truncate text-[1.125rem] font-semibold md:text-[1.25rem] ${backHref ? "" : "pl-1.5"}`}>{title}</h1>
        {actions}
      </header>
      <div className="sheet flex-1 rounded-t-2xl bg-sheet px-4 pb-24 pt-4 md:mx-3 md:mb-3 md:rounded-2xl md:px-8 md:pb-14 md:pt-5">
        {crumbs && <Crumbs items={crumbs} />}
        <div className={`mx-auto w-full ${width === "wide" ? "max-w-[1000px]" : "max-w-3xl"}`}>
          {sub && <p className="mb-3 text-[0.8rem] leading-relaxed text-muted">{sub}</p>}
          {children}
        </div>
      </div>
    </>
  );
}

function Crumbs({ items }: { items: Crumb[] }) {
  return (
    <nav className="mb-4 hidden items-center gap-2 text-[0.8rem] text-muted md:flex">
      {items.map((c, i) => (
        <span key={i} className="flex items-center gap-2">
          {i > 0 && <span className="opacity-50">›</span>}
          {c.href ? <Link href={c.href} className="hover:text-ink">{c.label}</Link> : <span className="text-ink">{c.label}</span>}
        </span>
      ))}
    </nav>
  );
}

/**
 * 纸面上的一节：白条标题（右侧可放小字或操作）+ 内容。⛔ 不是卡片 —— 没有底色、没有边框。
 * 相邻的两节之间自动留白；节内的行用细线分开（border-b border-line-2，与 EntryRow 同一套）。
 */
export function Section({ id, title, side, icon, children }: {
  id?: string; title: React.ReactNode; side?: React.ReactNode; icon?: React.ReactNode; children: React.ReactNode;
}) {
  return (
    <section id={id} className="scroll-mt-6 [&+&]:mt-8">
      <div className="band flex items-center gap-2.5">
        {icon && <span className="shrink-0 text-muted">{icon}</span>}
        <h2 className="min-w-0 flex-1 truncate">{title}</h2>
        {side && <span className="shrink-0 text-[0.75rem] font-normal text-muted">{side}</span>}
      </div>
      {children}
    </section>
  );
}
