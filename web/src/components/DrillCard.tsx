"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { ArrowLeft, ArrowRight, Check, X } from "lucide-react";
import { useTranslations } from "next-intl";
import type { Choice, DrillContext, Reference } from "@/lib/claims";
import { QuestionBody } from "./QuestionBody";

/**
 * 刷题卡片：作答 → 揭晓（**此刻即落库**）→ 答案主张 + 解析 → 下一题。
 *
 * ## ⭐ 2026-10-08（P9 #15）：去掉自评
 *
 * 用户裁定：「不再需要判断懂不懂。因此没有人会用。」—— 9/8 那次已实证：
 * 一个用户连续多天在用却从不点自评。界面上的四键自评就此移除。
 *
 * FSRS 搁置但算法保留：后端在记录作答时按对错代为打分（对 Good / 错 Again），
 * 卡片照常在后台推进，界面上什么都不显示（见 backend study.RecordAttempt）。
 *
 * ## 2026-09-08 起不变的一条：作答本身就是事实
 *
 * 揭晓那一刻就 POST /api/attempts 落库，⛔ 不依赖任何后续动作才能存活。
 * 记录失败与会话过期**必须**显示出来 —— 那是「这题没记上」的唯一信号。
 */

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
  const t = useTranslations("drill");
  const source = useTranslations("source");
  const [picked, setPicked] = useState<string[]>([]);
  // ⚠️ expired 与 failed 分开：会话过期时重试【永远不会成功】，
  // 而原来两者都显示「记录失败了，可以重试」—— 用户会反复点，
  // 每一题都记不上，且完全不知道原因（刷了半小时白刷）。
  //
  // ⭐ saveState 描述的是【这次作答有没有被记下来】（揭晓时决定）。
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

  // ⭐ 揭晓那一刻就把作答记下来 —— ⛔ 不等自评。
  //
  // ⚠️ 用 ref 而不是看 saveState 做去重：揭晓在一次渲染里可能触发多次
  // （单选的 ready 与多选的 confirmed 都会让 revealed 翻真），
  // 而 setState 是异步的 —— 靠状态判重会漏，靠 ref 才是同步的。
  const recorded = useRef(false);
  useEffect(() => {
    if (!revealed || recorded.current) return;
    recorded.current = true;
    void recordAnswer();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [revealed]);

  async function recordAnswer() {
    setSaveState("saving");
    try {
      const res = await fetch("/api/attempts", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          questionId,
          chosen,
          // ⛔ 不带 rating：自评已从界面移除，FSRS 由后端按对错代打
          durationMs: Date.now() - startedAt.current,
          context,
        }),
      });
      if (res.ok) {
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
          <p className="text-sm text-muted">{t("pickOne")}</p>
        ) : (
          // 多选：留一次确认，在此之前可以随意改选
          <button
            type="button"
            disabled={!ready}
            onClick={() => setConfirmed(true)}
            className="inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-sm font-medium transition-opacity disabled:opacity-35"
            style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
          >
            {ready ? t("submitN", { n: pickCount }) : t("needN", { n: pickCount, picked: picked.length })}
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
              {t.rich("noReference", { b: (c) => <b className="text-ink">{c}</b> })}
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
                {t("youPicked")} <b className="font-mono">{chosen}</b>
                {!correct && (
                  <>
                    {t("referenceIs")} <b className="font-mono">{reference.answer}</b>
                  </>
                )}
                <span className="ml-2 text-xs text-muted">
                  {t("basedOn", { source: source(reference.source) })}
                </span>
              </span>
            </div>
          )}

          {/* ⭐ 记录成功时什么都不显示。⛔ 但失败与过期必须显示 ——
              那是「这题没记上」的唯一信号，去掉它就回到「刷了半天白刷、两边都不报错」的形状。 */}
          {saveState === "failed" && (
            <p className="rounded-md border border-line bg-raise px-4 py-3 text-xs text-muted"
               style={{ boxShadow: "inset 3px 0 0 var(--color-warn)" }}>
              {t("saveFailed")}
            </p>
          )}
          {saveState === "expired" && (
            <div
              className="rounded-md border px-4 py-3 text-[0.8rem]"
              style={{ borderColor: "var(--color-warn)",
                       background: "color-mix(in oklab, var(--color-warn) 8%, transparent)" }}
            >
              <div className="font-semibold text-ink">{t("saveExpired")}</div>
              <p className="mt-1 text-muted">
                {t("reloginHint")}
                <Link href="/auth/login" className="ml-1 underline underline-offset-4"
                      style={{ color: "var(--color-src-community)" }}>
                  {t("goLogin")}
                </Link>
              </p>
            </div>
          )}

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
  const t = useTranslations("drill");
  // ⭐ 2026-09-08：`answered` 现在的含义是【这次作答已落库】（揭晓即发生），
  //   ⛔ 不再是「已自评」。所以揭晓之后前进就是正常的下一题，
  //   「跳过（不记录）」只适用于**没答就走**的情况。
  //
  // ⚠️ 保留下面这条视觉纪律 —— 它当初的教训依然成立：
  // 2026-09-05 实测，把「跳过」做成醒目主色块，用户以为那就是前进的路，
  // 点下去这题从未被记录，当天刷了一轮 attempt 表一条没多。
  // ⇒ 真正会丢弃这道题的动作，⛔ 永远不能比记录它的动作更醒目。
  //
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
          {t("prev")}
        </Link>
      ) : (
        <span />
      )}
      {forward && (
        skipping ? (
          <Link
            href={forward}
            className="inline-flex items-center gap-1.5 text-sm text-muted transition-colors hover:text-ink"
            title={t("skipTitle")}
          >
            {t("skip")}
            <ArrowRight size={14} />
          </Link>
        ) : (
          <Link
            href={forward}
            className="inline-flex items-center gap-2 rounded-md px-5 py-2.5 text-sm font-medium"
            style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}
          >
            {t("next")}
            <ArrowRight size={15} />
          </Link>
        )
      )}
    </nav>
  );
}
