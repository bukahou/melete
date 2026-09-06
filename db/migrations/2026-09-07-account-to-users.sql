-- ═══════════════════════════════════════════════════════════════════
-- localauth 阶段 2 · 数据迁移：account → users / identities
--
-- 依据：接入计划 §3，范围按 work 2026-09-06 批准的 (B)
--       —— 现有登录代码改读 users，⛔ 仍不引入 localauth.Guard（那是阶段 3）。
--
-- ⚠️ 为什么阶段 2 必须做这件事（原清单的顺序错误）：
--    学习表换成 BINARY(16) 之后，若登录仍从 account(BIGINT) 发 JWT sub，
--    sub 会是 "3" 这样的字符串 → encodeID 报「不是合法 UUID」→ /me/* 全 500。
--    ⇒ 「阶段 2 只改类型」与「端点要能打」不能同时成立。
--
-- ⛔ TiDB 未碰（用户 2026-09-05 禁令）。本文件只在 dev DB 执行。
-- ═══════════════════════════════════════════════════════════════════

-- ⭐ 3 个 id 是用 Go 的 uuid.NewV7() 现生成后写死的，⛔ 不用 MySQL 的 UUID()
--    —— 那个是 v1（时间戳字段顺序不利于索引），与本项目其余 id 不同源。
--    写死而不是运行时生成，是为了让这次迁移【可重放且结果相同】。
CREATE TEMPORARY TABLE _acct_map (old_id BIGINT PRIMARY KEY, new_id BINARY(16) NOT NULL);
INSERT INTO _acct_map (old_id, new_id) VALUES
  (1,  UNHEX(REPLACE('00000000-0000-7000-8000-000000000001','-',''))),
  (4,  UNHEX(REPLACE('00000000-0000-7000-8000-000000000002','-',''))),
  (12, UNHEX(REPLACE('00000000-0000-7000-8000-000000000003','-','')));

-- ── users ─────────────────────────────────────────────────────────
-- ⚠️ username 是 NOT NULL，而旧表里纯 OIDC 账号的 username 是 NULL。
--    §22 的立论是「OIDC 可能没邮箱，但 username 必须有」⇒ 得合成一个。
--    合成规则照 geass-v3 的 generateFederatedUsername 之意：有什么用什么，
--    都没有就用 provider 前缀 + 稳定后缀。
--    ⭐ 这里用 akasha_sub 的 SHA-256 前 8 位：确定性、必不相撞、
--      且⛔ 不泄漏 sub 本身（sub 是 pairwise 的，属于身份信息）。
--    ⚠️ 用户看不到它 —— OIDC 登录不输用户名，界面显示的是 display_name。
INSERT INTO users (id, username, status, created_at, updated_at,
                   password_hash, password_changed_at, display_name)
SELECT m.new_id,
       COALESCE(a.username, CONCAT('akasha_', LEFT(SHA2(a.akasha_sub, 256), 8))),
       1,                            -- StatusActive（⛔ 零值位是 Unknown，不可登录）
       a.created_at, UTC_TIMESTAMP(),
       a.password_hash,
       -- ⚠️ 有口令的账号必须有 password_changed_at：吊销判定用它，
       --    NULL 会让「改密前签发的 token 是否仍有效」无从判断。
       IF(a.password_hash IS NULL, NULL, a.created_at),
       a.display
FROM account a JOIN _acct_map m ON m.old_id = a.id;

-- ── identities（裁决 ①(b)：编排不动，只换存储表）──────────────────
INSERT INTO identities (provider, subject, user_id, created_at)
SELECT 'akasha', a.akasha_sub, m.new_id, a.created_at
FROM account a JOIN _acct_map m ON m.old_id = a.id
WHERE a.akasha_sub IS NOT NULL;

-- ── attempt ───────────────────────────────────────────────────────
-- ⭐ 保留数据，⛔ 不 TRUNCATE。
-- ⚠️ 这偏离了接入计划 §3.2（那里写「12 行直接清空，不写转换脚本」）。
--    偏离的理由：当时的判断是「为 12 行写迁移脚本是纯粹的成本」，
--    而实际做下来 id 映射就在同一个迁移里，代价是 4 行 JOIN。
--    ⇒ 前提变了，结论跟着变。用户的 FSRS 卡片与作答记录因此保住。
ALTER TABLE attempt ADD COLUMN user_id BINARY(16) NULL AFTER id;
UPDATE attempt t JOIN _acct_map m ON m.old_id = t.account_id SET t.user_id = m.new_id;
ALTER TABLE attempt
  MODIFY user_id BINARY(16) NOT NULL,
  DROP INDEX idx_attempt_acc_q,
  DROP INDEX idx_attempt_time,
  DROP COLUMN account_id,
  ADD KEY idx_attempt_user_q (user_id, question_id),
  ADD KEY idx_attempt_time (user_id, created_at);

-- ── card（⚠️ account_id 在主键里，要重建主键）──────────────────────
ALTER TABLE card ADD COLUMN user_id BINARY(16) NULL FIRST;
UPDATE card c JOIN _acct_map m ON m.old_id = c.account_id SET c.user_id = m.new_id;
ALTER TABLE card
  MODIFY user_id BINARY(16) NOT NULL,
  DROP PRIMARY KEY,
  DROP INDEX idx_card_due,
  DROP COLUMN account_id,
  ADD PRIMARY KEY (user_id, question_id),
  ADD KEY idx_card_due (user_id, due);

DROP TEMPORARY TABLE _acct_map;

-- ⛔ account / account_session 原样保留，本次不动 ——
--    但阶段 2 之后它们【不再有读者】，阶段 6 的 DROP 因此变成纯清理：
--    ⭐ 不可逆的那一步发生在「已经没人用它」之后，而不是「切换的同时」。
-- ⚠️ account_session 的 24 条会话就此作废（计划 §3.2 已定），
--    表现是所有人需要重新登录一次。
