"use client";

import { useRef, useState } from "react";
import Link from "next/link";
import { ArrowLeft, ArrowRight, Check, X } from "lucide-react";
import { SOURCE_LABEL, type Choice, type DrillContext, type Reference, type ScheduleResult } from "@/lib/claims";
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

/** nextLine 把「下次什么时候再见」说成人话。 */
function nextLine(s: ScheduleResult): string {
  const ms = new Date(s.due).getTime() - Date.now();
  if (ms <= 0) return "已记录。这题还没稳，稍后会再出现。";
  const mins = Math.round(ms / 60_000);
  if (mins < 60) return `已记录。约 ${mins} 分钟后再见到它。`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `已记录。约 ${hours} 小时后再见到它。`;
  return `已记录。约 ${Math.round(hours / 24)} 天后再见到它。`;
}

export function DrillCard({
  questionId,
  stem,
  choices,
  pickCount,
  reference,
  context,
  nav,
  children,
}: {
  questionId: number;
  stem: string;
  choices: Choice[];
  pickCount: number;
  reference: Reference | null;
  /** 这道题是从哪个入口做的 —— 随 attempt 落库，是「上次专项」的唯一来源 */
  context: DrillContext;
  /**
   * 翻页链接。⚠️ 这里必须区分「答完之后的下一题」与「不答直接跳过」——
   *
   * wrong / unsure / unseen / due 这四个模式的题目集合会随作答【缩短】：
   * 做完 offset 0 那题它就离开集合，原来的 offset 1 变成 offset 0。
   * 此时再跳到 offset 1 会漏掉一题（2026-09-04 实测：队列 [3,1,2]，
   * 做完 #3 后点下一题落到 #2，#1 被跳过）。
   *
   * 所以答完之后要回到 offset 0（队列自己前进了），不答则照常 offset+1。
   * 服务端组件看不见「答没答」，只有这里看得见 —— 导航因此归属这里。
   */
  nav: { prevHref?: string; skipHref?: string; nextHref?: string; shrinking: boolean };
  children: React.ReactNode;
}) {
  const [picked, setPicked] = useState<string[]>([]);
  const [rated, setRated] = useState<number | null>(null);
  const [schedule, setSchedule] = useState<ScheduleResult | null>(null);
  // ⚠️ expired 与 failed 分开：会话过期时重试【永远不会成功】，
  // 而原来两者都显示「记录失败了，可以重试」—— 用户会反复点，
  // 每一题都记不上，且完全不知道原因（刷了半小时白刷）。
  const [saveState, setSaveState] =
    useState<"idle" | "saving" | "saved" | "failed" | "expired">("idle");
  const startedAt = useRef(Date.now());

  const chosen = picked.join("");
  const correct = reference != null && chosen === reference.answer;
  const ready = picked.length === pickCount;

  // ⭐ 选够即揭晓，没有单独的「揭晓」按钮。
  //
  // Anki 里翻面是必需的：正面没有选项，翻面【就是】你提交答案的动作。
  // 而选择题里【选中已经是提交】—— 多选题选够 pickCount 那一刻答案就完整了。
  // 那个按钮是在让人确认一件刚刚已经做过的事，是一次纯粹的形式，
  // 而形式在「刷题」这种高频重复的动作里是实打实的摩擦。
  // （2026-09-05 用户判断：「揭晓不属于刷题的感觉，会阻挡顺畅」。）
  //
  // ⚠️ 但【只有单选】自动揭晓。多选必须留一次确认：
  //   单选：点下去【就是】提交，不存在「还没想好」的中间态
  //   多选：选够 N 项 ≠ 我确认了 —— 你可能想换一项，而纸质考试也是能擦的
  // QuestionBody 的 toggle 里有 `if (revealed) return`，所以一旦揭晓就锁死；
  // 多选题若也自动揭晓，第二项点错就再也改不回来。
  // ⚠️ 这是 2026-09-05 我自己在自动揭晓那一版里引入的回归，审查时逮到。
  const [confirmed, setConfirmed] = useState(false);
  const revealed = pickCount === 1 ? ready : confirmed;

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
      if (res.ok) {
        const body = (await res.json().catch(() => null)) as { schedule?: ScheduleResult } | null;
        setSchedule(body?.schedule ?? null);
        setSaveState("saved");
      } else {
        // 401 = 会话过期（BFF 在转发前先查了会话）。重试无用，只能重新登录。
        setSaveState(res.status === 401 ? "expired" : "failed");
      }
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
        pickCount === 1 ? (
          // 单选：没有需要点的东西，只提示
          <p className="text-sm text-muted">选一个答案</p>
        ) : (
          // 多选：留一次确认，在此之前可以随意改选
          <button
            type="button"
            disabled={!ready}
            onClick={() => setConfirmed(true)}
            className="inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-sm font-medium transition-opacity disabled:opacity-35"
            style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
          >
            {ready ? `提交这 ${pickCount} 项` : `请选 ${pickCount} 项（已选 ${picked.length}）`}
          </button>
        )
      ) : (
        <>
          {/* ⚠️ 6 道题一条答案主张都没有（素材本身的缺陷）。
              原来这里是 `{reference && ...}` —— 没有参考答案时【什么都不显示】，
              用户选完答案看不到任何反馈，会以为界面坏了。
              判不了对错要明说，这跟「不偷偷改调度」是同一条：把状况摆出来。 */}
          {!reference && (
            <div className="rounded-md border border-line bg-raise p-4 text-sm text-muted"
                 style={{ boxShadow: "inset 3px 0 0 var(--color-muted)" }}>
              这道题没有任何答案来源，<b className="text-ink">无法判定对错</b> ——
              素材里就缺，不是你选错了。自评仍会记录，但它不参与正确率统计。
            </div>
          )}
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
                ? schedule
                  ? nextLine(schedule)
                  : "已记录。这个自评会进入你的复习计划。"
                : saveState === "failed"
                  ? "记录失败了，再点一次自评可以重试。"
                  : saveState === "expired"
                    ? "登录已过期 —— 这一题没有记录下来。"
                    : "这道题你答得怎么样？—— 自评决定它何时回到你的复习队列"}
            </p>
            {saveState === "expired" && (
              <div
                className="mt-3 rounded-md border px-3 py-2.5 text-[0.8rem]"
                style={{ borderColor: "var(--color-warn)",
                         background: "color-mix(in oklab, var(--color-warn) 8%, transparent)" }}
              >
                <div className="font-semibold text-ink">重新登录后这一题需要再做一次</div>
                <p className="mt-1 text-muted">
                  ⚠️ 继续往下刷也不会被记录。
                  <Link href="/auth/login" className="ml-1 underline underline-offset-4"
                        style={{ color: "var(--color-src-community)" }}>
                    去登录
                  </Link>
                </p>
              </div>
            )}
            {/* ⭐ 自评被下调时把理由摆出来，⛔ 不偷偷改调度。
                与三方答案主张并列展示是同一条哲学：不替学习者下结论，把分歧摆出来。 */}
            {schedule && schedule.effectiveRating !== schedule.rating && (
              <div
                className="mt-3 rounded-md border px-3 py-2.5 text-[0.8rem]"
                style={{ borderColor: "var(--color-warn)", background: "color-mix(in oklab, var(--color-warn) 8%, transparent)" }}
              >
                <div className="font-semibold text-ink">
                  你按了「{RATINGS[schedule.rating - 1]?.label}」，按「{RATINGS[schedule.effectiveRating - 1]?.label}」安排复习
                </div>
                <ul className="mt-1 space-y-0.5 text-muted">
                  {schedule.reasons.map((r) => (
                    <li key={r}>· {r}</li>
                  ))}
                </ul>
              </div>
            )}
            <div className="mt-3 grid grid-cols-4 gap-2">
              {RATINGS.map((r) => {
                const active = rated === r.value;
                return (
                  <button
                    key={r.value}
                    type="button"
                    disabled={saveState === "saving" || saveState === "saved" || saveState === "expired"}
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

      <DrillNav nav={nav} answered={saveState === "saved"} />
    </div>
  );
}

/** DrillNav 翻页。已作答且集合会缩短时走 nextHref，否则走 skipHref（＝跳过）。 */
function DrillNav({
  nav,
  answered,
}: {
  nav: { prevHref?: string; skipHref?: string; nextHref?: string; shrinking: boolean };
  answered: boolean;
}) {
  // ⛔⛔ 未自评时【不能】把前进按钮做成主 CTA。
  //
  // 2026-09-05 实测的后果：用户选完答案、看到揭晓，按钮已经是个醒目的
  // 主色块写着「跳过」—— 看起来就是前进的路，点下去这道题**从未被记录**，
  // FSRS 拿不到任何信号。当天浏览了一轮，attempt 表一条都没多。
  //
  // ⚠️ 问题不在文案而在【视觉权重】：把丢弃这道题的动作做得比记录它更醒目。
  // 现在未评分时它是一个安静的文字链接，并明写「不记录」。
  // ⚠️ 只有【会缩短的集合】才用 nextHref（它指向 offset 0）。
  // 普通浏览（无 mode）的下一题永远是 offset+1 —— 那种列表不会缩短。
  // 第一版写成 `nextHref ?? skipHref`，于是普通浏览也跳去了 offset 0。
  const skipping = nav.shrinking && !answered;
  const forward = nav.shrinking ? (answered ? nav.nextHref : nav.skipHref) : nav.skipHref;
  if (!nav.prevHref && !forward) return null;
  return (
    <nav className="flex items-center justify-between border-t border-line pt-6">
      {nav.prevHref ? (
        <Link href={nav.prevHref} className="inline-flex items-center gap-1.5 text-sm text-muted transition-colors hover:text-ink">
          <ArrowLeft size={15} />
          上一题
        </Link>
      ) : (
        <span />
      )}
      {forward && (
        skipping ? (
          <Link
            href={forward}
            className="inline-flex items-center gap-1.5 text-sm text-muted transition-colors hover:text-ink"
            title="不做自评就前进，这道题不会进入复习计划"
          >
            跳过（不记录）
            <ArrowRight size={14} />
          </Link>
        ) : (
          <Link
            href={forward}
            className="inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-sm font-medium"
            style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
          >
            下一题
            <ArrowRight size={15} />
          </Link>
        )
      )}
    </nav>
  );
}
