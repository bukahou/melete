# Melete 任务进度

> 最后更新 2026-09-01

## 分期总览

| 阶段 | 内容 | 状态 |
|---|---|---|
| P0 | PDF → 结构化 JSON | ✅ 完成 |
| P1 | AI 富化：裁决 + 解析 + 三维标签 | ✅ **完成 1019/1019** |
| P2 | 最小可用：刷题 / 看解析 / 基础统计 | ✅ 完成（含认证、学习记录、个人分析） |
| P3 | FSRS 调度 | ⬜ 待办 |
| P4 | AI 实时问答 | ⬜ 待办 |
| P5 | atlantis 内容迁入 + concept 关联 | ⬜ 待办 |
| P6 | 第二个题库，验证扩展性 | ⬜ 待办 |

**P2 就是可用产品**，P3 之后才是「聪明」。不要在 P2 之前追求完整。

---

## P0 · PDF → 结构化 JSON ✅

- [x] 确认 PDF 有文本层（509 页 A3，每页一张整页水印图，非内容）
- [x] 摸清源文件形态（页眉/续行/答案行/投票分布/分页符）
- [x] 写解析器 `pipeline/banks/aws-saa-c03/parse.py`
- [x] 修 `re.M` 缺失导致 bank_label 只抽到 104 条的 bug
- [x] 西里尔字母混入拉丁选项字母的归一
- [x] 质量报告 + warnings 随题带出
- [x] 产出 `data/aws-saa-c03/questions.json`（1011 道，97.9% 零告警）
- [x] 素材收件区约定 + `pipeline/core/ingest.py` 编排器（素材不进仓库，只留登记单）
- [x] 产出 `data/aws-saa-c03/source.yaml` 登记单（sha256 + 解析器版本 + 产出统计）

## P1 · AI 富化 ✅ 完成（2026-09-02）

```
1019/1019 题 · check 全绿 · 正文合计 283 万字
confidence  high 941 · medium 72 · low 6
data_issue  6（结构性不可判）· notes 57（advisory）
标签        services 135 个 · concepts 73 个（51 种子词全用上 + 新增 22）
```

**核心产出 —— 裁决与两个来源的关系：**

```
裁决 ≠ 题库标注   362 道（35.5%）  ← 「照搬标注会背错三分之一」有了精确数字
裁决 ≠ 社区投票    18 道（1.8%）
题库 ≠ 社区       346 道
无社区投票        121 道（AI 是唯一的第二意见）
```

**362 vs 18 这组对比是最有价值的发现**：社区投票的可靠性远高于题库标注，
但社区也有 18 道判错 —— 三个来源都不能无条件信任，这正是 `answer_claim`
多来源设计的实证依据。

两份沉淀已固化为文档：
- `docs/design/active/distractor-patterns.md` —— **错误选项构造手法 16 类**
- `data-pipeline.md` 的**换题库检查清单 8 项** + 时效性变更表 9 条

## P1 · 旧待办（已全部完成）

### 已定决策（2026-09-01）

| 决策 | 结论 | 理由 |
|---|---|---|
| 模型策略 | **分层**：低档 Sonnet 5 `effort=medium` / 高档 Opus 5 `effort=high` | 钱花在真正需要推理的题上。分层判据在 `questions.json` 里已可判定，无额外成本 |
| 高档口径 | 分歧 ∪ 无社区投票 ∪ 低共识 ∪ 有告警 = **481 道**（低档 530） | 119 道无投票题只有 `bank_label` 一个来源，**AI 是唯一的第二意见**；若下放低档等于照搬题库标注，正是本项目要避免的事 |
| concept 词表 | **AI 自由产出**（给种子词表引导收敛），P5 再与 atlantis 对齐 | concept 按设计是全局共享的，atlantis 只是消费方之一，不该反过来当约束源。代价：需一轮人工归并 |

⚠️ **thinking token 计入输出计费，是成本主导项**（不是模型单价）。全量成本以校准批实测 token 为准，不用估算值拍板。

- [ ] **前置：Anthropic API key**（Batches 走按量付费，Max 订阅配额用不了）
      → `config/local/ubuntu/env/melete.env` 的 `MELETE_ANTHROPIC_API_KEY`，不进本仓库
