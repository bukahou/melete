-- attempt.context：记录每次作答的出处（入口 + 过滤条件）
-- 「上次专项」= 最近一条 context.mode 不是 unseen/all 的 attempt。
-- 历史行为 NULL，等价于「出处未知」，统计时视同顺序刷。
ALTER TABLE attempt ADD COLUMN context JSON NULL AFTER rating;
