import { remark } from "remark";
import remarkCjkFriendly from "remark-cjk-friendly/parseOnly";
import remarkGfm from "remark-gfm";
import remarkHtml from "remark-html";

/**
 * 渲染 AI 产出的解析正文。
 * 内容是本项目自己的管道生成的可信内容，不来自用户输入，
 * 所以这里用 dangerouslySetInnerHTML 是安全的。
 */
export async function Markdown({ children }: { children: string }) {
  // CJK 友好的强调规则：中日文标点紧挨 ** 时，CommonMark 原规则不认作强调，会把星号原样显示出来。
  // 删除线只认 ~~：中日文常用单个 ~ 表示范围（3~5 小时），两处之间会被误划成删除线
  const html = String(
    await remark().use(remarkGfm, { singleTilde: false }).use(remarkCjkFriendly).use(remarkHtml).process(children),
  );
  return <div className="prose-cn text-[0.92rem]" dangerouslySetInnerHTML={{ __html: html }} />;
}
