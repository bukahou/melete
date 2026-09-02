"use client";

import { useRef, useState } from "react";
import { Check, Eye, X } from "lucide-react";
import { SOURCE_LABEL, type Choice, type DrillContext, type Reference } from "@/lib/claims";
import { QuestionBody } from "./QuestionBody";

/**
 * 刷题卡片：作答 → 揭晓 → 四键自评。
 *
 * 自评是 FSRS 标准四键（用户拍板，见 learning-flows.md）——
 * 它同时是「不清楚的」集合的来源（rating ≤ 2）与 P3 记忆调度的输入。
 * 点了自评才落记录：作答 + 自评是一条完整的 attempt，不拆两次写。
 */

const RATINGS: Array<{ value: number; label: string; hint: string; color: string }> = [
  { value: 1, label: "不会", hint: "Again", color: "var(--color-warn)" },
  { value: 2, label: "模糊", hint: "Hard", color: "var(--color-src-bank)" },
  { value: 3, label: "掌握", hint: "Good", color: "var(--color-ok)" },
  { value: 4, label: "轻松", hint: "Easy", color: "var(--color-src-community)" },
];

export function DrillCard({
  questionId,
  stem,
  choices,
  pickCount,
  reference,
  context,
  children,
}: {
  questionId: number;
  stem: string;
  choices: Choice[];
  pickCount: number;
  reference: Reference | null;
  /** 这道题是从哪个入口做的 —— 随 attempt 落库，是「上次专项」的唯一来源 */
  context: DrillContext;
  children: React.ReactNode;
}) {
  const [picked, setPicked] = useState<string[]>([]);
  const [revealed, setRevealed] = useState(false);
  const [rated, setRated] = useState<number | null>(null);
  const [saveState, setSaveState] = useState<"idle" | "saving" | "saved" | "failed">("idle");
  const startedAt = useRef(Date.now());

  const chosen = picked.join("");
  const correct = reference != null && chosen === reference.answer;
  const ready = picked.length === pickCount;

  async function rate(rating: number) {
    if (saveState === "saving" || saveState === "saved") return;
    setRated(rating);
    setSaveState("saving");
    try {
      const res = await fetch("/api/attempts", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          questionId,
          chosen,
          rating,
          durationMs: Date.now() - startedAt.current,
          context,
        }),
      });
      setSaveState(res.ok ? "saved" : "failed");
    } catch {
      setSaveState("failed");
    }
  }

  return (
    <div className="space-y-8">
      <QuestionBody
        stem={stem}
        choices={choices}
        pickCount={pickCount}
        selected={picked}
        onSelect={setPicked}
        revealed={revealed}
        correctAnswer={reference?.answer}
      />

      {!revealed ? (
        <button
          type="button"
          disabled={!ready}
          onClick={() => setRevealed(true)}
          className="inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-sm font-medium transition-opacity disabled:opacity-35"
          style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
        >
          <Eye size={15} />
          {ready ? "揭晓" : pickCount > 1 ? `请选 ${pickCount} 项（已选 ${picked.length}）` : "请先作答"}
        </button>
      ) : (
        <>
          {reference && (
            <div
              className="flex items-center gap-3 rounded-md border border-line bg-raise p-4 text-sm"
              style={{ boxShadow: `inset 3px 0 0 ${correct ? "var(--color-ok)" : "var(--color-warn)"}` }}
            >
              <span
                className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full"
                style={{
                  background: correct ? "var(--color-ok)" : "var(--color-warn)",
                  color: "var(--color-raise)",
                }}
              >
                {correct ? <Check size={14} /> : <X size={14} />}
              </span>
              <span>
                你选了 <b className="font-mono">{chosen}</b>
                {!correct && (
                  <>
                    ，参考答案 <b className="font-mono">{reference.answer}</b>
                  </>
                )}
                <span className="ml-2 text-xs text-muted">以「{SOURCE_LABEL[reference.source]}」为准</span>
              </span>
            </div>
          )}

          {/* 四键自评 —— 记录这次作答的必经之路 */}
          <div className="rounded-md border border-line bg-raise p-4">
            <p className="text-xs text-muted">
              {saveState === "saved"
                ? "已记录。这个自评会进入你的复习计划。"
                : saveState === "failed"
                  ? "记录失败了，可以重试。"
                  : "这道题你答得怎么样？—— 自评决定它何时回到你的复习队列"}
            </p>
            <div className="mt-3 grid grid-cols-4 gap-2">
              {RATINGS.map((r) => {
                const active = rated === r.value;
                return (
                  <button
                    key={r.value}
                    type="button"
                    disabled={saveState === "saving" || saveState === "saved"}
                    onClick={() => rate(r.value)}
                    className="rounded-md border py-2.5 text-center transition-all disabled:cursor-default"
                    style={{
                      borderColor: active ? r.color : "var(--color-line)",
                      background: active
                        ? `color-mix(in oklab, ${r.color} 12%, transparent)`
                        : "transparent",
                      opacity: saveState === "saved" && !active ? 0.35 : 1,
                    }}
                  >
                    <span className="block text-sm font-medium" style={active ? { color: r.color } : undefined}>
                      {r.label}
                    </span>
                    <span className="mt-0.5 block font-mono text-[0.62rem] uppercase tracking-wider text-muted">
                      {r.hint}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>

          {children}
        </>
      )}
    </div>
  );
}
