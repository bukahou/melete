-- ═══════════════════════════════════════════════════════════════════
-- 题库权限：三档用户 + 公开 / 私有题库（设计见 docs/design/active/bank-access.md，P9 裁决 #27–#30）
--
-- ## bank.visibility
--   public  任何登录用户都能看
--   private 只有高级用户（content:private）与 admin（user:manage）能看 —— 对普通用户隐藏
--   ⭐ 缺省 private：新题库忘了标时，宁可普通用户看不到，⛔ 不能默认公开。
--   来源是题库配置 enrich_spec.json 的 "visibility"，由 load.py 写入；⛔ 应用里没有切换的地方。
--
-- ## user_permissions.granted_by
--   哪个 admin 做的升级（geass 的同名列；melete 当初按「仅 atlhyper 需要」没建）。
--   超级用户（= 直接操作数据库的人）写库时留空。
--
-- ⚠️ 迁移完成的那一刻所有题库都是 private —— 旧镜像不读这一列，线上不受影响；
--    新镜像上线前先导入 visibility（IPA → public），见设计文档 §7 的上线顺序。
-- ═══════════════════════════════════════════════════════════════════

ALTER TABLE bank ADD COLUMN visibility VARCHAR(16) NOT NULL DEFAULT 'private';
ALTER TABLE user_permissions ADD COLUMN granted_by BINARY(16) NULL;
