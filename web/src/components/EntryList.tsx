import Link from "next/link";
import { useTranslations } from "next-intl";

/**
 * 入口列表（P9 #42，2026-10-21 用户裁定）：「选一组题」的列表 —— 历年 / 分组、按考纲域。
 *
 * ⭐ 一行 = 名字 + 右边固定两列：已答（做过的题 / 这一组的题数）· 掌握率（每题只看最近一次，#9 的口径）。
 *   没做过的行显示 0 / N 与 —，每行结构一样，数字上下对齐。
 * ⛔ 不放进度条（做了 1–2 题只剩一个点，像脏点）· ⛔ 不放每行的箭头（整行可点，悬停整行高亮）·
 *   ⛔ 名字不加粗（一整列加粗 = 没有重点）。行与行之间一条细线，视线能从名字走到数字。
 * 分组用白条（EntryHead）：平铺列表一条白条兼作列头；考纲树的分野是白条，整条能点。
 * ⚠️ 名字允许折行，⛔ 不截成省略号：手机上数字列一挤，「第 101–20…」就看不出是哪一组（2026-10-21 实测）。
 */
export type EntryStats = { total: number; answered: number; correct: number };

// 名字吃掉其余宽度；两列数字固定宽度才对得齐（手机窄一档）
const GRID = "grid grid-cols-[minmax(0,1fr)_4.75rem_3.25rem] items-center sm:grid-cols-[minmax(0,1fr)_6.5rem_4.75rem]";
const FOCUS = "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent-ink";
const NUM = "justify-self-end text-[0.84rem] font-normal text-muted tabular-nums";

function Cells({ stats }: { stats: EntryStats }) {
  const t = useTranslations("practice");
  const done = stats.answered > 0;
  const dim = done ? "" : "opacity-55";
  return (
    <>
      <span className={`${NUM} ${dim}`}>
        <span className="sr-only">{t("colAnswered")} </span>{stats.answered} / {stats.total}
      </span>
      <span className={`${NUM} ${dim}`}>
        <span className="sr-only">{t("colMastery")} </span>
        {done ? `${Math.round((stats.correct / stats.answered) * 100)}%` : "—"}
      </span>
    </>
  );
}

function ColumnNames() {
  const t = useTranslations("practice");
  return (
    <>
      <span aria-hidden className="justify-self-end text-[0.75rem] font-normal text-muted">{t("colAnswered")}</span>
      <span aria-hidden className="justify-self-end text-[0.75rem] font-normal text-muted">{t("colMastery")}</span>
    </>
  );
}

/**
 * 白条：一组的标题。
 *   columns       → 右边写两列的列名（平铺列表的列头）
 *   stats + href  → 这一组自己的两列数字，整条能点（考纲树的分野）
 */
export function EntryHead({ title, href, stats, columns }: {
  title: string; href?: string; stats?: EntryStats; columns?: boolean;
}) {
  const body = (
    <>
      <span className="min-w-0 py-1.5 leading-snug">{title}</span>
      {columns ? <ColumnNames /> : stats ? <Cells stats={stats} /> : null}
    </>
  );
  const cls = `${GRID} min-h-9 bg-tile px-4 text-[0.94rem] font-semibold [&:not(:first-child)]:mt-4`;
  return href
    ? <Link href={href} className={`${cls} transition-colors hover:bg-accent-soft ${FOCUS}`}>{body}</Link>
    : <div className={cls}>{body}</div>;
}

/** 只有列名的一行 —— 考纲树用：分野白条上放的是它自己的数字，列名得另起一行。 */
export function EntryColumns() {
  return (
    <div className={`${GRID} px-4 pb-1.5 pt-3`}>
      <span />
      <ColumnNames />
    </div>
  );
}

/** 一行：名字 + 两列数字，整行可点。level 2 = 考纲树的中分類（缩进、小一号）。 */
export function EntryRow({ href, title, stats, level = 1 }: {
  href: string; title: string; stats: EntryStats; level?: 1 | 2;
}) {
  const size = level === 2
    ? "min-h-[2.875rem] pl-8 text-[0.89rem] sm:pl-11"
    : "min-h-14 pl-5 text-[0.97rem]";
  return (
    <Link href={href} className={`${GRID} ${size} mx-2 border-b border-line-2 pr-2 transition-colors hover:bg-accent-soft ${FOCUS}`}>
      <span className="min-w-0 py-2 leading-snug">{title}</span>
      <Cells stats={stats} />
    </Link>
  );
}
