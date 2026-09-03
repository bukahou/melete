-- 时间列统一为 UTC
--
-- 问题：开发环境 的 MySQL 服务端时区是 JST，`DEFAULT CURRENT_TIMESTAMP` 写的是
-- JST 墙上时间；而 Go 侧 time.Now() 经驱动（loc 缺省 UTC）写的是 UTC。
-- 于是 account_session 同一行里 created_at 是 JST、expires_at 是 UTC，
-- 且 `expires_at > NOW()` 拿 UTC 值比 JST 时钟 —— 会话实际早 9 小时过期。
-- 面向用户的表现：做过题却显示「连续 0 天」（连续天数按 JST 自然日聚合，
-- 而 created_at 已经是 JST 再被当成 UTC 转一次，落到了明天）。
--
-- 方案：存储一律 UTC，展示时区是应用层的事（study/sessions.go 的 studyTZ）。
-- 这样 TiDB 迁移也安全 —— TiDB Cloud 跑 UTC，不会再错第二次。
--
-- 本脚本只修 **MySQL 生成的** 列（减 9 小时）。
-- ⛔ 不要碰 account_session.expires_at —— 它由 Go 写入，本来就是 UTC，
--    再减一次会让所有会话提前 9 小时失效。
-- card.due / last_review 由 Go 写（P3 尚未启用，当前 0 行），同样不碰。
--
-- 前置：应用侧 DSN 已加 time_zone='+00:00'，此后 CURRENT_TIMESTAMP 与 NOW() 都是 UTC。
-- 幂等性：本脚本【不幂等】，只能跑一次。重复执行会再减 9 小时。

UPDATE bank            SET created_at = created_at - INTERVAL 9 HOUR;
UPDATE question        SET created_at = created_at - INTERVAL 9 HOUR, updated_at = updated_at - INTERVAL 9 HOUR;
UPDATE answer_claim    SET created_at = created_at - INTERVAL 9 HOUR;
UPDATE explanation     SET created_at = created_at - INTERVAL 9 HOUR, updated_at = updated_at - INTERVAL 9 HOUR;
UPDATE account         SET created_at = created_at - INTERVAL 9 HOUR;
UPDATE account_session SET created_at = created_at - INTERVAL 9 HOUR, last_active_at = last_active_at - INTERVAL 9 HOUR;
UPDATE attempt         SET created_at = created_at - INTERVAL 9 HOUR;
