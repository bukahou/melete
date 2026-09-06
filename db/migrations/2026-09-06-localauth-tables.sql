-- ═══════════════════════════════════════════════════════════════════
-- localauth 接入 · 阶段 1：建表（⛔ 只建，不动旧表、不接线）
--
-- 依据：config/cross-exam/2026-09-06-melete-localauth-acceptance-plan.md §2.2
--       用户 2026-09-06 裁决八项（案卷 §37），⑦ = dev DB 可执行
--
-- ⚠️ 本文件【只新增】。旧的 account / account_session 原样保留 ——
--    计划 §9.1：新旧并存到阶段 6 才拆。理由是 melete 已上线、用户每天在用，
--    先删后建会让中间过程登录不可用，且丢掉「旧的怎么做的」这个参照物。
--
-- ⛔⛔ TiDB 禁令继续有效（用户 2026-09-05）：本文件只在 dev DB 执行。
-- ═══════════════════════════════════════════════════════════════════

-- ⛔ 所有表显式钉死 COLLATE。
--
-- ⚠️ 这不是风格问题：不写 COLLATE 就跟着服务器默认值走，
--    开发库 MySQL 8.0 是 utf8mb4_0900_ai_ci，而 TiDB 默认 utf8mb4_bin（逐字节比）。
--    同一份 schema 换个库，uk_username 的折叠规则就变了 ——
--    alice / Alice / ALICE 在 TiDB 上会是三个账号。
-- ⚠️ 本地怎么测都测不出来：本地两种规则都折叠大小写，
--    所以「我们试过没问题」证明不了生产没问题。
-- ⚠️ 而 TiDB 禁令期内我们【也验不了】—— 钉死是照 §22.2 的规范做，
--    ⛔ 不等于「已验证」。这条债在 tracker 里仍然开着。

-- ── users（§22.2 定稿，15 列，⛔ 无 role）──────────────────────────
CREATE TABLE IF NOT EXISTS users (
  id                   BINARY(16)    NOT NULL,   -- UUIDv7（时间有序，避免 v4 的页分裂）
  username             VARCHAR(64)   NOT NULL,   -- 唯一登录标识（OIDC 可能没邮箱，但一定有它）
  status               TINYINT       NOT NULL,   -- 1 active / 2 inactive / 3 banned
  created_at           DATETIME      NOT NULL,
  updated_at           DATETIME      NOT NULL,
  password_hash        VARCHAR(255)  NULL,       -- NULL = 纯 OIDC 账号
  password_changed_at  DATETIME      NULL,       -- ⭐ 吊销判定用它，不是逐条删 session
  email                VARCHAR(255)  NULL,       -- ⚠️ 只能由「向本应用完成邮箱验证」写入
  email_verified       TINYINT(1)    NOT NULL DEFAULT 0,
  upstream_email       VARCHAR(255)  NULL,       -- ⛔ 永不自动提升为 email，即使字面相同
  display_name         VARCHAR(128)  NULL,
  avatar_url           VARCHAR(500)  NULL,
  locale               VARCHAR(16)   NULL,
  last_login_at        DATETIME      NULL,
  last_login_ip        VARBINARY(16) NULL,       -- ⚠️ 必须是解析后的可信 IP
  deleted_at           DATETIME      NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- ── user_sessions ─────────────────────────────────────────────────
-- ⭐ refresh 存 BINARY(32) 原始摘要，不再 hex 化：模块的 SessionStore 收 []byte，
--    少一次编码 = 少一处两边不一致的可能。
CREATE TABLE IF NOT EXISTS user_sessions (
  id             BINARY(16)    NOT NULL,
  user_id        BINARY(16)    NOT NULL,
  refresh_hash   BINARY(32)    NOT NULL,
  device_info    VARCHAR(255)  NULL,
  client_ip      VARBINARY(16) NULL,   -- ⛔ 不得是 XFF 整条链（那是客户端可伪造的）
  created_at     DATETIME      NOT NULL,
  last_active_at DATETIME      NOT NULL,
  expires_at     DATETIME      NOT NULL,
  -- ⭐ 用 revoked_at 而不是 is_valid：时间点比布尔多一份审计信息，
  --    「什么时候被吊销的」在排查会话异常时是关键线索。
  revoked_at     DATETIME      NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_refresh (refresh_hash),
  KEY idx_user_live (user_id, revoked_at),
  KEY idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- ── user_permissions（§22.4 方案 B：无角色层）──────────────────────
-- ⚠️ melete 初始权限集为空，表照建（用户裁决 ④）：
--    DDL 是三家共享模板，建了将来加权限零改表；不建则验收覆盖不到 §22.4。
-- ⛔ 无 granted_by —— §22.4 ③ 定「仅 atlhyper 需要」。
CREATE TABLE IF NOT EXISTS user_permissions (
  user_id     BINARY(16)  NOT NULL,
  permission  VARCHAR(64) NOT NULL,   -- <资源>:<动作>，或单个 *
  granted_at  DATETIME    NOT NULL,
  expires_at  DATETIME    NULL,       -- 留列（VIP 天然有期限），逻辑可后做
  PRIMARY KEY (user_id, permission),
  KEY idx_permission (permission)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- ── identities（联邦身份，替代 account.akasha_sub）──────────────────
-- 用户裁决 ① = (b)：OIDC 的编排（找或建 → 发会话）一字不动，只换存储表。
-- ⭐ 形状照 geass-v3 的 LoginWithIdentity（work 实测确认，阶段 A 已被用户接受）。
-- ⛔ 查不到 identity 就直接建新账号，⛔ 不按邮箱找已有账号 ——
--    自动按邮箱认亲等于把安全责任委托给上游。
CREATE TABLE IF NOT EXISTS identities (
  provider   VARCHAR(32)  NOT NULL,   -- 'akasha'
  subject    VARCHAR(128) NOT NULL,   -- pairwise sub
  user_id    BINARY(16)   NOT NULL,
  created_at DATETIME     NOT NULL,
  PRIMARY KEY (provider, subject),
  KEY idx_identity_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- ── login_failures（Guard 的两个 FailureStore 共用一张表）──────────
-- ⭐ 模块要两个独立的 FailureStore 实例（IP 维度 / 账号维度），
--    但它不关心它们是不是同一张表 —— 隔离由 scope 列保证。
CREATE TABLE IF NOT EXISTS login_failures (
  scope    VARCHAR(8)   NOT NULL,   -- 'ip' | 'account'
  fail_key VARCHAR(255) NOT NULL,   -- 解析后的 IP 文本 或 username
  fails    INT          NOT NULL,
  first_at DATETIME     NOT NULL,
  last_at  DATETIME     NOT NULL,
  PRIMARY KEY (scope, fail_key),
  KEY idx_last (last_at)            -- 过期清理
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- ── verification_tokens（注册 / 找回 / 改邮箱共用）─────────────────
-- ⛔ 只存 verifier 的摘要，不存验证码明文。
CREATE TABLE IF NOT EXISTS verification_tokens (
  id            BINARY(16)   NOT NULL,
  purpose       VARCHAR(32)  NOT NULL,   -- register | recovery | email_change
  subject       VARCHAR(255) NOT NULL,   -- 邮箱地址 或 userID 文本
  verifier_hash BINARY(32)   NOT NULL,
  attempts      INT          NOT NULL DEFAULT 0,
  created_at    DATETIME     NOT NULL,
  expires_at    DATETIME     NOT NULL,
  consumed_at   DATETIME     NULL,       -- ⭐ 一次性消费
  PRIMARY KEY (id),
  KEY idx_pending (purpose, subject, consumed_at),
  KEY idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
