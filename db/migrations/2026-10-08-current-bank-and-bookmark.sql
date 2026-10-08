-- ═══════════════════════════════════════════════════════════════════
-- P9 第 2 步：「当前题库」与收藏
--
-- ## users.current_bank_id
--
-- P9 #1 裁定：选题库从首页挪进设置，「像切换账号一样」—— 首页 = 当前题库的仪表盘。
-- ⇒ 「当前是哪个题库」必须跟着账号走（换设备 / iOS 端看到的是同一个），
--    ⛔ 不放 cookie / localStorage。
--
-- NULL = 还没选过。⛔ 不给默认值：「默认哪个题库」是界面的决定，
-- 写进库里就会在新增题库时变成一条谁也不记得的隐含规则。
--
-- ⚠️ 这是用户的【偏好】，不是学习进度 —— 进度仍然只从 attempt 推出来，
--    「学习侧只存事实，不存状态」这条不被它打破。
--
-- ## bookmark
--
-- P9 第 5 步要用的收藏。一个用户对一道题只有「收藏了 / 没收藏」两态，
-- 所以主键就是 (user_id, question_id)，取消收藏 = DELETE。
-- ⛔ 不加 bank_id 列：经 question.bank_id 推得出，冗余一份就多一处会不一致的地方。
--
-- ⚠️ 显式钉 COLLATE：新表一律钉死（见 tracker 的 COLLATE 条目），
--    取 utf8mb4_general_ci 与 users 一致 —— 本表没有字符串列，钉它是为了不留例外。
-- ⚠️ TiDB 纪律：不建外键，关系约束在应用层。
-- ═══════════════════════════════════════════════════════════════════

ALTER TABLE users ADD COLUMN current_bank_id BIGINT NULL;

CREATE TABLE IF NOT EXISTS bookmark (
  user_id     BINARY(16) NOT NULL,
  question_id BIGINT     NOT NULL,
  created_at  DATETIME   NOT NULL,
  PRIMARY KEY (user_id, question_id),
  KEY idx_bookmark_time (user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
