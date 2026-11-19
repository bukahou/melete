-- 逐个选项的点评表。与 db/schema.sql 里的定义一致；可重复执行。
--
-- 对错不在这里存：由 answer_claim 的裁决决定，这里只放原因。
-- 不建外键（TiDB 的外键不级联），choice_id 的完整性由导入器保证。

CREATE TABLE IF NOT EXISTS choice_explanation (
  choice_id  BIGINT      NOT NULL,
  source     VARCHAR(32) NOT NULL,   -- ai | editor | user
  locale     VARCHAR(16) NOT NULL,
  body       TEXT        NOT NULL,
  created_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (choice_id, source, locale)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
