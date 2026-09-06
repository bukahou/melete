-- Melete Schema (MySQL 8 / TiDB 双兼容)
--
-- 四条纪律：无外键 · 不假设 id 连续 · 批量分批提交 · 复杂 JSON 查询放应用层
-- 理由见 CLAUDE.md「MySQL ↔ TiDB 四条纪律」。
--
-- 本文件是 pipeline（load.py）与 backend（Go）的**共同契约**，
-- 所以不放在其中任何一方目录下。

-- ============ 内容侧 ============

CREATE TABLE IF NOT EXISTS bank (
  id          BIGINT       NOT NULL AUTO_INCREMENT,
  slug        VARCHAR(64)  NOT NULL,
  name        VARCHAR(255) NOT NULL,
  description TEXT,
  locale      VARCHAR(16)  NOT NULL DEFAULT 'zh',
  kind        VARCHAR(16)  NOT NULL,               -- cert | custom
  meta        JSON,                                -- 考纲域定义等题库特有元数据
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_bank_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS question (
  id          BIGINT       NOT NULL AUTO_INCREMENT,
  bank_id     BIGINT       NOT NULL,
  external_no INT          NOT NULL,               -- 原题号，用于溯源
  -- session 是「哪一套卷子」。IPA IT パスポート 一个题库有 15 套公开问题，
  -- 各自从 問1 数起 —— 没有它，15 套一起导会撞唯一键。
  -- ⚠️ NOT NULL DEFAULT ''：MySQL 的 UNIQUE 把多个 NULL 视为互不相同，
  -- 可空列进唯一键等于放弃去重。单套题库（SAA/SAP）留空串。
  session     VARCHAR(16)  NOT NULL DEFAULT '',
  stem        TEXT         NOT NULL,
  kind        VARCHAR(16)  NOT NULL,               -- single | multi
  pick_count  TINYINT      NOT NULL DEFAULT 1,
  data_issue  TEXT,                                -- 题目本身有缺陷时的说明，正常为 NULL
  raw         JSON,                                -- 原始抽取结果 + warnings，可追溯
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_q_bank_no (bank_id, session, external_no),
  KEY idx_q_bank (bank_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS choice (
  id          BIGINT      NOT NULL AUTO_INCREMENT,
  question_id BIGINT      NOT NULL,
  label       CHAR(1)     NOT NULL,                -- A-F
  body        TEXT        NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_choice (question_id, label)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ★ 核心设计：答案是一组带来源的主张，不是单一字段。
--   884 道有对照数据的题里 38% 存在题库标注与社区投票不一致，
--   单字段会把这个分歧掩盖掉 —— 而分歧本身是最好的学习材料。
CREATE TABLE IF NOT EXISTS answer_claim (
  id          BIGINT      NOT NULL AUTO_INCREMENT,
  question_id BIGINT      NOT NULL,
  source      VARCHAR(32) NOT NULL,   -- bank_label | community_vote | ai_verdict | user_note
  answer      VARCHAR(8)  NOT NULL,   -- 排序后的字母集合，如 "AB"
  confidence  SMALLINT,               -- 0-100，无则 NULL
  rationale   TEXT,                   -- 为什么；AI 裁决必填
  meta        JSON,                   -- 如投票分布 {"A":80,"C":20}
  created_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_claim (question_id, source),
  KEY idx_claim_q (question_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 完整解析。与 answer_claim 同构（多来源 + 多语言）：
--   · rationale 是「为什么选它」的论证，绑定到某一条答案主张
--   · explanation 是「面向学习者的完整讲解」，绑定到题目本身
-- 两者职责不同，且解析同样会有多个来源（AI / 用户笔记 / 将来的官方解析）。
CREATE TABLE IF NOT EXISTS explanation (
  id          BIGINT      NOT NULL AUTO_INCREMENT,
  question_id BIGINT      NOT NULL,
  source      VARCHAR(32) NOT NULL,   -- ai | editor | user
  locale      VARCHAR(16) NOT NULL DEFAULT 'zh',
  body        MEDIUMTEXT  NOT NULL,
  created_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_expl (question_id, source, locale),
  KEY idx_expl_q (question_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 通用 (type, value) 结构，不硬编码 AWS。
-- bank_id = 0 表示全局标签（concept 类，跨题库共享并链到知识条目）
--
-- ⚠️ 为什么用 0 而不是 NULL：MySQL 的 UNIQUE 索引把多个 NULL 视为互不相同，
--    若 bank_id 可空，uk_tag 对全局标签就完全不生效 —— 重跑导入会插出重复的
--    concept 标签，且并发下无法靠 SELECT-then-INSERT 兜住竞态。
--    用哨兵值 0 让唯一约束真正生效，MySQL 与 TiDB 行为也一致。
CREATE TABLE IF NOT EXISTS tag (
  id      BIGINT       NOT NULL AUTO_INCREMENT,
  bank_id BIGINT       NOT NULL DEFAULT 0,
  type    VARCHAR(16)  NOT NULL,      -- domain | topic | concept（角色；显示名在 bank.meta.tagTypes）
  value   VARCHAR(128) NOT NULL,      -- 'domain-3' | 'S3' | 'cache-cdn'
  i18n    JSON,                       -- {"zh":"高性能架构","ja":"高パフォーマンス設計"}
  PRIMARY KEY (id),
  UNIQUE KEY uk_tag (bank_id, type, value),
  KEY idx_tag_type (type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS question_tag (
  question_id BIGINT  NOT NULL,
  tag_id      BIGINT  NOT NULL,
  weight      TINYINT NOT NULL DEFAULT 1,   -- 主标签 2 / 次要 1
  PRIMARY KEY (question_id, tag_id),
  KEY idx_qt_tag (tag_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS article (               -- atlantis 迁入（P5）
  id          BIGINT       NOT NULL AUTO_INCREMENT,
  path        VARCHAR(255) NOT NULL,
  locale      VARCHAR(16)  NOT NULL,
  title       VARCHAR(255) NOT NULL,
  description TEXT,
  sections    JSON         NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_article (path, locale)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS article_tag (
  article_id BIGINT NOT NULL,
  tag_id     BIGINT NOT NULL,
  PRIMARY KEY (article_id, tag_id),
  KEY idx_at_tag (tag_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============ 学习侧 ============

-- 账号：本地密码与 Akasha 联邦两种登录方式归到同一行。
-- Akasha 按其 2026-08-09 定案不做密码认证，密码账号体系归各接入应用自持 ——
-- 与 geass-v3 同模式。akasha_sub / username 均可空（至少有其一），
-- 此处的 NULL 语义与 tag.bank_id 不同：多个 NULL 合法（未绑定该方式），
-- 唯一约束只需管住非空值，MySQL 行为恰好如此。
CREATE TABLE IF NOT EXISTS account (
  id            BIGINT       NOT NULL AUTO_INCREMENT,
  akasha_sub    VARCHAR(128) NULL,           -- Akasha OIDC 的 sub；未绑定为 NULL
  username      VARCHAR(64)  NULL,           -- 本地密码登录名；未启用为 NULL
  password_hash VARCHAR(255) NULL,           -- bcrypt；无密码账号为 NULL
  display       VARCHAR(128),
  created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_account_sub (akasha_sub),
  UNIQUE KEY uk_account_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 会话（refresh token 的载体）。
--
-- 对齐 geass-v3 的 user_sessions（见其 internal/auth/DECISIONS.md）：
-- **refresh 不做成 JWT，而是落库为一行**。理由是 JWT 一旦签出就无法撤回，
-- 而 refresh 的生命周期以月计 —— 必须能吊销（登出、换设备、密码泄漏）。
-- 吊销 = 把 is_valid 置 0，不需要黑名单机制。
--
-- access token 反过来是纯 JWT、不落库：TTL 只有 1 小时，撤回的代价
-- 由「最多再活 1 小时」兜住，换来验签不查库。
CREATE TABLE IF NOT EXISTS account_session (
  id            BIGINT       NOT NULL AUTO_INCREMENT,
  account_id    BIGINT       NOT NULL,
  -- 只存 refresh token 的 SHA-256，不存原文：库被读走也无法直接冒用
  token_hash    CHAR(64)     NOT NULL,
  device_info   VARCHAR(255),                -- User-Agent 摘要，用于「我的登录设备」
  expires_at    DATETIME     NOT NULL,
  is_valid      TINYINT(1)   NOT NULL DEFAULT 1,
  created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_active_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_session_token (token_hash),
  KEY idx_session_account (account_id, is_valid),
  KEY idx_session_expires (expires_at)        -- 过期清理用
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS card (                  -- FSRS 状态，一个用户 × 一道题
  account_id  BIGINT    NOT NULL,
  question_id BIGINT    NOT NULL,
  state       TINYINT   NOT NULL DEFAULT 0,  -- 0 new / 1 learning / 2 review / 3 relearning
  due         DATETIME  NOT NULL,
  stability   DOUBLE    NOT NULL DEFAULT 0,
  difficulty  DOUBLE    NOT NULL DEFAULT 0,
  reps        INT       NOT NULL DEFAULT 0,
  lapses      INT       NOT NULL DEFAULT 0,
  last_review DATETIME,
  PRIMARY KEY (account_id, question_id),
  KEY idx_card_due (account_id, due)          -- 「今天该复习什么」的主查询
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS attempt (
  id          BIGINT     NOT NULL AUTO_INCREMENT,
  account_id  BIGINT     NOT NULL,
  question_id BIGINT     NOT NULL,
  chosen      VARCHAR(8) NOT NULL,
  correct     TINYINT(1) NOT NULL,
  duration_ms INT,
  rating      TINYINT,                        -- FSRS 1-4
  -- 这次作答的出处：{"mode":"tag","tagId":44} / {"mode":"wrong"} / {"mode":"unseen"}。
  -- 这是 attempt 上唯一「从事实推不出来」的信息：做了 27 道带 EC2 标签的题，
  -- 看不出是「EC2 专项」里做的还是顺序刷时撞上的。其余一切状态（错题 / 不确定 /
  -- 没做过 / 顺序断点 / 专项进度）都是对 attempt 的查询，不落库。
  context     JSON,
  created_at  DATETIME   NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_attempt_acc_q (account_id, question_id),
  KEY idx_attempt_time (account_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ═══════════════════════════════════════════════════════════════════
-- localauth 认证域（2026-09-06 起）
--
-- ⚠️ 这一段与上方的 account / account_session 【并存】是过渡态：
--    localauth 接入的阶段 6 才 DROP 旧表（见接入计划 §9.1 —— melete 已上线，
--    先删后建会让中间过程登录不可用，且丢掉「旧的怎么做的」这个参照物）。
--
-- ⚠️ 本段显式钉死 COLLATE，而上方的旧表【没有】—— 这个不一致是有意留下的：
--    给存量表改排序规则是一次影响数据的 DDL，不属于建表这一步。
--    ⛔ 那条债仍开着，见 docs/tasks/active/tracker.md 的 COLLATE 条目。
-- ═══════════════════════════════════════════════════════════════════
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
