import Link from "next/link";
import { notFound } from "next/navigation";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { getLocale, getTranslations } from "next-intl/server";
import { ApiError, listAdminUsers, type AdminUser, type AdminUserPage, type Tier } from "@/lib/api";
import { timeAgo } from "@/lib/format";

export const revalidate = 0;

/**
 * 用户管理（P9 #27–#29，设计见 docs/design/active/bank-access.md）。
 *
 * 三档：普通（公开题库）· 高级（全部题库）· admin（全部 + 管理普通 / 高级）。
 * admin 在这里把普通用户升级为高级、把高级降回普通。
 *   ⛔ admin 不能操作任何 admin（含自己）—— 那一行没有按钮，后端也拒绝。
 *   ⛔ 这里没有「设为 admin」—— 由超级用户（= 直接操作数据库的人）决定。
 * ⚠️ 非 admin 打开这一页：接口回 404 ⇒ 这里也是 404。侧栏不显示入口只是方便，门在后端。
 */
const NOTICE: Record<string, { key: string; tone: "ok" | "warn" }> = {
  ok: { key: "noticeOk", tone: "ok" },
  admin: { key: "noticeAdmin", tone: "warn" },
  fail: { key: "noticeFail", tone: "warn" },
};

/**
 * 头像：Akasha 带来的地址；没有就用显示名的第一个字。
 * ⚠️ 用 <img> 而不是 next/image：地址来自上游、域名不固定，⛔ 不为它开图片代理的白名单。
 * referrerPolicy=no-referrer：⛔ 不把 Melete 的管理页地址泄给头像的托管方。
 */
function Avatar({ user }: { user: AdminUser }) {
  if (user.avatarUrl) {
    // eslint-disable-next-line @next/next/no-img-element
    return <img src={user.avatarUrl} alt="" referrerPolicy="no-referrer" className="h-9 w-9 shrink-0 rounded-full object-cover" />;
  }
  return (
    <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[0.9rem] font-semibold text-accent-ink">
      {Array.from(user.displayName)[0] ?? "?"}
    </span>
  );
}

function TierPill({ tier, label }: { tier: Tier; label: string }) {
  const style =
    tier === "admin" ? "bg-ink text-raise" :
    tier === "advanced" ? "bg-accent-soft text-accent-ink" :
    "bg-line-2 text-muted";
  return <span className={`shrink-0 rounded-full px-2.5 py-0.5 text-[0.72rem] font-semibold ${style}`}>{label}</span>;
}

export default async function AdminPage({ searchParams }: { searchParams: Promise<{ page?: string; n?: string }> }) {
  const sp = await searchParams;
  const page = Math.max(1, Number(sp.page) || 1);
  const [t, locale] = await Promise.all([getTranslations("admin"), getLocale()]);
  let data: AdminUserPage;
  try {
    data = await listAdminUsers(page);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const notice = sp.n ? NOTICE[sp.n] : undefined;
  const pages = Math.max(1, Math.ceil(data.total / data.pageSize));
  const action = (u: AdminUser) =>
    u.tier === "admin" ? null : (
      <form method="POST" action="/admin/tier" className="shrink-0">
        <input type="hidden" name="userId" value={u.id} />
        <input type="hidden" name="tier" value={u.tier === "basic" ? "advanced" : "basic"} />
        <input type="hidden" name="page" value={page} />
        <button type="submit"
                className="rounded-lg px-3 py-1.5 text-[0.8rem] font-semibold"
                style={u.tier === "basic"
                  ? { background: "var(--color-accent-soft)", color: "var(--color-accent-ink)" }
                  : { border: "1px solid var(--color-line)", color: "var(--color-muted)" }}>
          {u.tier === "basic" ? t("upgrade") : t("downgrade")}
        </button>
      </form>
    );

  return (
    <div className="mx-auto grid max-w-3xl gap-4 pt-2">
      <header className="grid gap-1">
        <nav className="flex items-center gap-2 text-[0.82rem] text-muted">
          <Link href="/" className="hover:text-ink">{t("home")}</Link>
          <span className="opacity-50">›</span>
          <span className="text-ink">{t("title")}</span>
        </nav>
        <h1 className="display text-[1.4rem]">{t("title")}</h1>
      </header>

      {/* 三档说明：管理的人要知道「升级」到底意味着什么 */}
      <section className="card grid gap-2 px-5 py-4 text-[0.84rem]">
        {(["basic", "advanced", "admin"] as const).map((tier) => (
          <p key={tier} className="flex items-baseline gap-3">
            <TierPill tier={tier} label={t(`tier_${tier}`)} />
            <span className="text-muted">{t(`tierNote_${tier}`)}</span>
          </p>
        ))}
      </section>

      {notice && (
        <div className="rounded-md border px-4 py-3 text-sm"
             style={{
               borderColor: `var(--color-${notice.tone})`,
               background: `color-mix(in oklab, var(--color-${notice.tone}) 8%, transparent)`,
             }}>
          {t(notice.key)}
        </div>
      )}

      <section className="card overflow-hidden">
        <div className="flex items-center gap-3 px-5 pb-2 pt-4">
          <h2 className="shrink-0 text-[0.95rem] font-semibold">{t("users")}</h2>
          <span className="h-px flex-1 bg-line" />
          <span className="text-[0.78rem] text-muted">{t("total", { n: data.total })}</span>
        </div>
        <div className="divide-y divide-line-2 border-t border-line-2">
          {data.items.map((u) => (
            <div key={u.id} className="flex items-center gap-3 px-5 py-3">
              <Avatar user={u} />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[0.92rem]">{u.displayName}</span>
                <span className="block truncate text-[0.72rem] text-muted">{u.email ?? t("noEmail")}</span>
                <span className="block truncate text-[0.7rem] text-muted">
                  {t("joined", { when: timeAgo(u.createdAt, locale) })}
                  {" · "}
                  {u.lastLoginAt ? t("lastLogin", { when: timeAgo(u.lastLoginAt, locale) }) : t("neverLogin")}
                </span>
              </span>
              <TierPill tier={u.tier} label={t(`tier_${u.tier}`)} />
              <span className="w-[6.5rem] shrink-0 text-right">{action(u)}</span>
            </div>
          ))}
        </div>
        {pages > 1 && (
          <div className="flex items-center justify-between border-t border-line-2 px-5 py-3 text-[0.82rem]">
            {page > 1 ? <Link href={`/admin?page=${page - 1}`} className="inline-flex items-center gap-1 text-accent-ink"><ChevronLeft size={14} />{t("prev")}</Link> : <span />}
            <span className="text-muted tabular-nums">{page} / {pages}</span>
            {page < pages ? <Link href={`/admin?page=${page + 1}`} className="inline-flex items-center gap-1 text-accent-ink">{t("next")}<ChevronRight size={14} /></Link> : <span />}
          </div>
        )}
      </section>
    </div>
  );
}
