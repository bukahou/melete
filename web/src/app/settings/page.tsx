import Link from "next/link";
import { unstable_rethrow } from "next/navigation";
import { Check, Languages, Library, Palette } from "lucide-react";
import { cookies } from "next/headers";
import { DEFAULT_THEME, THEMES, THEME_COOKIE, isTheme } from "@/lib/theme";
import { getMyBank, listBanks, type Bank, type CurrentBank } from "@/lib/api";
import { getLocale, getTranslations } from "next-intl/server";
import { LOCALES, LOCALE_LABEL } from "@/i18n/locales";

export const revalidate = 0;

export async function generateMetadata() {
  return { title: (await getTranslations("settings"))("title") };
}

/**
 * 设置：题库 · 语言 · 主题 —— 只管「这个应用的偏好」。
 * ⭐ 2026-10-09 起「登录设备」与「登出」搬到「我的」：那里管「这个账号」（P9 #31）。
 *
 * ⭐ 2026-10-08 用户裁定：登录只用第三方（Akasha），自动注册后【不再能改账号信息】——
 *   改密码、改邮箱两节已撤下（后端端点暂留，待确认 iOS 不用后再删，见 tracker）。
 *   「登录设备 / 登出其他设备」保留：它不改账号信息，是丢了设备时的安全开关。
 *
 * 以下是撤下前的历史说明：账号设置原是阶段 5 那六个后端端点的前端入口。
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
function Section({ id, icon, title, hint, children }: {
  id?: string; icon: React.ReactNode; title: string; hint?: string; children: React.ReactNode;
}) {
  return (
    <section id={id} className="card scroll-mt-6">
      <div className="flex items-baseline gap-3 px-6 pt-5">
        <span className="translate-y-0.5 text-muted">{icon}</span>
        <span className="eyebrow">{title}</span>
        {hint && <span className="ml-auto text-[0.78rem] text-muted">{hint}</span>}
      </div>
      <div className="px-6 pb-6 pt-4">{children}</div>
    </section>
  );
}

function Field(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className="w-full rounded-lg border border-transparent bg-surface px-3 py-2 text-sm outline-none transition-colors focus:border-accent-ink"
    />
  );
}

/**
 * 设置页的提交按钮。⭐ 2026-10-08 改版：淡底而不是强调色实底 ——
 * 设置页每一节的「保存」地位相同，都不是这一屏唯一最该点的东西（层级规则 1）；
 * 旧版五个实底按钮一字排开，又回到了「全是重点 = 没有重点」。
 */
