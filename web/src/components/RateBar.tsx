import type { TagStat } from "@/lib/claims";

/**
 * 正确率条 —— 单序列水平条形图（dataviz：一个序列不需要图例，标题已说明画的是什么）。
 *
 * 颜色按状态而非按序列：正确率是有语义的量，红/橙/绿分别对应
 * 「明显薄弱 / 需要巩固 / 已掌握」，而不是给每个标签一个身份色。
 * 阈值直接标在图例里，避免读者自己猜配色含义。
 */
export function rateColor(rate: number): string {
  if (rate < 50) return "var(--color-warn)";
  if (rate < 80) return "var(--color-src-bank)";
  return "var(--color-ok)";
}

export function RateBar({ stat }: { stat: TagStat }) {
  const name = stat.i18n?.en ?? stat.value;
  const color = rateColor(stat.rate);
  return (
    <div className="flex items-center gap-3 py-1.5">
      <span className="w-44 shrink-0 truncate text-xs" title={name}>
        {name}
      </span>
      {/* 轨道用 hairline 灰，数据本身才是唯一允许"大声"的东西 */}
      <div className="h-[7px] flex-1 overflow-hidden rounded-[4px] bg-line">
        <div className="h-full rounded-[4px]" style={{ width: `${stat.rate}%`, background: color }} />
      </div>
      <span className="w-10 shrink-0 text-right text-xs tabular-nums" style={{ color }}>
        {Math.round(stat.rate)}%
      </span>
      <span className="w-12 shrink-0 text-right text-[0.7rem] tabular-nums text-muted">
        {stat.correct}/{stat.total}
      </span>
    </div>
  );
}

/** 阈值图例 —— 颜色有语义时必须说明，不能让读者猜。 */
export function RateLegend() {
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-[0.7rem] text-muted">
      {[
        ["< 50%　明显薄弱", "var(--color-warn)"],
        ["50–79%　需巩固", "var(--color-src-bank)"],
        ["≥ 80%　已掌握", "var(--color-ok)"],
      ].map(([label, c]) => (
        <span key={label} className="inline-flex items-center gap-1.5">
          <span className="h-2 w-2 rounded-full" style={{ background: c }} aria-hidden />
          {label}
        </span>
      ))}
    </div>
  );
}
