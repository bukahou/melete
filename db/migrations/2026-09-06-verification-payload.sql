-- verification_tokens 补一列：payload。
--
-- ⭐ 模块的 VerificationRecord.Payload 是「附带数据」：
--    change_email 时是【新地址】，recover 时是【码发往的地址】。
--    ⚠️ 它与 subject 不是一回事 —— subject 是归属（已有用户用 userID，
--    注册时用户还不存在用 email），payload 是这次操作的目标。
--    少了它，改邮箱流程存不下"要改成什么"。
--
-- ⚠️ 这是接入计划 §2.2 漏掉的【第二处同类缺口】（第一处是 user_sessions
--    的 prev_refresh_hash）。两次都是同一个成因：照着方法签名设计存储，
--    没有读【记录类型】的字段。⭐ 教训已经记过一次，这次是它的复发 ——
--    说明当时只记了结论没改做法。做法应该是：实现任何 Store 之前，
--    先把它读写的 record 类型逐字段过一遍，对照表结构列一张缺口表。
ALTER TABLE verification_tokens
  ADD COLUMN payload VARCHAR(255) NULL AFTER subject;
