import Link from "next/link";
import { unstable_rethrow } from "next/navigation";
import { KeyRound, Languages, Monitor, Mail, ShieldAlert } from "lucide-react";
import { getSessions, type SessionInfo } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import { getLocale, getTranslations } from "next-intl/server";
import { LOCALES, LOCALE_LABEL } from "@/i18n/locales";

export const revalidate = 0;

export async function generateMetadata() {
  return { title: (await getTranslations("settings"))("title") };
}

/**
 * 账号设置 —— 阶段 5 那六个后端端点的前端入口。
 *
 * ⚠️ 在此之前它们【后端能用、界面点不到】：
 * 改密、看登录设备、登出其它设备、改邮箱全都只能 curl。
 * ⭐ 案卷 §18.2 记过同一形状（geass-v3 的 service 层有实现但无端点）：
 * 「功能的『有』必须实测到最外层可达」—— 端点可达之后，
 * 下一层可达是【界面上点得到】。
 *
 * 形态沿用登录页：⛔ 不引入客户端状态，表单 POST 给 BFF 路由 → 303 回来。
 * 表单提交天然串行，⇒ 顺带避开并发刷新那类问题。
 */
function Section({ icon, title, hint, children }: {
  icon: React.ReactNode; title: string; hint?: string; children: React.ReactNode;
}) {
  return (
    <section className="rounded-lg border border-line bg-raise">
      <div className="flex items-baseline gap-3 border-b border-line px-6 py-4">
        <span className="translate-y-0.5 text-muted">{icon}</span>
        <span className="eyebrow">{title}</span>
        {hint && <span className="ml-auto text-[0.78rem] text-muted">{hint}</span>}
      </div>
      <div className="p-6">{children}</div>
    </section>
  );
}

function Field(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm outline-none transition-colors focus:border-muted"
    />
  );
}

function Submit({ children, tone = "cta" }: { children: React.ReactNode; tone?: "cta" | "warn" }) {
  const style = tone === "warn"
    ? { borderColor: "var(--color-warn)", color: "var(--color-warn)" }
    : { background: "var(--color-cta)", color: "var(--color-cta-fg)" };
  return (
    <button
      type="submit"
      className={`rounded-md px-4 py-2 text-sm font-medium ${tone === "warn" ? "border" : ""}`}
      style={style}
    >
      {children}
    </button>
  );
}

// ⚠️ 只映射【码 → 消息键 + 语气】，文案本身在 messages 里。
// ⛔ 仍然不回显任何来自 URL 的文字（与登录页同一条纪律）——
// 白名单换成 key 白名单，性质没变。
const NOTICE: Record<string, { key: string; tone: "ok" | "warn" }> = {
  "pw-ok": { key: "noticePwOk", tone: "ok" },
  "pw-old": { key: "noticePwOld", tone: "warn" },
  "pw-weak": { key: "noticePwWeak", tone: "warn" },
  "pw-breached": { key: "noticePwBreached", tone: "warn" },
  "sess-ok": { key: "noticeSessOk", tone: "ok" },
  "mail-sent": { key: "noticeMailSent", tone: "ok" },
  "mail-ok": { key: "noticeMailOk", tone: "ok" },
  "mail-bad": { key: "noticeMailBad", tone: "warn" },
  "mail-taken": { key: "noticeMailTaken", tone: "warn" },
  "lang-ok": { key: "noticeLangOk", tone: "ok" },
  "rate": { key: "noticeRate", tone: "warn" },
  "fail": { key: "noticeFail", tone: "warn" },
};