- [ ] 跑 40 道校准批（<$1），人工验收输出质量
- [ ] 拿校准批实测 token 重报全量成本 → 用户确认预算上限 → 全量开跑
- [ ] 写提示词：一次调用产出 verdict + reasoning + explanation + 三维标签
- [ ] 从 atlantis 抽出 concept 路径白名单（防止 AI 编造路径）
- [ ] 走 Claude Batches API 跑 1011 道
- [ ] 质量门禁：verdict 合法率 100% / concepts 路径存在率 100%
- [ ] 47 道低共识题人工抽查
- [ ] 21 道解析告警题单独处理
- [ ] 产出 `data/aws-saa-c03/enriched.json`

## P2 · 最小可用 ✅

完整闭环已跑通：登录 → 刷题 → 三方主张对峙 + AI 解析 → 四键自评 →
错题本/不清楚/没做过 → 个人进度与弱项分析。剩余项都属于**部署**（P2.5），不是功能缺口。

- [x] `backend/api/openapi/melete.yaml` Spec-first 契约（5 端点 / 11 schema）
      · 用 **OpenAPI 3.0.3 而非 3.1**：oapi-codegen 尚不支持 3.1；实测两版生成代码完全一致，
        降级零损失且消除警告。将来工具链支持了再升
      · 后端 `oapi-codegen` 生成 server stub，前端 `openapi-typescript` 生成 TS 类型
        —— **同一份契约驱动前后端**
- [x] `db/schema.sql` 12 张表，已建在 开发机 `melete` 库
      · 新增 `explanation` 表（与 `answer_claim` 同构，多来源 + 多语言）——
        原 DDL 里完整解析没有落脚点
      · `tag.bank_id` 由 `NULL` 改为 `NOT NULL DEFAULT 0`：MySQL 唯一索引视多个 NULL 为
        互不相同，全局 concept 标签的 `uk_tag` 会完全失效
- [x] `pipeline/core/load.py` 幂等导入器（已验证重跑不产生重复）
      · 硬错误拒绝 / 软错误跳过并告警 —— 题库自身脏点不该阻塞其余 1000 道好题
- [x] Go 后端：chi + sqlx，bank / question 两个领域，`go vet` 通过
      · **改用 sqlx 而非 architecture.md 定的 GORM** —— 写入全在 Python pipeline，
        Go 侧几乎纯只读且是多表 join + 聚合，GORM 的关系映射/AutoMigrate/钩子一个用不上
- [x] Next.js 16 前端：首页 / 题库页 / 刷题页 / 单题详情，`next build` 通过
      · `ClaimsPanel` 并列展示各方主张，分歧时高亮 —— 项目核心 UI
      · `resolveAnswer()` 显式定义判对错的优先级：AI 裁决 > 社区投票 > 题库标注
- [x] Akasha OIDC 接入 + **全站登录墙**（版权边界，用户拍板）
      · melete client 已注册进生产 Akasha（confidential + PKCE），secret 在 config 仓 melete.env
      · web 手写标准 code flow（jose 一个依赖），会话为 7 天 HS256 JWT cookie
- [x] **本地密码登录**（Akasha 按定案不做密码，密码归应用自持，geass-v3 同模式）
      · account 表扩展 username/password_hash；双登录方式归同一行，session 以 accountId 为主键
      · 测试账号的用户名与口令【不写在这里】——凭证在 config 私有仓 local/ubuntu/env/
- [x] `attempt` 写入 + **FSRS 四键自评**（Again/Hard/Good/Easy → rating 1-4）
      · correct 由后端按参考答案判定（ai_verdict > community_vote > bank_label），
        判定权威单一化，前端本地判定逻辑已删除
      · 浏览器走 BFF 转发口 /api/attempts，账号 id 从会话取出加 X-Melete-Account 头，
        前端永远不自报身份；内网 API 信任该头（不对公网暴露）
- [x] drill 三个个人化模式：**wrong（最近一次答错）/ unsure（自评≤2）/ unseen（没做过）**
      · 「最近一次」语义：后来做对的题自动移出错题本
