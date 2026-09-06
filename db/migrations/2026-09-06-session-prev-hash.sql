-- user_sessions 补一列：上一个 refresh 的摘要。
--
-- ⭐ 为什么必须有它：模块的 RotateOutcome 要区分三种情况，而其中两种
--    在只存「当前 hash」的表上【长得一模一样】：
--
--      Replayed  命中【上一个】hash —— 这个 token 已被换走，现在又有人拿它来换
--      Unknown   查不出来历（从来不存在，或行已清理）
--
--    ⚠️ 分不清的后果不是"少一个枚举值"：Replayed 的处置是【吊销该用户全部会话】
--    （模块 session_guard.go 明写没有开关），Unknown 只是拒绝这一次。
--    把 Replayed 误判成 Unknown = 重放检测形同虚设；反过来 = 无故全员登出。
--
-- ⚠️ 这一列是接入计划 §2.2 的 DDL 漏掉的 —— 写计划时只照着「存 refresh 摘要」
--    的直觉设计，没有回头看 RotateOutcome 需要什么。⭐ 教训：
--    实现接口之前先读它的【返回类型】，返回类型决定存储必须记住什么。
ALTER TABLE user_sessions
  ADD COLUMN prev_refresh_hash BINARY(32) NULL AFTER refresh_hash;

-- 查找用。⛔ 刻意不做 UNIQUE：唯一性不需要（32 字节随机摘要），
-- 而 UNIQUE 会在多行 NULL 上引入一条不必要的约束语义。
ALTER TABLE user_sessions
  ADD KEY idx_prev_refresh (prev_refresh_hash);
