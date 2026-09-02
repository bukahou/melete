import { AlertTriangle } from "lucide-react";
import { SOURCE_LABEL, hasDisagreement, voteDistribution, type AnswerClaim } from "@/lib/claims";

/**
 * 答案主张并列面板 —— 本项目的核心 UI。
 *
 * 每个来源一枚「印章」：顶边来源色条 + 标签 + 巨大的答案字母。
 * 绝不合并成单一「正确答案」—— 题库说 A、86% 的人说 B、AI 判 B 并给理由，
 * 这个对峙本身就是要呈现的学习材料。
 * 身份 = 色条 + 文字标签，从不只靠颜色（dataviz 规范）。
 */

const SOURCE_COLOR: Record<AnswerClaim["source"], string> = {
  bank_label: "var(--color-src-bank)",
  community_vote: "var(--color-src-community)",
  ai_verdict: "var(--color-src-ai)",
  user_note: "var(--color-ok)",
};

/** 投票分布：横条列表。4px 圆角端、hairline 轨道（dataviz 规范）。 */
function VoteBars({ claim }: { claim: AnswerClaim }) {
  const dist = voteDistribution(claim);
  if (dist.length === 0) return null;
  return (
    <div className="mt-3 space-y-1.5 border-t border-line pt-3">
      {dist.map(([letter, pct]) => (
        <div key={letter} className="flex items-center gap-2 text-[0.7rem]">
          <span className="w-5 font-mono text-muted">{letter}</span>
          <div className="h-[5px] flex-1 overflow-hidden rounded-[4px] bg-line">
            <div
              className="h-full rounded-[4px]"
              style={{ width: `${pct}%`, background: "var(--color-src-community)" }}
            />
          </div>
          <span className="w-8 text-right tabular-nums text-muted">{pct}%</span>
        </div>
      ))}
    </div>
  );
}

export function ClaimsPanel({ claims }: { claims: AnswerClaim[] }) {
  if (claims.length === 0) {
    return <p className="text-sm text-muted">这道题还没有任何答案主张。</p>;
  }
  const disputed = hasDisagreement(claims);
  const rationales = claims.filter((c) => c.rationale);

  return (
    <section>
      <h3 className="section-rule">
        <span className="eyebrow">答案主张</span>
        {disputed && (
          <span className="inline-flex items-center gap-1 text-xs font-medium" style={{ color: "var(--color-warn)" }}>
            <AlertTriangle size={12} />
            各方不一致
          </span>
        )}
      </h3>

      <div className="mt-5 grid gap-3 sm:grid-cols-3">
        {claims.map((claim) => {
          const color = SOURCE_COLOR[claim.source];
          return (
            <div
              key={claim.source}
              className="rounded-md border border-line bg-raise p-4"
              style={{ borderTop: `3px solid ${color}` }}
            >
              <div className="flex items-baseline justify-between gap-2">
                <span className="text-xs font-medium text-muted">{SOURCE_LABEL[claim.source]}</span>
                {claim.confidence != null && (
                  <span className="tabular-nums text-xs text-muted">{claim.confidence}%</span>
                )}
              </div>
              <div
                className="display mt-2 text-[2.4rem] leading-none tracking-[0.08em]"
                style={{ fontFamily: "var(--font-mono-x)", color }}
              >
                {claim.answer}
              </div>
              {claim.source === "community_vote" && <VoteBars claim={claim} />}
            </div>
          );
        })}
      </div>

      {rationales.map((claim) => (
        <blockquote
          key={`${claim.source}-r`}
          className="mt-4 rounded-r-md border-l-2 bg-raise py-4 pl-5 pr-5"
          style={{ borderLeftColor: SOURCE_COLOR[claim.source] }}
        >
          <p className="text-xs font-medium" style={{ color: SOURCE_COLOR[claim.source] }}>
            {SOURCE_LABEL[claim.source]}的理由
          </p>
          <p className="mt-2 text-[0.9rem] leading-[1.9]">{claim.rationale}</p>
        </blockquote>
      ))}
    </section>
  );
}
