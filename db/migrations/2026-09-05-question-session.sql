-- question 加 session 列：一个题库里可以有多套卷子
--
-- IPA IT パスポート 的 15 套公开问题各自从 問1 数起，而唯一键是
-- (bank_id, external_no) —— 一起导会直接撞。
--
-- ⚠️ 为什么 NOT NULL DEFAULT '' 而不是可空：
--   MySQL 的 UNIQUE 索引把多个 NULL 视为互不相同，可空列进唯一键
--   等于对现有两个题库放弃去重。空串是一个确定的值，参与比较。
--   （与本 schema 里 answer_claim 那处「用 0 不用 NULL」是同一条理由）
--
-- 现有 SAA / SAP 是单套题库，session 留空串，行为与改动前完全一致。
--
-- 顺序：先跑这条，再跑 load.py 重导 —— 反过来导入会因为列不存在而失败。

ALTER TABLE question
  ADD COLUMN session VARCHAR(16) NOT NULL DEFAULT '' AFTER external_no;

ALTER TABLE question
  DROP INDEX uk_q_bank_no,
  ADD UNIQUE KEY uk_q_bank_no (bank_id, session, external_no);
