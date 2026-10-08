"use client";

import { useEffect, useMemo, useState } from "react";
import { Check, Play } from "lucide-react";
import { useTranslations } from "next-intl";

/**
 * 4.3 自选条件表单（P9 #12）。
 *
 * ⭐ 2026-10-08 用户反馈：「点击按钮不能快速切换。因此不能判断是否切换成功了」。两层原因：
 *   1. 选中样式几乎看不出来 —— 未选底色与选中底色在浅青配色下只差一点（#e3f0f0 vs #d5ebee），
 *      原生单选圈 / 复选框又小。⇒ 选中 = 强调色描边 + 浅底 + ✓，未选 = 白底细边，差别一眼可见
 *   2. 点完不知道组合出来有几题，0 题要点进去才发现。⇒ 每次点选都问一次题数（用户裁定方案 b），
 *      开始按钮写着「开始（37 题）」，0 题时变灰并明说
 *
 * 仍然是一张普通的 GET 表单：提交走 start 路由（与题数同一个解析，pickForm），
 * 这里的状态只是为了「画出来」和「问题数」，⛔ 不另存一份条件。
 */
export type PickOption = { value: string; label: string };
export type PickDomain = PickOption & { groups: PickOption[] };

function Chip({ type, name, value, label, on, onChange }: {
  type: "radio" | "checkbox"; name: string; value: string; label: string; on: boolean; onChange: () => void;
}) {
  return (
    <label
      className={`flex cursor-pointer select-none items-center gap-2.5 rounded-lg border px-3 py-2 text-sm transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-accent-ink ${
        on
          ? "border-accent-ink bg-accent-soft font-semibold text-accent-ink"
          : "border-line bg-tile text-ink hover:border-accent-ink"
      }`}
    >
      <input type={type} name={name} value={value} checked={on} onChange={onChange} className="sr-only" />
      <span
        aria-hidden
        className={`flex h-4 w-4 shrink-0 items-center justify-center border transition-colors ${
          type === "radio" ? "rounded-full" : "rounded"
        } ${on ? "border-accent-ink bg-accent-ink text-tile" : "border-muted"}`}
      >
        {on && <Check size={11} strokeWidth={3.5} />}
      </span>
      <span className="min-w-0 truncate">{label}</span>
    </label>
  );
}

export function PickForm({ slug, statuses, domains, ranges, labels }: {
  slug: string;
  statuses: PickOption[];
  domains: PickDomain[];
  ranges: PickOption[];
  labels: { status: string; tags: string; range: string; rangeAll: string; bookmarkNote: string };
}) {
  const t = useTranslations("practice");
  const [status, setStatus] = useState("all");
  const [tags, setTags] = useState<string[]>([]);
  const [range, setRange] = useState("");
  const [total, setTotal] = useState<number | null>(null);
  const [counting, setCounting] = useState(true);

  const query = useMemo(() => {
    const q = new URLSearchParams({ st: status, r: range });
    tags.forEach((v) => q.append("t", v));
    return q.toString();
  }, [status, tags, range]);

  // 每次条件变化问一次题数。⚠️ 旧请求要作废：快速连点时晚到的旧结果会覆盖新结果
  useEffect(() => {
    const ctl = new AbortController();
    setCounting(true);
    const timer = setTimeout(async () => {
      try {
        const res = await fetch(`/banks/${slug}/practice/pick/count?${query}`, { signal: ctl.signal });
        const body = res.ok ? await res.json().catch(() => null) : null;
        setTotal(typeof body?.total === "number" ? body.total : null);
      } catch {
        if (ctl.signal.aborted) return;
        setTotal(null); // 算不出来就不显示数字，⛔ 不拦着开始 —— 刷题页自己会说「没有题目」
      }
      setCounting(false);
    }, 120);
    return () => { ctl.abort(); clearTimeout(timer); };
  }, [slug, query]);

  const toggleTag = (v: string) => setTags((cur) => (cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v]));
  const empty = !counting && total === 0;

  return (
    <form method="GET" action={`/banks/${slug}/practice/pick/start`} className="grid gap-4">
      <fieldset className="card p-5">
        <legend className="sr-only">{labels.status}</legend>
        <div aria-hidden className="eyebrow mb-3">{labels.status}</div>
        <div className="grid gap-2 sm:grid-cols-3">
          {statuses.map((s) => (
            <Chip key={s.value} type="radio" name="st" value={s.value} label={s.label}
                  on={status === s.value} onChange={() => setStatus(s.value)} />
          ))}
        </div>
        <p className="mt-3 text-xs text-muted">{labels.bookmarkNote}</p>
      </fieldset>

      <fieldset className="card p-5">
        <legend className="sr-only">{labels.tags}</legend>
        <div aria-hidden className="eyebrow mb-3">{labels.tags}</div>
        <div className="grid gap-3">
          {domains.map((d) => (
            <div key={d.value} className="grid gap-2">
              <Chip type="checkbox" name="t" value={d.value} label={d.label}
                    on={tags.includes(d.value)} onChange={() => toggleTag(d.value)} />
              {d.groups.length > 0 && (
                <div className="grid gap-2 pl-6 sm:grid-cols-2">
                  {d.groups.map((g) => (
                    <Chip key={g.value} type="checkbox" name="t" value={g.value} label={g.label}
                          on={tags.includes(g.value)} onChange={() => toggleTag(g.value)} />
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      </fieldset>

      {ranges.length > 1 && (
        <fieldset className="card p-5">
          <legend className="sr-only">{labels.range}</legend>
          <div aria-hidden className="eyebrow mb-3">{labels.range}</div>
          <select name="r" value={range} onChange={(e) => setRange(e.target.value)}
                  className={`w-full rounded-lg border px-3 py-2 text-sm outline-none ${range ? "border-accent-ink bg-accent-soft font-semibold text-accent-ink" : "border-line bg-tile"}`}>
            <option value="">{labels.rangeAll}</option>
            {ranges.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
          </select>
        </fieldset>
      )}

      {/* ⭐ 开始按钮贴在底部：条件很长时（IPA 三层树）也一直看得见题数 —— 那是「点了有没有生效」的回声。
          手机上要让开底部标签栏 */}
      <div className="card sticky bottom-20 z-10 flex items-center gap-4 px-5 py-3 shadow-[0_-2px_12px_rgba(0,0,0,0.06)] md:bottom-4">
        <span className="min-w-0 flex-1 text-[0.85rem]" aria-live="polite">
          {empty ? <span style={{ color: "var(--color-warn)" }}>{t("pickNone")}</span>
            : counting ? <span className="text-muted">{t("pickCounting")}</span>
            : total != null ? <>{t("pickMatch", { n: total })}</>
            : null}
        </span>
        <button type="submit" disabled={empty}
                className="inline-flex shrink-0 items-center gap-2 rounded-xl px-6 py-2.5 text-sm font-semibold transition-opacity disabled:cursor-not-allowed disabled:opacity-40"
                style={{ background: "var(--color-accent)", color: "var(--color-accent-fg)" }}>
          <Play size={13} fill="currentColor" />
          {total != null && !counting && total > 0 ? t("pickGoN", { n: total }) : t("pickGo")}
        </button>
      </div>
    </form>
  );
}