- [x] 定 hostname：**`melete.bukahou.com`**（已查在用清单无撞名）
- [ ] **拿 TiDB 跑一次完整导入验证**（不要等上线才发现差异）
- [x] 登录后首页 dashboard + `/me` 分析页
      · 后端三端点：`/me/progress`（进度）`/me/tag-stats`（弱项）`/me/resume`（断点）
      · **每题只计最近一次作答** —— 否则重做错题会让正确率越刷越低，与「我在进步」相反
      · 断点从 attempt 推导，不建断点表（少一处要维护一致性的状态）
      · 正确率条按语义配色（<50 薄弱 / 50-79 需巩固 / ≥80 已掌握）并附阈值图例
      · 首页登录后显示「继续学习 + 弱项 Top 3」，与宣言段并存而非替换

## P2.5 · 部署上线 🔄

设计见 `docs/design/active/deployment.md`（含三处与 geass-v3 的关键差异 + 被否决方案）。
**刻意把 CI 放最后** —— 先手动验证「镜像能跑、清单对、域名通」，再自动化。

- [x] 定 hostname / 镜像架构（amd64+arm64）/ CD 策略（起步 manual）
- [x] **认证改造：api 成为 token 唯一签发方**（iOS 约束触发，设计见 deployment.md §0）
      · access 1h HS256 + refresh 落 account_session 表（对齐 v3 的 user_sessions）
      · /auth/sso 改收 idToken 由 api 自验 Akasha JWKS（**修当前的越权漏洞**）
      · 去掉 X-Melete-Service-Token（iOS 无法安全持有）+ 登录端点速率限制
      · web 端 token 存 httpOnly cookie（SSR 登录墙要用），iOS 存 Keychain
      · hostname 增加 melete-api.bukahou.com
- [ ] 拿 TiDB 建表 + 导一次内容（同时清掉「TiDB 兼容性验证」这个老待办）
- [x] 两个 Dockerfile + 本地构建与容器联调验证
      · api 11.2MB（distroless/static:nonroot）· web 237MB（standalone + 非 root）
      · 后端用 `--platform=$BUILDPLATFORM` 交叉编译，避开 QEMU 跑 go build（慢 5-10 倍）
      · web 镜像**环境无关**（无 NEXT_PUBLIC_*），改 API 地址只需改 ConfigMap
      · 已验证 amd64 + arm64 双架构可构建
- [x] config 仓部署清单 `clusters/集群甲/apps/melete/`（7 文件）+ ArgoCD Application
      · 9 个资源全部通过服务端 dry-run schema 校验，未 apply
      · DSN 指向 开发机 MySQL（验收合格后才迁 TiDB，流程写在 config.yaml 注释）
- [ ] Akasha client 追加生产回调地址（保留 localhost）
- [x] CF 建两条 DNS CNAME → tunnel UUID（Proxied）：`melete` 与 `melete-api`
      · 已生效，两域名返回 404 = 链路已通到集群 Gateway，只差 HTTPRoute 接住
      · 已更新 config 仓 CLAUDE.md 的「在用 hostname 清单」
      · wildcard 已能兜底，但按 config 仓规矩业务上线必须显式建
      · cloudflared 无需改动（远程管理模式，分流由 HTTPRoute 决定）
      · 本机无 flarectl，可直接调 CF API（token 在 config 仓 infra.env）
- [ ] 后端对 `/me/*` 加 `Cache-Control: no-store`（不依赖 CF 默认行为，自己声明意图）
- [ ] 手动 apply 一次，确认可访问
- [ ] 最后接 GitHub Actions（path filter / buildx / 回写 config 仓）

## P3 · FSRS ⬜

- [ ] 选 FSRS 实现（Go 侧库或自实现）
- [ ] `card` 表状态机 + 「今天该复习什么」查询
- [ ] 作答 → rating 映射规则（对/错 + 用时 → 1-4）
- [ ] 复习队列页

## P4 · AI 实时问答 ⬜

- [ ] 后端 `/api/ai/ask` 代理（key 不出后端）
- [ ] 单用户日调用上限（防误用烧钱）
- [ ] 前端追问入口

## P5 · atlantis 迁入 ⬜

- [ ] 复制 114 篇 × 2 语言内容（**原仓库不动**）
- [ ] `article` / `article_tag` 导入
- [ ] concept 标签双向关联
- [ ] 错题 → 知识条目跳转

## P6 · 第二个题库 🔄

