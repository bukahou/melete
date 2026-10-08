import Link from "next/link";

/**
 * 宽画布。⚠️ 旧版用 translate 按【视口】居中来突破 layout 的阅读宽度 ——
 * 有了左侧栏之后视口中心不再是内容区中心，会压到侧栏上。改为占满内容区。
 */
export function Wide({ children }: { children: React.ReactNode }) {
  return <div className="w-full">{children}</div>;
}

/**
 * 统计带：页面顶部一条，左边标题与一句话，右边几个大号衬线数字。
 * 首页与题库页同一形态 —— 层级只用数字与留白建立。
 */
export function Band({
  title,
  sub,
  crumbs,
  children,
  below,
}: {
  title: React.ReactNode;
  sub?: React.ReactNode;
  crumbs?: Array<{ href?: string; label: string }>;
  children?: React.ReactNode;
  below?: React.ReactNode;
}) {
  return (
    <div className="mb-6 grid items-end gap-6 pt-2 lg:grid-cols-[1fr_auto]">
      <div>
        {crumbs && (
          <nav className="mb-3 flex items-center gap-2 text-[0.82rem] text-muted">
            {crumbs.map((c, i) => (
              <span key={i} className="flex items-center gap-2">
                {i > 0 && <span className="opacity-50">›</span>}
                {c.href ? <Link href={c.href} className="hover:text-ink">{c.label}</Link> : <span className="text-ink">{c.label}</span>}
              </span>
            ))}
          </nav>
        )}
        <h1 className="display text-[1.5rem] leading-[1.25]">{title}</h1>
        {sub && <p className="mt-2 text-[0.9rem] text-muted">{sub}</p>}
        {below}
      </div>
      {children && <div className="flex flex-wrap">{children}</div>}
    </div>
  );
}

export function Stat({ value, unit, label, color }: { value: React.ReactNode; unit?: string; label: string; color?: string }) {
  return (
    <div className="min-w-[8.5rem] border-l border-line px-8 first:border-l-0 first:pl-0 last:pr-0">
      <div className="text-[1.5rem] font-semibold leading-none tabular-nums" style={color ? { color } : undefined}>
        {value}
        {unit && <small className="ml-0.5 text-[1rem] text-muted">{unit}</small>}
      </div>
      <div className="mt-1.5 text-[0.75rem] text-muted">{label}</div>
    </div>
  );
}
