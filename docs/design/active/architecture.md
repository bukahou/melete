# Melete 架构设计

> 状态：active · 最后更新 2026-09-01
> 本文是详细设计。**动机与核心判断见根目录 `CLAUDE.md`**，此处不重复。

## 1. 领域模型

### 1.1 概念关系

```
bank(题库) ─1:N─ question(题目) ─1:N─ choice(选项)
                      │
                      ├─1:N─ answer_claim(答案主张)   ★ 一题多个答案来源
                      │
                      └─N:M─ tag(标签)  ──N:M── article(知识条目)
                                 │                      ↑
                          type=concept 时跨题库共享   atlantis 迁入

account(用户) ─1:N─ card(FSRS 卡片) ─1:1─ question
       └────────1:N─ attempt(作答记录) ─N:1─ question
```

### 1.2 Schema（MySQL 8 / TiDB 双兼容）

> 四条纪律：**无外键** · **不假设 id 连续** · **批量分批提交** · **复杂 JSON 查询放应用层**

```sql
-- ============ 内容侧 ============

CREATE TABLE bank (
  id          BIGINT       NOT NULL AUTO_INCREMENT,
  slug        VARCHAR(64)  NOT NULL,               -- aws-saa-c03
  name        VARCHAR(255) NOT NULL,
  description TEXT,
  locale      VARCHAR(16)  NOT NULL DEFAULT 'zh',
  kind        VARCHAR(16)  NOT NULL,               -- cert | custom
  meta        JSON,                                -- 考纲域定义等题库特有元数据
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_bank_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE question (
  id          BIGINT       NOT NULL AUTO_INCREMENT,
  bank_id     BIGINT       NOT NULL,
  external_no INT          NOT NULL,               -- 原题号，用于溯源
  stem        TEXT         NOT NULL,
  kind        VARCHAR(16)  NOT NULL,               -- single | multi
  pick_count  TINYINT      NOT NULL DEFAULT 1,
  raw         JSON,                                -- 原始抽取结果 + warnings，可追溯
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_q_bank_no (bank_id, external_no),
  KEY idx_q_bank (bank_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE choice (
  id          BIGINT      NOT NULL AUTO_INCREMENT,
  question_id BIGINT      NOT NULL,
  label       CHAR(1)     NOT NULL,                -- A-F
  body        TEXT        NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_choice (question_id, label)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ★ 核心设计：答案是一组带来源的主张，不是单一字段
CREATE TABLE answer_claim (
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

CREATE TABLE tag (
  id      BIGINT       NOT NULL AUTO_INCREMENT,
  bank_id BIGINT       NULL,          -- NULL = 全局标签（concept 类）
  type    VARCHAR(16)  NOT NULL,      -- domain | service | concept
  value   VARCHAR(128) NOT NULL,      -- 'domain-3' | 'S3' | 'cache/cdn'
  i18n    JSON,                       -- {"zh":"高性能架构","ja":"高パフォーマンス設計"}
  PRIMARY KEY (id),
  UNIQUE KEY uk_tag (bank_id, type, value),
  KEY idx_tag_type (type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE question_tag (
  question_id BIGINT  NOT NULL,
  tag_id      BIGINT  NOT NULL,
  weight      TINYINT NOT NULL DEFAULT 1,   -- 主标签 2 / 次要 1
  PRIMARY KEY (question_id, tag_id),
  KEY idx_qt_tag (tag_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE article (               -- atlantis 迁入
  id          BIGINT       NOT NULL AUTO_INCREMENT,
  path        VARCHAR(255) NOT NULL,        -- infrastructure/linux/memory
  locale      VARCHAR(16)  NOT NULL,        -- zh | ja
  title       VARCHAR(255) NOT NULL,
  description TEXT,
  sections    JSON         NOT NULL,        -- 沿用 atlantis 的块结构
  PRIMARY KEY (id),
  UNIQUE KEY uk_article (path, locale)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE article_tag (
  article_id BIGINT NOT NULL,
  tag_id     BIGINT NOT NULL,
  PRIMARY KEY (article_id, tag_id),
  KEY idx_at_tag (tag_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============ 学习侧 ============

CREATE TABLE account (
  id         BIGINT       NOT NULL AUTO_INCREMENT,
  akasha_sub VARCHAR(128) NOT NULL,          -- Akasha OIDC 的 sub
  display    VARCHAR(128),
  created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_account_sub (akasha_sub)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE card (                  -- FSRS 状态，一个用户 × 一道题
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

CREATE TABLE attempt (
  id          BIGINT     NOT NULL AUTO_INCREMENT,
  account_id  BIGINT     NOT NULL,
  question_id BIGINT     NOT NULL,
  chosen      VARCHAR(8) NOT NULL,
  correct     TINYINT(1) NOT NULL,
  duration_ms INT,
  rating      TINYINT,                        -- FSRS 1-4
  created_at  DATETIME   NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_attempt_acc_q (account_id, question_id),
  KEY idx_attempt_time (account_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 1.3 关键查询

**「我哪里不会」** —— 不需要额外设计，标签聚合即是：

```sql
SELECT t.type, t.value,
       COUNT(*) AS total,
       SUM(a.correct) AS ok,
       ROUND(SUM(a.correct)/COUNT(*)*100, 1) AS rate