- [x] 选题库：**AWS SAP-C02**（英文原版，含完整社区讨论）
      · 弃用中英对照版：`AWS Config` 被打成 `Con g` 四百余处（pdf-lib 二次加工丢连字）
      · 英文原版还白捡了每条投票背后的论证 —— SAA 只有百分比
- [x] **验证 core 无需改动 ✓** 只写了 `banks/aws-sap-c02/parse.py`（231 行，从零写）
      ingest / enrich / load 全部零改动直接支持
- [x] P0 完成：529 题、题号零缺失、零告警 98.1%、投票覆盖 99.1%
      **答案分歧率 54.6%**（SAA 是 38%）—— Professional 级标注更不可靠
- [ ] core/enrich.py 加**翻译字段**支持（英文题库需要中文翻译，做成可选字段）
- [ ] 开第二个富化会话

---

## 已定决策：素材里的社区讨论正文**不抽取**（2026-09-02，用户拍板）

PDF 里有完整的 源站 评论（SAP-C02 614 处），解析器有意剥掉，**保持剥掉**。
理由：Melete 的价值链是「AI 裁决给出接近正确的答案 → 学习者仍不确定时与 Melete 的 AI 讨论（P4）」。
第三方用户之间的讨论过程不在这条链上，对学习者没有用；投票百分比作为一个答案主张来源已足够。
不进 questions.json、不进 DB、不做 AI 裁决的输入。

## 已定决策：重复题只导入正本，主张并入（2026-09-02，用户拍板，方案 b）

解析器给后出现的重复题挂 `duplicate_of_N`；`load.py` 不导入它，把它的答案主张并入正本
（缺的来源追加 + `meta.from_no`；同来源不同答案记 `meta.variants`）。FSRS 只剩一张卡，schema 不改。
SAP-C02：#85→#84、#438→#10，导入 527 题。

## 技术债（发现即登记，不阻塞主线）

- [ ] `web` 的 `npm run lint` 失效：脚本是 `next lint`，Next 16 已移除该命令
      （报 `Invalid project directory ... /web/lint`）。需换 ESLint CLI 直调。
- [ ] OpenAPI 的 query 枚举（如 `type: [domain, topic, concept]`）**运行时未校验**：
      `?type=service` 返回 200 空数组而非 400。oapi-codegen strict-server 只生成类型，
      需加 `oapi-codegen/nethttp-middleware` 的 OpenAPI 请求校验中间件。
- [ ] 题干 / 选项译文在 DB 里没有落点（`question.stem` / `choice.body` 单语）。
      SAP-C02 富化产物带 `translation`，导入前需定多语言方案：`question_i18n` 表 或 加列。

## 首页重构（方案已定，待实施）

方案页：https://claude.ai/code/artifact/<ARTIFACT_ID>
原则：**每个模块都是入口，不是事实**。12 栏面板；「继续」拆两条轨道（顺序进度 / 上次专项）。

- [x] 「上次专项」的存储：`attempt.context`（用户拍板 2026-09-02）；`/me/resume` 返回 sequential + focus 两条轨道
- [x] 宣言挪去登录页；34% 数据带、打架题删除
- [x] 标签轴文案通用化（`tagTypeLabel(bank.meta, type)`）
- [x] 12 栏面板首页上线（2026-09-02）
- [ ] `/me/tag-stats` 尚无 `bank` 参数（progress / resume 已有）；多题库时补
- [ ] 「当前题库」= 第一个题库；多题库时改为最近活跃（`currentBank` 一处）

## 阻塞 / 待定

- [x] ~~GitHub 仓库尚未创建~~ —— 已建，`31f0b36` 已推到 origin/main
- [ ] hostname 未定
- [x] ~~P1 的模型未定~~ —— 已定分层策略，见上
- [ ] **P1 的 Anthropic API key 未配**（硬阻塞，没有 key 跑不了 Batches）
- [ ] P1 全量预算上限未定（等校准批实测数据）

## 已知风险

| 风险 | 说明 |
|---|---|
| 题库答案不可信 | 340 道标注与社区不一致。**若照搬标注答案会背错三分之一** —— 这是 P1 存在的根本理由 |
| 104 道无投票数据 | 题号 905–1011，只有单一答案来源，AI 裁决无对照 |
| 仓库可见性 | 必须永久 private，理由见根目录 CLAUDE.md |
