import { Page } from "./Page";

/**
 * 四个出题入口页（P9 4.1–4.4）共用的外壳：顶栏（返回首页 + 标题）+ 纸（#41），纸面左上是面包屑，
 * 内容上方一行小字说明。入口页是阅读宽度的列表 —— 一行一个选项，扫视比铺满屏更快。
 * 列表本身见 EntryList（#42）。
 */
export function PracticeShell({ home, title, sub, children }: {
  home: string; title: string; sub: string; children: React.ReactNode;
}) {
  return (
    <Page title={title} crumbs={[{ href: "/", label: home }, { label: title }]} sub={sub}>
      {children}
    </Page>
  );
}
