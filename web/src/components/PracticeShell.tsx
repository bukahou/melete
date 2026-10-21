import { Band } from "./Band";

/**
 * 四个出题入口页（P9 4.1–4.4）共用的外壳：面包屑回首页 + 标题 + 一句说明。
 * 入口页是阅读宽度的列表，⛔ 不用 Wide —— 一行一个选项，扫视比铺满屏更快。
 * 列表本身见 EntryList（#42）。
 */
export function PracticeShell({ home, title, sub, children }: {
  home: string; title: string; sub: string; children: React.ReactNode;
}) {
  return (
    <div className="mx-auto max-w-3xl pt-2">
      <Band crumbs={[{ href: "/", label: home }, { label: title }]} title={title} sub={sub} />
      <div className="grid gap-4">{children}</div>
    </div>
  );
}
