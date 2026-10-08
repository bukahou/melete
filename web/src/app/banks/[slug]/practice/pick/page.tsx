import { notFound } from "next/navigation";
import { getLocale, getTranslations } from "next-intl/server";
import { ApiError, getBank, listAxisTags, type BankDetail, type Tag } from "@/lib/api";
import { tagName, tagTypeLabel } from "@/lib/claims";
import { sessionLabel } from "@/lib/drillLabel";
import { PracticeShell } from "@/components/PracticeShell";

export const revalidate = 0;

/**
 * 4.3 自选条件（P9 #12）：状态 ∧ 考纲域（多选并集）∧ 范围。
 *
 * 形态沿用设置页：⛔ 不引入客户端状态，GET 表单交给 start 路由归一成刷题 URL。
 * 为什么不直接 GET 到刷题页：表单的字段形状（复选框里一个分组 = 多个 id、范围下拉 = "s:…/n:a-b"）
 * 与刷题页的 URL 形状不同，⛔ 不该让刷题页认两套 —— drillSpec 只认一种。
 */
const STATUSES = ["all", "wrong", "unseen", "bookmarked", "contested"] as const;

function ranges(bank: BankDetail, rangeLabel: (from: number, to: number) => string, locale: string) {
  const sessions = bank.stats.sessions ?? [];
  const named = sessions.filter((s) => s.session !== "");
  if (named.length > 0) return named.map((s) => ({ value: `s:${s.session}`, label: sessionLabel(bank.meta, s.session, locale) }));
  const all = sessions[0];
  if (!all) return [];
  const size = bank.meta.groupSize ?? 100;
  const out = [];
  for (let from = Math.floor((all.noFrom - 1) / size) * size + 1; from <= all.noTo; from += size) {
    const to = Math.min(from + size - 1, all.noTo);
    out.push({ value: `n:${from}-${to}`, label: rangeLabel(from, to) });
  }
  return out;
}

export default async function PickPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const [t, dash, locale] = await Promise.all([getTranslations("practice"), getTranslations("dash"), getLocale()]);
  let bank: BankDetail, tags: Tag[];
  try {
    [bank, tags] = await Promise.all([getBank(slug), listAxisTags(slug)]);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const domainLabel = tagTypeLabel(bank.meta, "domain", locale);
  const domains = tags.filter((x) => x.type === "domain").sort((a, b) => a.value.localeCompare(b.value));
  const topicByValue = new Map(tags.filter((x) => x.type === "topic").map((x) => [x.value, x]));
  const tree = new Map((bank.meta.topicTree ?? []).map((d) => [d.domain, d.groups]));
  const rangeOpts = ranges(bank, (from, to) => dash("rangeLabel", { from, to }), locale);

  const box = "flex cursor-pointer items-center gap-2.5 rounded-lg bg-surface px-3 py-2 text-sm has-[:checked]:bg-accent-soft has-[:checked]:font-semibold";

  return (
    <PracticeShell home={t("home")} title={dash("drillPick")} sub={t("pickSub", { domain: domainLabel })}>
      <form method="GET" action={`/banks/${slug}/practice/pick/start`} className="space-y-6">
        <fieldset className="card p-5">
          <legend className="sr-only">{t("pickStatus")}</legend>
          <div aria-hidden className="eyebrow mb-3">{t("pickStatus")}</div>
          <div className="grid gap-2 sm:grid-cols-3">
            {STATUSES.map((s) => (
              <label key={s} className={box}>
                <input type="radio" name="st" value={s} defaultChecked={s === "all"} />
                {dash(`status_${s}`)}
              </label>
            ))}
          </div>
          <p className="mt-3 text-xs text-muted">{t("pickBookmarkNote")}</p>
        </fieldset>

        <fieldset className="card p-5">
          <legend className="sr-only">{t("pickTags", { domain: domainLabel })}</legend>
          <div aria-hidden className="eyebrow mb-3">{t("pickTags", { domain: domainLabel })}</div>
          <div className="space-y-3">
            {domains.map((d) => {
              const groups = tree.get(d.value) ?? [];
              return (
                <div key={d.id} className="space-y-2">
                  <label className={box}>
                    <input type="checkbox" name="t" value={String(d.id)} />
                    <span className="font-semibold">{tagName(d, locale)}</span>
                  </label>
                  {groups.length > 0 && (
                    <div className="grid gap-2 pl-6 sm:grid-cols-2">
                      {groups.map((g) => {
                        const ids = g.topics.map((v) => topicByValue.get(v)?.id).filter((x): x is number => x != null);
                        if (ids.length === 0) return null;
                        return (
                          <label key={g.name} className={box}>
                            {/* ⭐ 一个分组 = 它下面几个中分類，值是逗号串；start 路由把所有勾选拍平成并集 */}
                            <input type="checkbox" name="t" value={ids.join(",")} />
                            {g.name}
                          </label>
                        );
                      })}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </fieldset>

        {rangeOpts.length > 1 && (
          <fieldset className="card p-5">
            <legend className="sr-only">{t("pickRange")}</legend>
          <div aria-hidden className="eyebrow mb-3">{t("pickRange")}</div>
            <select name="r" defaultValue="" className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm">
              <option value="">{t("pickRangeAll")}</option>
              {rangeOpts.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </fieldset>
        )}

        <button type="submit" className="rounded-xl px-6 py-2.5 text-sm font-semibold"
                style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}>
          {t("pickGo")}
        </button>
      </form>
    </PracticeShell>
  );
}
