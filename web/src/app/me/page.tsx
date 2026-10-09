import Link from "next/link";
import { unstable_rethrow } from "next/navigation";
import { ChevronRight, LogOut, Monitor, Settings, ShieldCheck } from "lucide-react";
import { getLocale, getTranslations } from "next-intl/server";
import { getMyProfile, getSessions, listBanks, type Bank, type SessionInfo, type Tier } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import { UserAvatar } from "@/components/UserAvatar";

export const revalidate = 0;

export async function generateMetadata() {
  return { title: (await getTranslations("me"))("title") };
}

/**
 * 我的（2026-10-09 用户裁定，P9 #31）：管「这个账号」—— 资料 · 档位 · 能看的题库 · 登录设备 · 登出。
 * 「设置」只管「这个应用的偏好」（题库 / 语言 / 外观）。
 *
 * ⭐ 资料来自第三方账号、每次登录同步；Melete 里 ⛔ 没有修改资料的入口（P9 #25）。
 * ⭐ 登出只在这里（这一页唯一的实底按钮）—— 侧栏图标与设置页底部的登出已去掉，同一件事只留一个地方。
 * ⚠️ 登出后能否「换账号」取决于上游：现在只登出 Melete（并联动第三方的 end_session）；
 *    「换个账号登录」需要 akasha 的 oidcrp 支持 prompt=select_account，记在 tracker。
 */
const NOTICE: Record<string, { key: string; tone: "ok" | "warn" }> = {
  "sess-ok": { key: "noticeSessOk", tone: "ok" },
  fail: { key: "noticeFail", tone: "warn" },
};

function TierPill({ tier, label }: { tier: Tier; label: string }) {
  const style =
    tier === "admin" ? "bg-ink text-raise" :
    tier === "advanced" ? "bg-accent-soft text-accent-ink" :
    "bg-line-2 text-muted";
  return <span className={`shrink-0 rounded-full px-2.5 py-0.5 text-[0.72rem] font-semibold ${style}`}>{label}</span>;
}

function CardTitle({ icon, children, side }: { icon?: React.ReactNode; children: React.ReactNode; side?: React.ReactNode }) {
  return (
    <div className="mb-3 flex items-center gap-3">
      {icon && <span className="text-muted">{icon}</span>}
      <h2 className="shrink-0 text-[0.95rem] font-semibold">{children}</h2>
      <span className="h-px flex-1 bg-line" />
      {side && <span className="shrink-0 text-[0.78rem] text-muted">{side}</span>}
    </div>
  );
}

