import Link from "next/link";
import { unstable_rethrow } from "next/navigation";
import { KeyRound, Monitor, Mail, ShieldAlert } from "lucide-react";
import { getSessions, type SessionInfo } from "@/lib/api";
import { timeAgo } from "@/lib/format";

export const revalidate = 0;
export const metadata = { title: "账号设置" };

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

const NOTICE: Record<string, { text: string; tone: "ok" | "warn" }> = {
  "pw-ok": { text: "密码已修改。其它设备已被登出。", tone: "ok" },
  "pw-old": { text: "当前密码不正确。", tone: "warn" },
  "pw-weak": { text: "新密码不符合要求（至少 8 位）。", tone: "warn" },
  "pw-breached": { text: "密码已修改 —— 但它出现在已知泄露集合中，建议尽快换一个。", tone: "warn" },
  "sess-ok": { text: "已登出其它设备。", tone: "ok" },
  "mail-sent": { text: "验证码已发往新邮箱。", tone: "ok" },
  "mail-ok": { text: "邮箱已更新，并已通知原邮箱。", tone: "ok" },
  "mail-bad": { text: "验证码无效或已过期。", tone: "warn" },
  "mail-taken": { text: "该邮箱已被占用。", tone: "warn" },
  "rate": { text: "操作过于频繁，请稍后再试。", tone: "warn" },
  "fail": { text: "操作失败，请重试。", tone: "warn" },
};

export default async function SettingsPage({
  searchParams,
}: { searchParams: Promise<{ n?: string; c?: string }> }) {
  const sp = await searchParams;
  // ⛔ 只按白名单映射文案，不回显任何来自 URL 的文字（与登录页同一条纪律）。
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
        <p className="eyebrow">账号设置</p>
        <h1 className="display mt-3 text-2xl">密码 · 登录设备 · 邮箱</h1>
      </header>

      {notice && (
        <div
          className="rounded-md border px-4 py-3 text-sm"
          style={{
            borderColor: notice.tone === "ok" ? "var(--color-ok)" : "var(--color-warn)",
            background: `color-mix(in oklab, var(--color-${notice.tone}) 8%, transparent)`,
          }}
        >
          {notice.text}
          {sp.c && sp.n === "pw-breached" && (
            <span className="ml-1 text-muted">（在已知泄露集合中出现 {sp.c} 次）</span>
          )}
        </div>
      )}

      <Section icon={<KeyRound size={15} />} title="密码" hint="改密后其它设备会被登出">
        <form method="POST" action="/settings/password" className="space-y-3">
          {/* ⭐ 留空 = 首次设置密码（纯 Akasha 账号）。
              ⚠️ 「要不要验旧密码」由账号有没有密码决定，⛔ 不由这里填不填决定 —— 后端判。 */}
          <Field name="old" type="password" autoComplete="current-password"
                 placeholder="当前密码（从未设过密码则留空）" />
          <Field name="new" type="password" required autoComplete="new-password"
                 placeholder="新密码（至少 8 位）" />
          <Submit>保存密码</Submit>
        </form>
        <p className="mt-3 text-xs leading-relaxed text-muted">
          ⚠️ 新密码会与已知泄露口令库比对。命中<b className="text-ink">不会阻止你</b>，
          只会告诉你它出现过多少次 —— 判断留给你。
        </p>
      </Section>

      <Section icon={<Monitor size={15} />} title="登录设备"
               hint={sessionsFailed ? "暂时取不到" : `${sessions.length} 台`}>
        {sessionsFailed ? (
          <p className="text-sm text-muted">列表暂时取不到，其余功能不受影响。</p>
        ) : (
          <>
            <ul className="space-y-2 text-sm">
              {sessions.map((s) => (
                <li key={s.id} className="flex items-baseline gap-3">
                  <span className={s.current ? "font-medium text-ink" : "text-muted"}>
                    {s.deviceInfo?.slice(0, 60) || "未知设备"}
                  </span>
                  {s.current && (
                    <span className="rounded-sm border border-line px-1.5 text-[0.68rem] text-muted">
                      当前
                    </span>
                  )}
                  <time className="ml-auto font-mono text-[0.74rem] text-muted">
                    {timeAgo(s.lastActiveAt)}
                  </time>
                </li>
              ))}
            </ul>
            {sessions.length > 1 && (
              <form method="POST" action="/settings/sessions" className="mt-5">
                <Submit tone="warn">登出其它设备</Submit>
              </form>
            )}
          </>
        )}
        <p className="mt-3 text-xs leading-relaxed text-muted">
          ⚠️ 这里不显示任何令牌 —— 那是撤销凭据，摆在界面上等于让能看到屏幕的人拿走会话。
        </p>
      </Section>

      <Section icon={<Mail size={15} />} title="邮箱" hint="找回密码要用它">
        <form method="POST" action="/settings/email" className="space-y-3">
          <input type="hidden" name="step" value="send" />
          <Field name="email" type="email" required placeholder="新邮箱地址" />
          <Submit>发送验证码</Submit>
        </form>
        <form method="POST" action="/settings/email" className="mt-4 space-y-3">
          <input type="hidden" name="step" value="confirm" />
          <Field name="code" required inputMode="numeric" placeholder="收到的 6 位验证码" />
          <Submit>确认更换</Submit>
        </form>
        <p className="mt-3 flex gap-2 text-xs leading-relaxed text-muted">
          <ShieldAlert size={14} className="mt-0.5 shrink-0" />
          <span>
            换成功后会给<b className="text-ink">原邮箱</b>发一封通知 ——
            那不是礼貌，是万一账号被人接管时<b className="text-ink">唯一会让你察觉的信号</b>。
          </span>
        </p>
      </Section>

      <p className="text-xs text-muted">
        <Link href="/me" className="underline underline-offset-4">← 回到我的学习</Link>
      </p>
    </div>
  );
}
