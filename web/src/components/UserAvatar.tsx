/**
 * 头像：第三方账号带来的地址；没有就用显示名的第一个字。管理页 · 「我的」· 侧栏共用（无 hook，服务端也能渲染）。
 * ⚠️ 用 <img> 而不是 next/image：地址来自上游、域名不固定，⛔ 不为它开图片代理的白名单。
 * referrerPolicy=no-referrer：⛔ 不把 Melete 的页面地址泄给头像的托管方。
 */
export function UserAvatar({ name, url, size = 36 }: { name: string; url?: string | null; size?: number }) {
  const style = { width: size, height: size };
  if (url) {
    // eslint-disable-next-line @next/next/no-img-element
    return <img src={url} alt="" referrerPolicy="no-referrer" style={style} className="shrink-0 rounded-full object-cover" />;
  }
  return (
    <span style={{ ...style, fontSize: size * 0.42 }}
          className="flex shrink-0 items-center justify-center rounded-full bg-accent-soft font-semibold text-accent-ink">
      {Array.from(name)[0] ?? "?"}
    </span>
  );
}