function Submit({ children, tone = "cta" }: { children: React.ReactNode; tone?: "cta" | "warn" }) {
  const style = tone === "warn"
    ? { borderColor: "var(--color-warn)", color: "var(--color-warn)" }
    : { background: "var(--color-accent-soft)", color: "var(--color-accent-ink)" };
  return (
    <button
      type="submit"
      className={`rounded-lg px-4 py-2 text-sm font-semibold ${tone === "warn" ? "border" : ""}`}
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
  "lang-ok": { key: "noticeLangOk", tone: "ok" },
  "theme-ok": { key: "noticeThemeOk", tone: "ok" },
  "rate": { key: "noticeRate", tone: "warn" },
  "fail": { key: "noticeFail", tone: "warn" },
};

export default async function SettingsPage({
  searchParams,
}: { searchParams: Promise<{ n?: string }> }) {
  const sp = await searchParams;
  const [t, locale] = await Promise.all([getTranslations("settings"), getLocale()]);
  const notice = sp.n ? NOTICE[sp.n] : undefined;
  const themeCookie = (await cookies()).get(THEME_COOKIE)?.value;
  const theme = isTheme(themeCookie) ? themeCookie : DEFAULT_THEME;

  // 题库切换（P9 #1）：取不到也不让整页 500 —— 其余设置照常可用。
  // ⛔ 先放行 Next 的内部信号（redirect / notFound）—— 否则 api.ts 在 401 时发起的续期会被 catch 吞掉。
  let banks: Bank[] = [];
  let current: CurrentBank | null = null;
  try {
    [banks, current] = await Promise.all([listBanks(), getMyBank()]);
  } catch (e) {
    unstable_rethrow(e);
  }

  return (
    <div className="mx-auto max-w-2xl space-y-6 pt-4">
      <header>
        {/* 设置在「我的」之下（P9 #34）：面包屑一键回去 */}
        <nav className="flex items-center gap-2 text-[0.82rem] text-muted">
          <Link href="/me" className="hover:text-ink">{t("mine")}</Link>
          <span className="opacity-50">›</span>
          <span className="text-ink">{t("title")}</span>
        </nav>
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
        </div>
      )}

      {/* ⭐ 放在最上面：P9 起「换题库」只在这里做，是设置页最常用的一项。 */}
      <Section id="bank" icon={<Library size={15} />} title={t("bankTitle")} hint={t("bankHint")}>
        {banks.length === 0 ? (
          <p className="text-sm text-muted">{t("bankFailed")}</p>
        ) : (
          <ul className="divide-y divide-line-2">
            {banks.map((b) => {
              const chosen = current?.source === "chosen" && current.bankSlug === b.slug;
              return (
                <li key={b.slug} className="flex items-center gap-4 py-3 first:pt-0 last:pb-0">
                  <span className="min-w-0 flex-1">
                    <span className={`block truncate text-sm ${chosen ? "font-semibold text-ink" : ""}`}>{b.name}</span>
                    <span className="block font-mono text-[0.72rem] text-muted">{b.slug}</span>
                  </span>
                  {chosen ? (
                    <span className="inline-flex items-center gap-1 text-[0.8rem] text-muted">
                      <Check size={13} />{t("bankCurrent")}
                    </span>
                  ) : (
                    <form method="POST" action="/settings/bank">
                      <input type="hidden" name="bank" value={b.slug} />
                      <button
                        type="submit"
                        className="rounded-md border border-line px-3 py-1.5 text-[0.8rem] transition-colors hover:border-muted"
                      >
                        {t("bankUse")}
                      </button>
                    </form>
                  )}
                </li>
              );
            })}
          </ul>
        )}
        <p className="mt-4 text-xs leading-relaxed text-muted">
          {current?.source === "recent" && <>{t("bankRecentNote", { name: banks.find((b) => b.slug === current?.bankSlug)?.name ?? current.bankSlug ?? "" })} </>}
          {t("bankNote")}
        </p>
      </Section>

      {/* ⭐ 语言也放这里一份 —— 顶栏的切换器是「随手换」，这里是「账号的设置在哪」。
          两处写同一个 cookie，⛔ 不是两套状态。 */}
      <Section icon={<Languages size={15} />} title={t("langTitle")} hint={t("langHint")}>
        <form method="POST" action="/settings/language-form" className="space-y-3">
          <select
            name="locale"
            defaultValue={locale}
            className="w-full rounded-lg border border-transparent bg-surface px-3 py-2 text-sm outline-none transition-colors focus:border-accent-ink"
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

      <Section icon={<Palette size={15} />} title={t("themeTitle")} hint={t("themeHint")}>
        <form method="POST" action="/settings/theme-form" className="flex flex-wrap items-center gap-2">
          {THEMES.map((th) => (
            <label key={th}
                   className="flex cursor-pointer items-center gap-2 rounded-lg bg-surface px-3.5 py-2 text-sm has-[:checked]:bg-accent-soft has-[:checked]:font-semibold">
              <input type="radio" name="theme" value={th} defaultChecked={th === theme} className="accent-[var(--color-accent-ink)]" />
              {t(`theme_${th}`)}
            </label>
          ))}
          <Submit>{t("themeSubmit")}</Submit>
        </form>
      </Section>

    </div>
  );
}
