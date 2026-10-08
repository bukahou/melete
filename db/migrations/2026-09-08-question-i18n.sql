-- ═══════════════════════════════════════════════════════════════════
-- 题目多语言：question_i18n / choice_i18n
--
-- ## 为什么需要它
--
-- 当前 question.stem 与 choice.body 是【单语言】的，而且语言是在**导入时烧死**的：
--   pipeline/core/load.py:226   stem = tr.get("stem") or q["stem"]
-- SAP-C02 的源是英文，导入时被替换成中文译文 —— 英文原文只存在于 config 仓的
-- enriched.json 里，库里已经找不到。⇒ 同一道题无法同时提供两种语言。
--
-- ## 形状照抄 explanation
--
-- explanation 早就是 (question_id, source, locale) 唯一键的多语言表，
-- 这两张表与它同构。⛔ 不用 JSON 列存译文：
--   · 「复杂 JSON 查询放应用层」是本项目的 TiDB 纪律之一
--   · 加一种语言不该需要改表结构
--
-- ## 与 question.stem 的关系
--
-- question.stem 仍是【源语言】正文，i18n 表只放译文。
-- 取某个 locale 时：先查 i18n，没有就回退 stem，并把「这是回退」告诉界面
-- （⛔ 不静默回退 —— 与「答案主张并列展示」同一条哲学：把状况摆出来）。
--
-- ⚠️ TiDB 纪律：不建外键（FK 晚且实验性，会静默不级联），关系约束在应用层。
-- ═══════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS question_i18n (
  question_id BIGINT      NOT NULL,
  locale      VARCHAR(16) NOT NULL,
  stem        TEXT        NOT NULL,
  -- source 记录译文从哪来，与 explanation.source 同一套取值（ai | editor | user）。
  -- ⭐ 留着它是为了将来「AI 译文被人工订正过」时能区分开，而不是事后加列。
  source      VARCHAR(32) NOT NULL DEFAULT 'ai',
  created_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (question_id, locale)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS choice_i18n (
  choice_id  BIGINT      NOT NULL,
  locale     VARCHAR(16) NOT NULL,
  body       TEXT        NOT NULL,
  source     VARCHAR(32) NOT NULL DEFAULT 'ai',
  created_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (choice_id, locale),
  -- 按题取整套选项译文时用（choice 表本身按 question_id 查，这里补一条按 locale 的入口）
  KEY idx_choice_i18n_locale (locale)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
