import { remark } from "remark";
import remarkGfm from "remark-gfm";
import remarkHtml from "remark-html";

/**
 * 渲染 AI 产出的解析正文。
 * 内容是本项目自己的管道生成的可信内容，不来自用户输入，
 * 所以这里用 dangerouslySetInnerHTML 是安全的。
 */
export async function Markdown({ children }: { children: string }) {
  const html = String(await remark().use(remarkGfm).use(remarkHtml).process(children));
  return <div className="prose-cn text-[0.92rem]" dangerouslySetInnerHTML={{ __html: html }} />;
}