export default async function MePage({ searchParams }: { searchParams: Promise<{ n?: string }> }) {
  const sp = await searchParams;
  const [t, locale, profile, banks] = await Promise.all([
    getTranslations("me"), getLocale(), getMyProfile(), listBanks(),
  ]);
  const notice = sp.n ? NOTICE[sp.n] : undefined;

  // 登录设备：取不到不让整页 500（⛔ 先放行 Next 的内部信号，否则 401 时的续期会被吞掉）
  let sessions: SessionInfo[] = [];
  let sessionsFailed = false;
  try {
    sessions = await getSessions();
  } catch (e) {
    unstable_rethrow(e);
    sessionsFailed = true;
  }

  return (
    <div className="mx-auto grid max-w-2xl grid-cols-1 gap-4 pt-2">
      <h1 className="display text-[1.4rem]">{t("title")}</h1>

      {notice && (
        <div className="rounded-md border px-4 py-3 text-sm"
             style={{
               borderColor: `var(--color-${notice.tone})`,
               background: `color-mix(in oklab, var(--color-${notice.tone}) 8%, transparent)`,
             }}>
          {t(notice.key)}
        </div>
      )}

      {/* 资料：来自第三方账号，只读 */}
      <section className="card px-5 py-5">
        <div className="flex items-center gap-4">
          <UserAvatar name={profile.displayName} url={profile.avatarUrl} size={56} />
          <div className="min-w-0 flex-1">
            <p className="flex items-center gap-2">
              <span className="truncate text-[1.1rem] font-semibold">{profile.displayName}</span>
              <TierPill tier={profile.tier} label={t(`tier_${profile.tier}`)} />
            </p>
            {profile.email && <p className="truncate text-[0.82rem] text-muted">{profile.email}</p>}
            <p className="text-[0.74rem] text-muted">
              {t("joined", { when: timeAgo(profile.createdAt, locale) })}
              {" · "}
              {profile.lastLoginAt ? t("lastLogin", { when: timeAgo(profile.lastLoginAt, locale) }) : t("neverLogin")}
            </p>
          </div>
        </div>
        <p className="mt-4 border-t border-line-2 pt-3 text-[0.74rem] text-muted">{t("profileNote")}</p>
      </section>

      {/* 我的题库：只列看得到的。⛔ 不写「普通账号只能看公开题库」之类的话 —— 那等于告诉他还有看不到的题库（2026-10-09 用户） */}
      <section className="card px-5 py-4">
        <CardTitle>{t("banksTitle")}</CardTitle>
        <ul className="grid grid-cols-1 gap-1.5">
          {banks.map((b: Bank) => (
            <li key={b.slug} className="flex items-center gap-2 text-[0.88rem]">
              <span className="min-w-0 flex-1 truncate">{b.name}</span>
              {b.visibility === "private" && (
                <span className="shrink-0 rounded-full border border-line px-2 py-0.5 text-[0.68rem] text-muted">{t("private")}</span>
              )}
            </li>
          ))}
        </ul>
      </section>

      {/* 登录设备（原在设置页） */}
      <section className="card px-5 py-4">
        <CardTitle icon={<Monitor size={15} />} side={sessionsFailed ? undefined : t("devCount", { n: sessions.length })}>
          {t("devTitle")}
        </CardTitle>
        {sessionsFailed ? (
          <p className="text-sm text-muted">{t("devFailed")}</p>
        ) : (
          <ul className="grid grid-cols-1 gap-2 text-[0.86rem]">
            {sessions.map((s) => (
              <li key={s.id} className="flex items-baseline gap-3">
                <span className={`min-w-0 truncate ${s.current ? "font-medium text-ink" : "text-muted"}`}>
                  {s.deviceInfo?.slice(0, 60) || t("devUnknown")}
                </span>
                {s.current && (
                  <span className="shrink-0 rounded-sm border border-line px-1.5 text-[0.68rem] text-muted">{t("devCurrent")}</span>
                )}
                <time className="ml-auto shrink-0 text-[0.74rem] text-muted tabular-nums">{timeAgo(s.lastActiveAt, locale)}</time>
              </li>
            ))}
          </ul>
        )}
        {sessions.length > 1 && (
          <form method="POST" action="/me/sessions" className="mt-4">
            <button type="submit" className="rounded-lg border px-4 py-2 text-[0.82rem] font-semibold"
                    style={{ borderColor: "var(--color-warn)", color: "var(--color-warn)" }}>
              {t("devLogoutOthers")}
            </button>
          </form>
        )}
        <p className="mt-3 text-[0.74rem] text-muted">{t("devNote")}</p>
      </section>

      {/* 去别处：用户管理（只有 admin）· 设置 —— 电脑与手机都只从这里进（P9 #34），侧栏 / 底栏不单列 */}
      <section className="card divide-y divide-line-2 overflow-hidden">
        {profile.tier === "admin" && (
          <Link href="/admin" className="flex items-center gap-3 px-5 py-3 hover:bg-accent-soft">
            <ShieldCheck size={16} className="text-muted" />
            <span className="flex-1 text-[0.92rem]">{t("admin")}</span>
            <ChevronRight size={15} className="text-muted" />
          </Link>
        )}
        <Link href="/settings" className="flex items-center gap-3 px-5 py-3 hover:bg-accent-soft">
          <Settings size={16} className="text-muted" />
          <span className="flex-1 text-[0.92rem]">{t("settings")}</span>
          <ChevronRight size={15} className="text-muted" />
        </Link>
      </section>

      {/* 登出：这一页唯一的实底按钮 */}
      <a href="/auth/logout"
         className="flex items-center justify-center gap-2 rounded-lg py-3 text-[0.92rem] font-semibold transition-opacity hover:opacity-90"
         style={{ background: "var(--color-cta)", color: "var(--color-cta-fg)" }}>
        <LogOut size={16} />{t("logout")}
      </a>
    </div>
  );
}
