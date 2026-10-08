-- ═══════════════════════════════════════════════════════════════════
-- P9 第 6 步：用语集（裁决 #5，2026-10-08 用户裁定「术语从题目里抽」）
--
-- ## term
--
-- 一个题库一套术语（bank_id 必填）：AWS 的「Reserved Instances」与将来别的题库的同名词
-- 释义口径未必相同，⛔ 不做跨题库共享（那是 concept 标签的职责）。
--
--   slug        正式名称（AWS = 官方英文名；IPA = 日文标准用语）—— 题库内唯一，也是生成管道的归并键
--   names       {"zh": "...", "ja": "..."}（IPA 只有 ja —— 产品方针：IPA 不出中文）
--   reading     读音（IPA 的平假名；AWS 为空）
--   definition  {"zh": "...", "ja": "..."} 一两句的通用释义
--   category    分组 = 题库自己的知识对象轴（AWS 的服务 / IPA 的中分類），用语集目录按它分组
--   search_text 小写拼接的 slug + names + reading —— 检索用 LIKE，⛔ 不在 SQL 里拆 JSON（TiDB 纪律之四）
--
-- ## term_question
--
-- 「这个词在哪些题考过」（it-pass 的出題歴）。由生成管道的第 1 遍（逐题抽词）直接给出，
-- ⛔ 不靠标签反推 —— 标签只到服务 / 中分類这一层，比术语粗。
--
-- ⚠️ 显式钉 COLLATE（新表一律钉）；⚠️ TiDB 纪律：不建外键。
-- ═══════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS term (
  id          BIGINT        NOT NULL AUTO_INCREMENT,
  bank_id     BIGINT        NOT NULL,
  slug        VARCHAR(191)  NOT NULL,
  names       JSON          NOT NULL,
  reading     VARCHAR(191)  NULL,
  definition  JSON          NOT NULL,
  category    VARCHAR(128)  NOT NULL DEFAULT '',
  search_text VARCHAR(1024) NOT NULL DEFAULT '',
  created_at  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_term_bank_slug (bank_id, slug),
  KEY idx_term_bank_category (bank_id, category)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS term_question (
  term_id     BIGINT NOT NULL,
  question_id BIGINT NOT NULL,
  PRIMARY KEY (term_id, question_id),
  KEY idx_tq_question (question_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
