/** 学习模式的显示名（通用，不含题库词）。 */
export const MODE_LABEL: Record<string, string> = {
  unseen: "没做过的",
  wrong: "做错的",
  unsure: "不确定的",
  contested: "分歧题",
  all: "全部",
  tag: "专项",
};

export function timeAgo(iso?: string): string {
  if (!iso) return "";
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} 分钟前`;
  if (s < 86400) return `${Math.round(s / 3600)} 小时前`;
  if (s < 86400 * 7) return `${Math.round(s / 86400)} 天前`;
  return new Date(iso).toLocaleDateString("zh-CN", { month: "numeric", day: "numeric" });
}
