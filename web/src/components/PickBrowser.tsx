"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { Check, ChevronDown, Play, Shuffle, X } from "lucide-react";
import { useTranslations } from "next-intl";
import { drillHref } from "@/lib/drillSpec";
import { pickContextFromForm } from "@/lib/pickForm";
import { BookmarkToggle } from "./BookmarkToggle";

/**
 * 4.3 自选条件 —— 照 it-pass「絞り込んで出題」：标签栏 + 题目列表（2026-10-08 用户裁定方案 b）。
 *
 * ⭐ 为什么不是表单：用户问「考纲域（可多选，不选 = 全部）是什么意思？我需要选吗？」——
 *   必填的状态与可选的范围 / 考纲域平铺成同样的三块，「不选 = 全部」只能靠一行小字解释。
 *   it-pass 的解法：所有条件都是顶部一排标签，可选条件平时只是一个标签，点开才弹出面板；
 *   结果直接列成题目，点一下标签列表就变 ——「生效了没有」不需要任何说明。
 *
 * 状态同步进 URL（replaceState）：刷新 / 后退回来条件还在。集合定义只在 pickForm + drillSpec 两处，
 * 列表第 i 行 = 刷题页 offset i（点哪题从哪题开始）。
 */
export type PickOption = { value: string; label: string };
export type PickNode = PickOption & { count: number; children: Array<PickOption & { count: number }> };

type Item = { id: number; no: number; stem: string; last: boolean | null; bookmarked: boolean };
type Sheet = "tags" | "range" | null;

function Pill({ on, onClick, children, caret }: { on: boolean; onClick: () => void; children: React.ReactNode; caret?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={on}
      className={`inline-flex shrink-0 items-center gap-1.5 rounded-full border px-3.5 py-1.5 text-[0.85rem] transition-colors ${
        on ? "border-accent bg-accent font-semibold text-accent-fg" : "border-accent-ink/50 bg-tile text-accent-ink hover:border-accent-ink"
      }`}
    >
      {on && !caret && <Check size={13} strokeWidth={3} />}
      {children}
      {caret && <ChevronDown size={14} />}
    </button>
  );
}

function Box({ on }: { on: boolean }) {
  return (
    <span aria-hidden className={`flex h-4 w-4 shrink-0 items-center justify-center rounded border ${on ? "border-accent-ink bg-accent-ink text-tile" : "border-muted"}`}>
      {on && <Check size={11} strokeWidth={3.5} />}
    </span>
  );
}