export default async function SettingsPage({
  searchParams,
}: { searchParams: Promise<{ n?: string; c?: string }> }) {
  const sp = await searchParams;
  const [t, locale] = await Promise.all([getTranslations("settings"), getLocale()]);
  const notice = sp.n ? NOTICE[sp.n] : undefined;

  let sessions: SessionInfo[] = [];
  let sessionsFailed = false;
  try {
    sessions = await getSessions();
  } catch (e) {
    // ⛔ 先放行 Next 的内部信号（redirect / notFound）—— 否则 api.ts 在 401 时
    //    发起的「去 /auth/renew 续期」会被这个 catch 吞掉，页面带着死 token 静静渲染，
    //    用户只看到「会话列表取不到」，永远续不上期。裸 `catch {}` 正是这种形状。
    unstable_rethrow(e);
    // ⚠️ 其它失败不该让整页 500 —— 改密与改邮箱仍然可用。
    sessionsFailed = true;
  }

  return (
    <div className="mx-auto max-w-2xl space-y-6 pt-4">
      <header>
        <p className="eyebrow">{t("title")}</p>
        <h1 className="display mt-3 text-2xl">{t("heading")}</h1>
      </header>

      {notice && (
        <div
          className="rounded-md border px-4 py-3 text-sm"
          style={{
            borderColor: notice.tone === "ok" ? "var(--color-ok)" : "var(--color-warn)",
            background: `color-mix(in oklab, var(--color-${notice.tone}) 8%, transparent)`,
          }}
        >
          {t(notice.key)}
          {sp.c && sp.n === "pw-breached" && (
            <span className="ml-1 text-muted">{t("breachCount", { n: sp.c })}</span>
          )}
        </div>
      )}

      <Section icon={<KeyRound size={15} />} title={t("pwTitle")} hint={t("pwHint")}>
        <form method="POST" action="/settings/password" className="space-y-3">
          {/* ⭐ 留空 = 首次设置密码（纯 Akasha 账号）。
              ⚠️ 「要不要验旧密码」由账号有没有密码决定，⛔ 不由这里填不填决定 —— 后端判。 */}
          <Field name="old" type="password" autoComplete="current-password"
                 placeholder={t("pwOld")} />
          <Field name="new" type="password" required autoComplete="new-password"
                 placeholder={t("pwNew")} />
          <Submit>{t("pwSubmit")}</Submit>
        </form>
        <p className="mt-3 text-xs leading-relaxed text-muted">
          {t.rich("pwNote", { b: (c) => <b className="text-ink">{c}</b> })}
        </p>
      </Section>

      <Section icon={<Monitor size={15} />} title={t("devTitle")}
               hint={sessionsFailed ? t("devUnavailable") : t("devCount", { n: sessions.length })}>
        {sessionsFailed ? (
          <p className="text-sm text-muted">{t("devFailed")}</p>
        ) : (
          <>
            <ul className="space-y-2 text-sm">
              {sessions.map((s) => (
                <li key={s.id} className="flex items-baseline gap-3">
                  <span className={s.current ? "font-medium text-ink" : "text-muted"}>
                    {s.deviceInfo?.slice(0, 60) || t("devUnknown")}
                  </span>
                  {s.current && (
                    <span className="rounded-sm border border-line px-1.5 text-[0.68rem] text-muted">
                      {t("devCurrent")}
                    </span>
                  )}
                  <time className="ml-auto font-mono text-[0.74rem] text-muted">
                    {timeAgo(s.lastActiveAt, locale)}
                  </time>
                </li>
              ))}
            </ul>
            {sessions.length > 1 && (
              <form method="POST" action="/settings/sessions" className="mt-5">
                <Submit tone="warn">{t("devLogoutOthers")}</Submit>
              </form>
            )}
          </>
        )}
        <p className="mt-3 text-xs leading-relaxed text-muted">{t("devNote")}</p>
      </Section>

      <Section icon={<Mail size={15} />} title={t("mailTitle")} hint={t("mailHint")}>
        <form method="POST" action="/settings/email" className="space-y-3">
          <input type="hidden" name="step" value="send" />
          <Field name="email" type="email" required placeholder={t("mailNew")} />
          <Submit>{t("mailSend")}</Submit>
        </form>
        <form method="POST" action="/settings/email" className="mt-4 space-y-3">
          <input type="hidden" name="step" value="confirm" />
          <Field name="code" required inputMode="numeric" placeholder={t("mailCode")} />
          <Submit>{t("mailConfirm")}</Submit>
        </form>
        <p className="mt-3 flex gap-2 text-xs leading-relaxed text-muted">
          <ShieldAlert size={14} className="mt-0.5 shrink-0" />
          <span>{t.rich("mailNote", { b: (c) => <b className="text-ink">{c}</b> })}</span>
        </p>
      </Section>

      {/* ⭐ 语言也放这里一份 —— 顶栏的切换器是「随手换」，这里是「账号的设置在哪」。
          两处写同一个 cookie，⛔ 不是两套状态。 */}
      <Section icon={<Languages size={15} />} title={t("langTitle")} hint={t("langHint")}>
        <form method="POST" action="/settings/language-form" className="space-y-3">
          <select
            name="locale"
            defaultValue={locale}
            className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm outline-none transition-colors focus:border-muted"
          >
            {LOCALES.map((l) => (
              <option key={l} value={l}>
                {LOCALE_LABEL[l]}
              </option>
            ))}
          </select>
          <Submit>{t("langSubmit")}</Submit>
        </form>
        <p className="mt-3 text-xs leading-relaxed text-muted">{t("langNote")}</p>
      </Section>

      <p className="text-xs text-muted">
        <Link href="/me" className="underline underline-offset-4">{t("backToMe")}</Link>
      </p>
    </div>
  );
}
