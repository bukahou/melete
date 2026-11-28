import { useTranslations } from "next-intl";
import { Markdown } from "@/components/Markdown";
import type { PickedExplanation } from "@/lib/claims";

/**
 * 题目解析。显示哪一份由后端按请求语言挑好（QuestionDetail.explanation）；
 * 没有该语言的版本时后端给原文并把 localized 标成 false，这里如实说出来，⛔ 不静默回退。
 */
export function ExplanationPanel({ explanation }: { explanation: PickedExplanation }) {
  const t = useTranslations("explanation");
  return (
    <section>
      <h3 className="section-rule">
        <span className="eyebrow">{t("title")}</span>
      </h3>
      {!explanation.localized && <p className="mt-3 text-xs text-muted">{t("fallback")}</p>}
      <div className="mt-5 card p-6 sm:p-8">
        <Markdown>{explanation.body}</Markdown>
      </div>
    </section>
  );
}
