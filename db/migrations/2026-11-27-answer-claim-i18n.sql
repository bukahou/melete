-- 答案主张理由（answer_claim.rationale）的其他语言版本。与 db/schema.sql 里的定义一致；可重复执行。
--
-- 原文仍在 answer_claim.rationale，语言与 question.stem / choice.body 一样是 bank.locale；
-- 这里只放译文，按 (question_id, source) 对应 answer_claim 的唯一键。
-- 不建外键（TiDB 的外键不级联），完整性由导入器保证：主张被清理时一并删掉它的译文。

CREATE TABLE IF NOT EXISTS answer_claim_i18n (
  question_id BIGINT      NOT NULL,
  source      VARCHAR(32) NOT NULL,   -- 与 answer_claim.source 相同
  locale      VARCHAR(16) NOT NULL,
  rationale   TEXT        NOT NULL,
  created_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (question_id, source, locale)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
