import Link from "next/link";
import { CircleDashed, HelpCircle, XCircle } from "lucide-react";
import { getBank, getMyProgress, getMyTagStats } from "@/lib/api";
import { tagTypeLabel, type TagType } from "@/lib/claims";
import { RateBar, RateLegend } from "@/components/RateBar";
import { getLocale, getTranslations } from "next-intl/server";

export async function generateMetadata() {
  return { title: (await getTranslations("me"))("title") };
}

/** 三条轴的角色与提示语是通用的；轴的**名字**从题库 meta 读，这里不写任何题库词。 */
const GROUPS: Array<{ type: TagType; hint: "hintDomain" | "hintTopic" | "hintConcept" }> = [
  { type: "domain", hint: "hintDomain" },
  { type: "topic", hint: "hintTopic" },
  { type: "concept", hint: "hintConcept" },
];

export default async function MePage() {
  const [t, locale] = await Promise.all([getTranslations("me"), getLocale()]);
  const progress = await getMyProgress();
  const [bank, ...groups] = await Promise.all([
    getBank(progress.bankSlug),
    ...GROUPS.map((g) => getMyTagStats(g.type, 2)),
  ]);

  const unseen = progress.questionCount - progress.seenCount;
  const rate = progress.seenCount ? (progress.correctCount / progress.seenCount) * 100 : 0;
  const slug = progress.bankSlug;

  return (
    <div className="space-y-14">
      <header>
        <p className="eyebrow">{t("title")}</p>
        <h1 className="display mt-4 text-3xl leading-tight">
          {progress.seenCount === 0
            ? t("empty")
            : t.rich("summary", {
                n: progress.seenCount,
                rate: Math.round(rate),
                // ⚠️ 数字上色靠 rich 的标记，⛔ 不把句子劈成三段拼接 ——
                // 那样日语的语序就没法调整了（「N 問を解答、正答率 X%」与中文结构不同）。
                num: (c) => <span className="tabular-nums">{c}</span>,
                pct: (c) => (
                  <span className="tabular-nums" style={{ color: rate < 60 ? "var(--color-warn)" : "var(--color-ok)" }}>
                    {c}
                  </span>
                ),
              })}
        </h1>
      </header>

      {progress.seenCount === 0 ? (
        <p className="text-sm leading-relaxed text-muted">
          {t("emptyHint")}
          <Link href={`/banks/${slug}/drill`} className="mx-1 underline underline-offset-4">
            {t("emptyLink")}
          </Link>
          {t("emptyTail")}
        </p>
      ) : (
        <>
          {/* 进度条：做过 / 未做，单一分段条比三个数字更快传达"还剩多少" */}
          <section className="space-y-3">
            <div className="meter" role="img"
                 aria-label={t("meterLabel", { seen: progress.seenCount, unseen })}>
              <span style={{ flex: progress.correctCount, background: "var(--color-ok)" }} />
              <span style={{ flex: progress.wrongCount, background: "var(--color-warn)" }} />
              <span style={{ flex: Math.max(unseen, 1), background: "var(--color-line)" }} />
            </div>
            <div className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted">
              {[
                [t("correct"), progress.correctCount, "var(--color-ok)"],
                [t("wrong"), progress.wrongCount, "var(--color-warn)"],
                [t("untouched"), unseen, "var(--color-line)"],
              ].map(([label, n, c]) => (
                <span key={label as string} className="inline-flex items-center gap-1.5">
                  <span className="h-2 w-2 rounded-full" style={{ background: c as string }} aria-hidden />
                  {label as string} <b className="tabular-nums text-ink">{n as number}</b>
                </span>
              ))}
              <span className="ml-auto">{t("attempts", { n: progress.attemptCount })}</span>
            </div>
          </section>

          {/* 三个入口：把统计直接变成行动 */}
          <section className="flex flex-wrap gap-3">
            {[
              { mode: "wrong", label: t("wrongBook"), n: progress.wrongCount, Icon: XCircle, color: "var(--color-warn)" },
              { mode: "unsure", label: t("unsureBook"), n: progress.unsureCount, Icon: HelpCircle, color: "var(--color-src-bank)" },
              { mode: "unseen", label: t("unseenBook"), n: unseen, Icon: CircleDashed, color: "var(--color-muted)" },
            ].map(({ mode, label, n, Icon, color }) => (
              <Link
                key={mode}
                href={`/banks/${slug}/drill?mode=${mode}`}
                className="inline-flex items-center gap-2 rounded-md border border-line bg-raise px-4 py-2.5 text-sm transition-colors hover:border-muted"
              >
                <Icon size={15} style={{ color }} />
                {label}
                <b className="tabular-nums">{n}</b>
              </Link>
            ))}
          </section>

          {/* 弱项分布 */}
          {GROUPS.map((g, i) => {
            const stats = groups[i];
            if (stats.length === 0) return null;
            return (
              <section key={g.type}>
                <h2 className="section-rule">
                  <span className="eyebrow">{tagTypeLabel(bank.meta, g.type, locale)}</span>
                  <span className="text-xs text-muted">{t(g.hint)}</span>
                </h2>
                <div className="mt-4 space-y-1">
                  {stats.map((s) => (
                    <RateBar key={s.tagId} stat={s} />
                  ))}
                </div>
                {i === 0 && (
                  <div className="mt-4">
                    <RateLegend />
                  </div>
                )}
              </section>
            );
          })}

          <p className="text-xs leading-relaxed text-muted">
            {t.rich("footnote", { b: (c) => <b className="text-ink">{c}</b> })}
          </p>
        </>
      )}
    </div>
  );
}