export function PickBrowser({ slug, initial, statuses, tree, ranges, labels }: {
  slug: string;
  initial: string;               // 进页时的查询串（st / t / r / seed）
  statuses: PickOption[];
  tree: PickNode[];              // 考纲域：一级 = domain，二级 = 分组（值是逗号串的 topic id）
  ranges: PickOption[];          // 卷子 / 题号段
  labels: { domain: string; range: string };
}) {
  const t = useTranslations("practice");
  const init = useMemo(() => new URLSearchParams(initial), [initial]);
  const [status, setStatus] = useState(init.get("st") ?? "all");
  const [tags, setTags] = useState<string[]>(init.getAll("t"));
  const [range, setRange] = useState(init.get("r") ?? "");
  const [seed, setSeed] = useState<string>(init.get("seed") ?? "");
  const [sheet, setSheet] = useState<Sheet>(null);
  const [items, setItems] = useState<Item[]>([]);
  const [total, setTotal] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  const query = useMemo(() => {
    const q = new URLSearchParams();
    if (status !== "all") q.set("st", status);
    tags.forEach((v) => q.append("t", v));
    if (range) q.set("r", range);
    if (seed) q.set("seed", seed);
    return q.toString();
  }, [status, tags, range, seed]);

  // 条件 → URL（刷新 / 后退不丢）+ 重新取列表。⚠️ 旧请求作废：连点时晚到的旧结果会覆盖新结果
  useEffect(() => {
    window.history.replaceState(null, "", query ? `?${query}` : window.location.pathname);
    const ctl = new AbortController();
    setLoading(true);
    fetch(`/banks/${slug}/practice/pick/list?${query}`, { signal: ctl.signal })
      .then((r) => (r.ok ? r.json() : Promise.reject()))
      .then((b) => { setItems(b.items); setTotal(b.total); setFailed(false); setLoading(false); })
      .catch(() => { if (!ctl.signal.aborted) { setFailed(true); setLoading(false); } });
    return () => ctl.abort();
  }, [slug, query]);

  const loadMore = useCallback(async () => {
    const r = await fetch(`/banks/${slug}/practice/pick/list?${query}&offset=${items.length}`);
    if (r.ok) { const b = await r.json(); setItems((cur) => [...cur, ...b.items]); }
  }, [slug, query, items.length]);

  const ctx = useMemo(() => pickContextFromForm(new URLSearchParams(query)), [query]);
  const toggleTag = (v: string) => setTags((cur) => (cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v]));

  // 标签上直接写出选了什么 —— 「考纲域：テクノロジ系」，⛔ 不只是变个颜色
  const nameOf = new Map(tree.flatMap((d) => [[d.value, d.label] as const, ...d.children.map((c) => [c.value, c.label] as const)]));
  const tagNames = tags.map((v) => nameOf.get(v) ?? v);
  const tagLabel = tags.length === 0 ? labels.domain
    : tagNames.length <= 2 ? `${labels.domain}：${tagNames.join("・")}` : t("pickMany", { what: labels.domain, first: tagNames[0], n: tagNames.length });
  const rangeLabel = range ? `${labels.range}：${ranges.find((r) => r.value === range)?.label ?? range}` : labels.range;

  return (
    <div className="grid gap-4">
      {/* 标题行：总数 + 打乱顺序（同 it-pass「絞り込んで出題（1100）」与右上角シャッフル） */}
      <div className="flex items-center gap-3">
        <h1 className="display text-[1.4rem]">
          {t("pickTitle")}
          <span className="ml-1.5 text-[1.05rem] font-normal text-muted tabular-nums">（{loading && total == null ? "…" : total ?? "—"}）</span>
        </h1>
        <button type="button" onClick={() => setSeed(seed ? "" : String(Math.floor(Math.random() * 2 ** 31)))}
                aria-pressed={Boolean(seed)}
                className={`ml-auto inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-[0.82rem] transition-colors ${
                  seed ? "bg-accent-soft font-semibold text-accent-ink" : "text-muted hover:text-accent-ink"}`}>
          <Shuffle size={15} />{seed ? t("pickShuffled") : t("pickShuffle")}
        </button>
      </div>

      {/* 标签栏：状态单选 + 两个可选条件（点开才出面板） */}
      <div className="flex flex-wrap gap-2">
        {statuses.map((s) => (
          <Pill key={s.value} on={status === s.value} onClick={() => setStatus(s.value)}>{s.label}</Pill>
        ))}
        <span className="mx-1 w-px self-stretch bg-line" aria-hidden />
        {ranges.length > 1 && (
          <Pill on={Boolean(range)} caret onClick={() => setSheet("range")}>{rangeLabel}</Pill>
        )}
        <Pill on={tags.length > 0} caret onClick={() => setSheet("tags")}>{tagLabel}</Pill>
        {(tags.length > 0 || range) && (
          <button type="button" onClick={() => { setTags([]); setRange(""); }}
                  className="inline-flex items-center gap-1 px-2 text-[0.8rem] text-muted hover:text-ink">
            <X size={13} />{t("pickClear")}
          </button>
        )}
      </div>
      
      {/* 题目列表：点哪题从哪题开始 */}
      <section className={`card overflow-hidden transition-opacity ${loading ? "opacity-60" : ""}`}>
        {failed ? (
          <p className="px-5 py-10 text-center text-sm text-muted">{t("pickFailed")}</p>
        ) : !loading && items.length === 0 ? (
          <p className="px-5 py-10 text-center text-sm" style={{ color: "var(--color-warn)" }}>{t("pickNone")}</p>
        ) : (
          <ol>
            {items.map((it, i) => (
              <li key={it.id} className="flex items-center border-b border-line-2 pr-3 last:border-b-0 hover:bg-accent-soft">
                <Link href={drillHref(slug, ctx, { i })} className="flex min-w-0 flex-1 items-center gap-4 py-3 pl-5">
                  <span className="w-14 shrink-0 font-semibold tabular-nums">#{it.no}</span>
                  <span className="min-w-0 flex-1 truncate text-[0.88rem] text-muted">{it.stem}</span>
                  {/* 上次对错：固定宽度占位，两列图标才对得齐 */}
                  <span className="flex w-4 shrink-0 justify-center">
                    {it.last === true && <Check size={16} style={{ color: "var(--color-ok)" }} aria-label={t("pickLastOk")} />}
                    {it.last === false && <X size={16} style={{ color: "var(--color-warn)" }} aria-label={t("pickLastNg")} />}
                  </span>
                </Link>
                {/* 同 it-pass：列表里直接收藏 / 取消（不进题也能整理） */}
                <BookmarkToggle key={`${it.id}-${it.bookmarked}`} questionId={it.id} initial={it.bookmarked} compact />
              </li>
            ))}
          </ol>
        )}
        {!failed && total != null && items.length < total && (
          <button type="button" onClick={loadMore}
                  className="w-full border-t border-line-2 py-3 text-[0.85rem] text-accent-ink hover:bg-accent-soft">
            {t("pickMore", { n: total - items.length })}
          </button>
        )}
      </section>

      {/* 从第 1 题开始（贴底，手机上让开底部标签栏） */}
      {total != null && total > 0 && (
        <div className="card sticky bottom-20 z-10 flex items-center gap-4 px-5 py-3 shadow-[0_-2px_12px_rgba(0,0,0,0.06)] md:bottom-4">
          <span className="min-w-0 flex-1 text-[0.85rem]">{t("pickMatch", { n: total })}</span>
          <Link href={drillHref(slug, ctx)}
                className="inline-flex shrink-0 items-center gap-2 rounded-xl px-6 py-2.5 text-sm font-semibold"
                style={{ background: "var(--color-accent)", color: "var(--color-accent-fg)" }}>
            <Play size={13} fill="currentColor" />{t("pickStartFirst")}
          </Link>
        </div>
      )}

      {/* 弹出面板：可选条件平时收起，点标签才出现（同 it-pass 的分野面板） */}
      {sheet && (
        <div className="fixed inset-0 z-30 flex items-end justify-center bg-black/30 md:items-center" onClick={() => setSheet(null)}>
          <div role="dialog" aria-modal className="card max-h-[80vh] w-full max-w-lg overflow-y-auto rounded-b-none md:rounded-b-[0.875rem]"
               onClick={(e) => e.stopPropagation()}>
            <div className="sticky top-0 flex items-center gap-3 border-b border-line bg-raise px-5 py-3">
              <span className="font-semibold">{sheet === "tags" ? labels.domain : labels.range}</span>
              <span className="text-[0.82rem] text-muted tabular-nums">{loading ? "…" : t("pickMatch", { n: total ?? 0 })}</span>
              <button type="button" onClick={() => setSheet(null)}
                      className="ml-auto rounded-lg px-4 py-1.5 text-sm font-semibold"
                      style={{ background: "var(--color-accent)", color: "var(--color-accent-fg)" }}>
                {t("pickDone")}
              </button>
            </div>
            {sheet === "tags" ? (
              <ul className="py-1">
                {tree.map((d) => (
                  <li key={d.value}>
                    <button type="button" onClick={() => toggleTag(d.value)}
                            className="flex w-full items-center gap-3 bg-accent-soft/60 px-5 py-2.5 text-left font-semibold hover:bg-accent-soft">
                      <Box on={tags.includes(d.value)} /><span className="flex-1">{d.label}</span>
                      <span className="text-[0.78rem] font-normal text-muted">{t("questions", { n: d.count })}</span>
                    </button>
                    {d.children.map((c) => (
                      <button key={c.value} type="button" onClick={() => toggleTag(c.value)}
                              className="flex w-full items-center gap-3 py-2 pl-10 pr-5 text-left text-[0.9rem] hover:bg-accent-soft">
                        <Box on={tags.includes(c.value)} /><span className="flex-1">{c.label}</span>
                        <span className="text-[0.78rem] text-muted">{t("questions", { n: c.count })}</span>
                      </button>
                    ))}
                  </li>
                ))}
              </ul>
            ) : (
              <ul className="py-1">
                {[{ value: "", label: t("pickRangeAll") }, ...ranges].map((r) => (
                  <li key={r.value}>
                    <button type="button" onClick={() => setRange(r.value)}
                            className="flex w-full items-center gap-3 px-5 py-2.5 text-left hover:bg-accent-soft">
                      <span aria-hidden className={`flex h-4 w-4 items-center justify-center rounded-full border ${range === r.value ? "border-accent-ink bg-accent-ink text-tile" : "border-muted"}`}>
                        {range === r.value && <Check size={11} strokeWidth={3.5} />}
                      </span>
                      <span className={range === r.value ? "font-semibold" : ""}>{r.label}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