FROM attempt a
JOIN question_tag qt ON qt.question_id = a.question_id
JOIN tag t           ON t.id = qt.tag_id
WHERE a.account_id = ? AND t.type = 'service'
GROUP BY t.id
ORDER BY rate ASC;
```

**「错题 → 该看哪篇知识条目」** —— 经 `concept` 标签跳转：

```sql
SELECT DISTINCT ar.path, ar.title
FROM question_tag qt
JOIN tag t          ON t.id = qt.tag_id AND t.type = 'concept'
JOIN article_tag at ON at.tag_id = t.id
JOIN article ar     ON ar.id = at.article_id AND ar.locale = ?
WHERE qt.question_id = ?;
```

**「今天该复习什么」**：`SELECT ... FROM card WHERE account_id=? AND due <= NOW() ORDER BY due LIMIT ?`

## 2. 后端

- **Spec-first**：先写 `api/openapi/melete.yaml`，再 `oapi-codegen` 生成 server stub
- 分层：`internal/<domain>/{handler,service,repository}`，domain = bank / question / study / ai
- 跨模块**通过接口耦合**，不通过具体 struct
- 配置用 `caarlos0/env` struct tag；**凭证类无默认值 + required**

### AI 代理

后端提供 `/api/ai/ask`，前端不接触 API key。

- key 存 K8s Secret，经 env 注入
- 请求带上题目上下文（题干/选项/各方答案主张/已生成解析）
- 需要限流：单用户每日调用上限，防止误用烧钱

## 3. 前端

沿用 atlantis 的 Next.js 16 + React 19 + Tailwind 4。可直接复用：

- `src/components/` 的块渲染器（对应 `article.sections`）
- `src/i18n/` 中日双语机制
- `src/theme/` 主题

新增路由方向：

```
/                       今日复习 + 进度总览
/banks/[slug]           题库总览（按 domain / service 的正确率热图）
/banks/[slug]/drill     刷题（FSRS 队列 / 随机 / 按标签筛选 / 错题本）
/questions/[id]         单题详情（题干 + 三方答案主张 + AI 解析 + 追问入口）
/kb/[...path]           知识条目（atlantis 迁入）
/stats                  统计
```

**PWA**：刷题发生在通勤等碎片时间，需离线可用。至少缓存题目与解析。

## 4. 部署

复制 geass-v3-web 的模式：

```
集群 namespace: melete
  melete-api      Go 后端
  melete-web      Next.js SSR
入口: Cilium Gateway API HTTPRoute → 跨 ns parentRef ingress/public-ingress
DNS:  flarectl 建 CNAME → tunnel UUID（Proxied）
```

清单放 **config 私有仓** `clusters/集群甲/apps/melete/`，本仓库不含内网信息。

⚠️ hostname 待定 —— 需先查 config 仓 CLAUDE.md 的「在用 hostname 清单」避免撞名。

## 5. 被否决的方案（留档，免得重走）

| 方案 | 否决理由 |
|---|---|
| 把题库塞进 atlantis | atlantis 是 CF Pages 静态站，要为它加 SSR + 数据库 = 补丁式演进。**反过来才对：新建应用，atlantis 内容迁入** |
| 塞进 geass-v3 复用后端 | geass 是媒体平台，混入考试应用违背高内聚低耦合 |
| `correct_answer` 单一字段 | 340 道题存在答案分歧，单字段会把分歧掩盖掉 |
| 知识条目也进 FSRS 调度 | 知识条目适合按需查阅而非背诵；可作为后续扩展，不在 P3 范围 |
| 先补齐 25+ 篇 AWS 知识条目再关联 | 应由**做题结果反向驱动**内容生产：错得最多的知识点才值得写。避免生成一批用不上的内容 |
| 开发库 dump + import 到生产 | 中间产物 JSON 才是 source of truth，**内容重跑即可，无需迁移数据库** |
| Gnosis 作为项目名 | 撞名知名以太坊项目 |
