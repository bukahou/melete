import Link from "next/link";
import { notFound } from "next/navigation";
import { Shuffle } from "lucide-react";
import { getTranslations } from "next-intl/server";
import { ApiError, getBank, type BankDetail } from "@/lib/api";
import { PracticeShell } from "@/components/PracticeShell";

export const revalidate = 0;

/**
 * 4.4 随机（P9 #19）：进入时选题数 —— 10 / 20 / 一场考试。
 * 碎片时间最常用 10 题；「一场考试」就是模拟考，与合格判断同一个题数口径。
 */
export default async function RandomPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const [t, dash] = await Promise.all([getTranslations("practice"), getTranslations("dash")]);
  let bank: BankDetail;
  try {
    bank = await getBank(slug);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const exam = bank.meta.examQuestions;
  const total = bank.stats.questionCount;
  const options = [
    { n: 10, label: t("randomN", { n: 10 }), hint: t("randomHintShort") },
    { n: 20, label: t("randomN", { n: 20 }) },
    ...(exam && exam !== 10 && exam !== 20 ? [{ n: exam, label: t("randomExam", { n: exam }), hint: t("randomHintExam") }] : []),
  ].filter((o) => o.n <= total);

  return (
    <PracticeShell home={t("home")} title={dash("drillRandom")} sub={t("randomSub")}>
      <div className="grid gap-4 sm:grid-cols-3">
        {options.map((o) => (
          <Link
            key={o.n}
            href={`/banks/${slug}/practice/random/start?n=${o.n}`}
            className="group grid gap-2 rounded-lg border border-line bg-raise p-6 transition-colors hover:border-ink"
          >
            <Shuffle size={16} className="text-muted group-hover:text-ink" />
            <span className="display text-[1.5rem] leading-tight">{o.label}</span>
            {o.hint && <span className="text-[0.78rem] text-muted">{o.hint}</span>}
          </Link>
        ))}
      </div>
    </PracticeShell>
  );
}
