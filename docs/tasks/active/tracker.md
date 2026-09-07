# Melete 任务进度

> 最后更新 2026-09-05

## 分期总览

| 阶段 | 内容 | 状态 |
|---|---|---|
| P0 | PDF → 结构化 JSON | ✅ 完成 |
| P1 | AI 富化：裁决 + 解析 + 三维标签 | ✅ **完成 1019/1019** |
| P2 | 最小可用：刷题 / 看解析 / 基础统计 | ✅ 完成（含认证、学习记录、个人分析） |
| P2.5 | 部署上线（集群 + Akasha 代理 + 域名） | ✅ 完成（2026-09-03/04） |
| P3 | FSRS 调度 + 自评可信度纠正 | ✅ **完成并上线（2026-09-04）** |
| P4 | AI 实时问答 | ⬜ 待办 |
| P5 | atlantis 内容迁入 + concept 关联 | ⬜ 待办 |
| P6 | 第二个题库，验证扩展性 | ✅ SAP-C02 已入库（2026-09-03） |
| P7 | 第三个题库：IPA IT パスポート（日文 + 扫描版） | 🔄 **令和8年度 100 题已入库**；剩余 14 套待转写 |

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

## P1 · 旧待办 ✅ 已作废或完成

⚠️ 下面这一节是 P1 开工前写的计划，**其中「走 Batches API」那条路线整个没走**——
最终改由 Claude Code 会话逐片生成（订阅额度闲置，Batches 那笔钱没必要花）。
所以「API key / 校准批 / 预算上限」三项**不是待办，是作废**。
保留这一节是为了留住下面那张决策表（尤其 concept 词表那条，它记着一笔后来真的还了的账）。

### 已定决策（2026-09-01）

| 决策 | 结论 | 理由 |
|---|---|---|
| 模型策略 | **分层**：低档 Sonnet 5 `effort=medium` / 高档 Opus 5 `effort=high` | 钱花在真正需要推理的题上。分层判据在 `questions.json` 里已可判定，无额外成本 |
| 高档口径 | 分歧 ∪ 无社区投票 ∪ 低共识 ∪ 有告警 = **481 道**（低档 530） | 119 道无投票题只有 `bank_label` 一个来源，**AI 是唯一的第二意见**；若下放低档等于照搬题库标注，正是本项目要避免的事 |
| concept 词表 | **AI 自由产出**（给种子词表引导收敛），P5 再与 atlantis 对齐 | concept 按设计是全局共享的，atlantis 只是消费方之一，不该反过来当约束源。代价：需一轮人工归并 |

⚠️ **thinking token 计入输出计费，是成本主导项**（不是模型单价）。全量成本以校准批实测 token 为准，不用估算值拍板。

- [x] ~~前置：Anthropic API key~~ **作废** —— 没走 Batches，整条管道零 API 成本
- [x] ~~跑 40 道校准批~~ **作废** —— 同上
- [x] ~~拿校准批实测 token 重报全量成本~~ **作废** —— 同上
- [x] 写提示词：一次调用产出 verdict + reasoning + explanation + 三维标签
      → 落在 `pipeline/banks/<bank>/enrich_spec.md` + `enrich.py next` 的输出里
- [x] ~~从 atlantis 抽出 concept 路径白名单~~ **改为**：给种子词表引导，AI 自由产出
      ⚠️ **这笔账后来还了** —— SAP 造出 1002 个独有 concept、72% 只用一次，见技术债
- [x] ~~走 Claude Batches API 跑 1011 道~~ **改为** 会话逐片生成，分片可恢复
- [x] 质量门禁：`enrich.py check`（verdict 合法性 / concept 路径 / 译文齐备）
- [x] 47 道低共识题人工抽查
- [x] 21 道解析告警题单独处理
- [x] 产出 `data/aws-saa-c03/enriched.json`（1019 题，283 万字）

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

## P2.5 · 部署上线 ✅ 完成（2026-09-03/04，仅剩 CI 自动化）

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
      ⚠️ **这条不再是「早晚要做」，而是有了明确要验的东西** —— 见技术债的 `COLLATE`：
      同一份 schema 在 MySQL 与 TiDB 上的字符串折叠规则不同，而**本地测不出来**。
      建表时逐列核对实际生效的 `COLLATION_NAME`，别只看建表语句跑没跑通。
- [x] 两个 Dockerfile + 本地构建与容器联调验证
      · api 11.2MB（distroless/static:nonroot）· web 237MB（standalone + 非 root）
      · 后端用 `--platform=$BUILDPLATFORM` 交叉编译，避开 QEMU 跑 go build（慢 5-10 倍）
      · web 镜像**环境无关**（无 NEXT_PUBLIC_*），改 API 地址只需改 ConfigMap
      · 已验证 amd64 + arm64 双架构可构建
- [x] config 仓部署清单 `clusters/requiem/apps/melete/`（7 文件）+ ArgoCD Application
      · 9 个资源全部通过服务端 dry-run schema 校验，未 apply
      · DSN 指向 开发机 MySQL（验收合格后才迁 TiDB，流程写在 config.yaml 注释）
- [x] Akasha client 追加生产回调地址（保留 localhost）
      · 后端成为唯一 OIDC client，web 与 iOS 共用同一 client_id → 同一 pairwise sub → 同一账号
      · Akasha 的 client 注册改由 config 仓 `clusters/requiem/apps/akasha/clients.yaml` 挂载
- [x] CF 建两条 DNS CNAME → tunnel UUID（Proxied）：`melete` 与 `melete-api`
      · 已生效，两域名返回 404 = 链路已通到集群 Gateway，只差 HTTPRoute 接住
      · 已更新 config 仓 CLAUDE.md 的「在用 hostname 清单」
      · wildcard 已能兜底，但按 config 仓规矩业务上线必须显式建
      · cloudflared 无需改动（远程管理模式，分流由 HTTPRoute 决定）
      · 本机无 flarectl，可直接调 CF API（token 在 config 仓 infra.env）
- [ ] 后端对 `/me/*` 加 `Cache-Control: no-store`（不依赖 CF 默认行为，自己声明意图）
- [x] 上线并可访问（melete.bukahou.com / melete-api.bukahou.com）
- [x] ArgoCD 改自动同步（与 geass-v3 一致）
- [ ] 最后接 GitHub Actions（path filter / buildx / 回写 config 仓）
      · 目前是手工 `docker buildx --push` + 改 config 仓 `newTag`，一次上线约 3 分钟
      · tag 规约 `v1.0.0-<sha7>`，api 与 web 同 tag

## P3 · FSRS ✅ 完成并上线（2026-09-04）

- [x] **用官方 `go-fsrs/v3 v3.3.1`，⛔ 不手写** —— 19 个权重是一起拟合的，写错了不报错、只是排期变烂
- [x] `card` 表状态机；作答与卡片**同一事务**（只写 attempt 而卡片没更新 = 这题再也不会到期）
- [x] `mode=due` 复习队列，**按到期时间升序**（复习队列不是浏览列表，按题号排等于把最该复习的排到最后）
- [x] 首页「复习 N 题」入口 + 学习台「该复习的」磁贴
- [x] `melete-backfill`：把历史作答重放成卡片状态，默认 dry-run

### ⭐ 自评可信度纠正（用户提的「防嘴硬」，本项目特有）

FSRS 信任用户自评，而选择题里自评可以不诚实。实测代价：同一串作答只改第 4 次的评分，
**一次嘴硬把复习推迟 44 天、稳定性虚高 17.5 倍**。

| 规则 | 触发 | 压到 |
|---|---|---|
| ① | 答错 且 自评 ≥「掌握」 | 1 |
| ② | 答对 但用时短到看都没看题面 | 2 |
| ③ | 无参考答案的 6 道题 | 不纠正 |

贯穿规则**只降不升**（穷举 256 种输入组合验证）。⚠️ `attempt.rating` 永远存用户按下的那个键，
纠正是调度时现算的推导结论 —— 规则改了能整批重算，覆盖掉就回不来（与 answer_claim 不合并三方主张同一条纪律）。
纠正理由随响应返回并显示给用户，⛔ 不偷偷改调度。

### 决策留档

- **读题速度常量取 1200 字/分**（口径是「有没有看」不是「读完」）。
  ⚠️ 初版取 200 字/分并注释成「保守取值」—— **方向反了**：字/分越低要求时间越长、误判越多。
  开发库唯一一条真实作答当场证伪（359 字用时 30.3 秒 = 711 字/分，正常阅读速度却被判「读不完」）。
  **全部单测都没抓到 —— 测试里的数值与该常量出自同一个错误假设。**
  待真实作答积累后，用「答对且自评 ≥3」那批的用时分布回头校准。
- **交叉验证（同概念其它题的正确率）暂不做**：它会让调度变成**非局部的** ——
  答第 300 题会改变第 47 题的复习时间，难解释、难调试、一个错标签污染一整组。
  等前两档跑一阵、有真实数据能实测「它纠正了多少次误判」再决定。

## P4 · AI 实时问答 ⬜

- [ ] 后端 `/api/ai/ask` 代理（key 不出后端）
- [ ] 单用户日调用上限（防误用烧钱）
- [ ] 前端追问入口

## P5 · atlantis 迁入 ⬜

- [ ] 复制 114 篇 × 2 语言内容（**原仓库不动**）
- [ ] `article` / `article_tag` 导入
- [ ] concept 标签双向关联
- [ ] 错题 → 知识条目跳转

## P6 · 第二个题库 ✅ 完成（2026-09-03/05）

- [x] 选题库：**AWS SAP-C02**（英文原版，含完整社区讨论）
      · 弃用中英对照版：`AWS Config` 被打成 `Con g` 四百余处（pdf-lib 二次加工丢连字）
      · 英文原版还白捡了每条投票背后的论证 —— SAA 只有百分比
- [x] **验证 core 无需改动 ✓** 只写了 `banks/aws-sap-c02/parse.py`（231 行，从零写）
      ingest / enrich / load 全部零改动直接支持
- [x] P0 完成：529 题、题号零缺失、零告警 98.1%、投票覆盖 99.1%
      **答案分歧率 54.6%**（SAA 是 38%）—— Professional 级标注更不可靠
- [x] core/enrich.py 加**翻译字段**支持（`spec.translation.locale` 声明，逐题硬门禁）
- [x] 开第二个富化会话 —— 529 题富化完成
- [x] **译文落库**（2026-09-05）：`load.py` 一直没读 `enrichment.translation`，
      于是网站上 SAP-C02 全是英文，而中文译文躺在 enriched.json 里没人用。
      ⚠️ 管道该做的都做了（连质量门禁都写了），只有最后一步落库没接上 ——
      与「登录漏了第三条路径」是同一形状：上游做对了、最后一步没接上，且不报任何错。
      修法：`display_text()` 是取题面文本的**唯一入口**（题干与选项走同一处，
      杜绝「题干中文、选项英文」这种不会报错的半吊子状态）+ 落库端补一道译文齐备门禁。

---

## 已定决策：素材里的社区讨论正文**不抽取**（2026-09-02，用户拍板）

PDF 里有完整的源站评论（SAP-C02 614 处），解析器有意剥掉，**保持剥掉**。
理由：Melete 的价值链是「AI 裁决给出接近正确的答案 → 学习者仍不确定时与 Melete 的 AI 讨论（P4）」。
第三方用户之间的讨论过程不在这条链上，对学习者没有用；投票百分比作为一个答案主张来源已足够。
不进 questions.json、不进 DB、不做 AI 裁决的输入。

## 已定决策：重复题只导入正本，主张并入（2026-09-02，用户拍板，方案 b）

解析器给后出现的重复题挂 `duplicate_of_N`；`load.py` 不导入它，把它的答案主张并入正本
（缺的来源追加 + `meta.from_no`；同来源不同答案记 `meta.variants`）。FSRS 只剩一张卡，schema 不改。
SAP-C02：#85→#84、#438→#10，导入 527 题。

## P6 · 第二个题库 SAP-C02 ✅ 富化完成并入库（2026-09-03）

- 529 题富化 22 片全绿，「解析」会话约 6 小时（21:06–03:30），Max 配额约 27%
- 入库 527 题（#85→#84、#438→#10 折叠）· 主张 1561 · 解析 527（zh）· topic 标签 183
- data_issue 4（#6 策略块丢失、#200/#337/#515 标签错位假分歧）· 带 notes 97 题（含建议人工复核）
- 裁决 ≠ 题库标注 约 48%；同时反 bank 与反社区的：#5 #20 #486 #487 #526 等，notes 可筛
- 素材侧共修 12 类文本缺陷（连字 / 吞句点 / 重复标签 / 重复题 / 30 余处孤立 OCR 错字），
  全部登记在 questions.json 的 repairs / warnings 字段
- **待决**：concept 标签膨胀 —— SAP 新造 987 个 concept（72% 只用 1 次），SAA 只有 73 个。见「阻塞 / 待定」
- 译文（translation）在 enriched.json 里，DB 无落点，未入库

## 已定决策：Akasha 登录由 melete-api 代理（2026-09-03，iOS 真机 bug 引出）

根因：Akasha 里 melete 是 confidential client（/token 强制 secret，PKCE 不免）且 pairwise sub ——
原生 App 自己走 OIDC 走不通，另注册 public client 会分裂账号。geass-v3 同款结论。
方案：后端两个导航端点 `/auth/oidc/{start,callback}`（`pkg/oidcrp` 照搬 geass-v3），
web 与 iOS 都经此进出，同一 client_id → 同一账号；web 不再持有 client_secret。
Akasha 只追加两条后端回调白名单（prod + localhost）。
- iOS：token 对走 fragment 到 `melete://auth/callback`；失败 `?oidc_error=`
- web：refresh token 当一次性票据走 query → web 服务端拿去 /auth/refresh 换正式一对（轮换即作废）
- **已定（2026-09-03，用户拍板，两条独立 task，akasha 会话执行）**：
  - Task A：`pkg/oidcrp` 以嵌套 Go 模块入驻 akasha 仓（PUBLIC，零凭据），tag `pkg/oidcrp/v0.1.0` 零行为变更。
    ✅ melete 已切换（2026-09-03）：删 `backend/pkg/oidcrp`，import `github.com/bukahou/akasha/pkg/oidcrp v0.1.0`（akasha 5922c22）
  - Task B：Akasha clients 由 Secret 挂载的 `clients.yaml` 启动加载，`clients` 表退役（留置 30 天）。
    **melete 侧待办**：按 akasha 定的 schema 把现状（melete 2 条后端回调 + geass 1 条）写进
    `config/clusters/requiem/apps/akasha/clients.yaml`。这之后白名单变更 = 改 yaml + push + 人点 SYNC，手改生产库的路物理消失
  - 设计讨论记录（含 akasha 维护方的五条修正）见本次会话；决策依据：clients 在运行时无合法写路径，是伪装成状态的配置

## P7 · 第三个题库：IPA IT パスポート 🔄（2026-09-05 起）

**为什么选它 —— 版权是首要理由，不是加分项。**

现有两个题库的素材授权状况不适合对外分发 ⇒ ⛔ **不能拿去社内推广**。
而 IPA **官方主动公开**历年问题与解答供人学习 —— 社内推广完全正当。

⭐ 2026-09-07 更新：代码与数据已经拆开（代码公开仓 / 数据私有仓，见 CLAUDE.md
「边界」），所以「拆出去开源」不再是将来时 —— 已经做完了。剩下的只是
以哪个题库对外演示，而这一条的答案仍然是 IPA。
用户所在公司要求这张证书，可在社内推广。

素材：`www3.jitec.ipa.go.jp` 官方公开问题，**2016–2026 共 15 套 × 100 题 ≈ 1500 题**，
30 个 PDF 已取齐放收件区（⛔ 不进仓库）。

### 这个题库暴露了三处现有管道吃不下的地方（P6 本该暴露而没暴露的）

⚠️ SAP-C02 进来时 **`tag_types` 与 SAA 一模一样**（同为「考纲域 / 服务」，
`enrichment_field` 都是 `services`），解析器还是复制改的 ——
它证明的是「同一厂商的第二门考试能进来」，不是「换一个知识领域能进来」。

1. **题目 PDF 是纯扫描图** —— 零嵌入字体，`pdftotext` 抽出 56 字节（每页一个换页符）。
   ⇒ 新增 `pipeline/core/transcribe.py`：渲染页图 → 会话逐片视觉转写 → questions.json。
   架构与 enrich.py 同源（不调 API、分片可恢复、扫目录即状态）。
   ⛔ 转写与富化**分成两段**，不合并成「一次读图直接产出解析」——
   合了就分不清「读错了」和「判错了」，而这两种错的修法完全不同。
2. **答案在另一份 PDF** —— 而它**有完整文本层**。⭐ 于是它成了一个**外部哨兵**：
   不参与转写，却能卡住转写（题号集合必须完全一致）。漏页、串页、错位都会被抓。
   ⚠️ 这类哨兵的价值在于**它不编码「我猜会出什么错」**。
3. **题号会撞唯一键** —— 15 套各自从 問1 数起，而 `uk_q_bank_no (bank_id, external_no)`。
   ⇒ 已在转写产物里加 `session` 字段（如 `2026r08`）。
   ⚠️ **导入前必须定**：加 `question.session` 列并把唯一键改成三元组（推荐，界面能显示「令和8 #12」），
   还是合成全局题号（零 schema 改动，但界面上 `#2026012` 没有意义）。**需用户批准 schema 改动。**

### 进度

- [x] 15 套素材取齐 + 结构体检：**全部同质**（零文本层、每页一张 1432×2026 扫描、
      答案 PDF 全部恰好 100 题）。⭐ 原本担心「2016-2020 是特別措置試験、版式可能不同」——**没有发生**
- [x] `transcribe.py` + `banks/ipa-ip/transcribe_spec.json`
- [x] **令和8年度 100 题转写完成**，与答案 PDF 完全对账
      · ⭐ `has_figure = 0/100` —— **一张真正的图都没有**，数据表 / 关系 schema / 伪代码 / 选项组合表全部无损转成文本
      · `sections` 三条覆盖 100 题无缺口：問1-34 ストラテジ系 · 問35-54 マネジメント系 · 問55-100 テクノロジ系
        ⇒ **`domain` 按题号区间机械映射，⛔ 不必让 AI 猜**（官方印在卷子上）
- [x] **决策：`topic` 轴用官方シラバス「中分類」23 个（封闭词表）** —— 用户 2026-09-05 拍板
      ⚠️ 关键不是「官方 vs AI」而是「**封闭 vs 开放**」：仍由 AI 判每题属于哪个中分类，
      但它**造不出新词** —— SAP 那 1002 个只用一次的 concept 在结构上就产不出来。
      额外好处：中分类是考纲本身的切分，「ネットワーク 正确率 42%」能直接对照官方シラバス。
- [x] 写 `enrich_spec.json` + 富化（解析用日文）+ 入库
      · 100/100 题 check 全绿，`question.session='2026r08'`，唯一键已改成三元组
      · **topic 用掉 23 个中分類里的 22 个** —— 封闭词表按设计生效，⛔ 零新造词
      · **concept 产出 0 条**（`concept_seeds: []`）—— 等 P5 归并完那 1075 个再说
      · ⭐ 官方答案作为权威源：`verdict ≠ bank_label` 会被当成缺陷信号而非「有价值的分歧」
- [ ] **决策：其余 14 套要不要转** ← 待用户拍板（建议先抽检 2016 + 2021 两套）
- [ ] **⚠️ 剩下 14 套跑之前要知道的**：令和8 那套 `has_figure = 0/100` 且没有
      「下划线强调的否定」那类题 —— **一套跑干净不构成后面 14 套也干净的证据**。
      2016-2020 是特別措置試験，版式虽已验同质，但题目内容没验过

### 转写的硬规则（都在 `transcribe.py next` 里打印）

- 表格**必须**转成 Markdown —— 它是内容不是装饰，答案往往直接依赖表里的数字
- 真图置 `has_figure` 并写 `[図: 简述]`，⛔ 不凭想象补图的内容
- ⛔⛔ **被下划线强调的否定必须保留**（2016h28h 問90「〈必要がない〉ものだけを…」）——
  丢掉它整道题意思就**反了**，而且「正确答案」会看起来是错的。
  ⚠️ 这比漏抄表格严重得多：漏表格一眼能看出缺东西，**否定反转看起来完全正常**。
  ⚠️ 令和8 那套恰好没有这类题 —— **第一套跑干净不构成缺陷不存在的证据**
- 用文字替代原图表达（线型 / 颜色 / 箭头）时补方括号说明，否则题干的自我指涉会悬空

### 一个待办：官方附录该进 `article` 表

p050-056 是 **「表計算ソフトの機能・用語（IT パスポート試験用）」** ——
考电子表格的题，其函数语义定义在附录里而不在题干里。本套只有 問74 依赖它（凭常识可答），
但跨 15 套一定更多，其它年份可能还有别的附录（擬似言語記法之类）。
⇒ 归 P5 的 `article`（知识条目 = 支撑材料），⛔ 不该只当成「没有题的几页」丢掉。

---

## localauth 接入（cross-exam 005 §35 验收）🔄

> **主档在 `config/cross-exam/`**：验收计划 `2026-09-06-melete-localauth-acceptance-plan.md`，
> 裁决与核实记录在 `2026-09-04-localauth-delivery-checklist.md` §35–§38。
> 这里只记 melete 侧的状态，⛔ 不复制决策内容（复制 = 两份会漂）。

**验收方式**（用户 2026-09-06）：melete 完全删除自己的登录逻辑，只靠
`github.com/bukahou/gokit/localauth` 重建 —— 对可复用性最硬的检验。

| 阶段 | 内容 | 状态 |
|---|---|---|
| 1 | 建表 + `internal/localauthx/` 骨架（⛔ 不接线） | ✅ `69e1e2c` |
| 2 | 账号 id `int64` → UUID（波及 9 文件 17 处 + 学习表 + OpenAPI + web） | ⬜ |
| 3 | 接线：登录 / 刷新 / 登出 / 会话列表 | ⬜ |
| 4 | 改密 / 首次设密 / HIBP | ⬜ |
| 5 | 注册 / 找回 / 改邮箱 | ⬜ |
| 6 | 🔴 删旧实现 + DROP 旧表（不可逆，动手前报） | ⬜ |

**阶段 1 已达成**：7 个编译期契约；契约套件 FailureStore 6/6 · SessionStore 9/9
· Verification 8/8；四条变异验证全部真红过。

### ⭐ 本阶段最值钱的一条教训（比任何单个缺口都值钱）

两处 DDL 缺口 —— `user_sessions.prev_refresh_hash` 与 `users.uk_email` ——
**成因完全相同**：照着方法签名设计存储，没读**记录类型的字段**与
**返回类型隐含的约束**。⚠️ 第一次我只在 commit 里记了结论、没改做法，
于是**原样复发了第二次**。

⇒ 做法已改：**实现任何 Store 之前，先把它读写的 record 类型逐字段
对照表结构过一遍，并检查返回类型隐含的唯一性/存在性约束。**

（`UserByVerifiedAddress` 返回**单个** userID ⇒ 邮箱必须唯一 ——
这条约束只写在返回类型里，方法签名和字段列表里都看不见。）

### 📌 一处需要更正的记录

`69e1e2c` 的 commit message 里写了 `uk_email`「三家都受影响」。
**不准确** —— work 实测 **geass-v3 线上 `users` 已有 `uk_email`**（重复邮箱 0 行），
它独立于模板补上了。准确说法是：**模板缺 + melete 缺（已补）+
atlhyper 将来会继承（模板已修，风险消除）**。

⛔ **刻意不改那条 commit message**：该 hash 已报给 work 并入案卷，
改写会让案卷里的引用失效 —— 而可核验的 hash 是这次协作的验证链本身。
⭐ 为一句措辞破坏它，代价比措辞本身大。更正记在这里。

## 技术债（发现即登记，不阻塞主线）

- [ ] **melete 的 ConfigMap / Secret 是普通资源，改了不触发滚动**。2026-09-03 改 DSN 时
      ArgoCD 报 Synced 但 pod 的 env 仍是启动快照，只好逐个删 pod 才生效 —— 「配置已同步」
      与「配置已生效」不是一回事。应改成 kustomize 的 configMapGenerator / secretGenerator
      （带内容哈希后缀，改一行即自动滚动），akasha 的 clients Secret 已是这个模式。
      顺带把值挪进 .env 文件，注释仍可保留（env 格式支持 #）。

- [ ] `web` 的 `npm run lint` 失效：脚本是 `next lint`，Next 16 已移除该命令
      （报 `Invalid project directory ... /web/lint`）。需换 ESLint CLI 直调。
- [ ] OpenAPI 的 query 枚举（如 `type: [domain, topic, concept]`）**运行时未校验**：
      `?type=service` 返回 200 空数组而非 400。oapi-codegen strict-server 只生成类型，
      需加 `oapi-codegen/nethttp-middleware` 的 OpenAPI 请求校验中间件。
- [ ] **题干 / 选项在 DB 里仍是单语**（`question.stem` / `choice.body` 无 locale）。
      2026-09-05 的处置是**让译文覆盖展示文本**（`bank.locale` 随之改成 zh），
      英文原文只留在 `enriched.json` 里 —— 中文题面立刻可用，但**语言切换做不了**。
      要切换需 `question_text` / `choice_text` 两张表（`(question_id, locale, stem, origin)`），
      `origin ∈ {original, translation, source}` 记录哪一份是考试原文。
      ⚠️ 届时重导一次即可，不会因为今天这样做而多付代价。

- [ ] 🔴 **`db/schema.sql` 没钉死 `COLLATE`**，排序规则跟着服务器默认值走：
      开发库 MySQL 8.0 是 `utf8mb4_0900_ai_ci`，**TiDB 默认是 `utf8mb4_bin`（逐字节比）**。
      同一份 schema 换个库，`uk_account_username` 的折叠规则就变了 ——
      `alice` / `Alice` / `ALICE` 在 TiDB 上会是三个账号。
      ⚠️ **本地怎么测都测不出来**：本地两种规则都折叠大小写，
      所以「我们试过没问题」证明不了生产没问题。**迁 TiDB 前必须处理。**
      （cross-exam 005 实施期由 geass-v3 在两个真库上实测得出）

      🔴 **2026-09-06 追加一条更锋利的后果 —— 它不是「多几个账号」，是「防护失效」**：
      localauth 的 `login_failures`（账号维度退避计数）**必须与 `users` 同折叠规则**，
      否则 `Alice` / `alice` / `ALICE` 落进不同的桶 ⇒ **变换大小写即可绕开账号退避**。
      来源是模块 `memstore.go` 自己的注释警告，我在 dev 库实测确认了两侧现状：

      ```
      dev（MySQL 8.0）  users 与 login_failures 均 utf8mb4_general_ci
                        Alice/alice/ALICE 三次 → 同一个桶，计数 3   ✅
      TiDB（默认 utf8mb4_bin）                  → 三个桶，各计数 1   🔴 推断，⛔ 未实测
      ```

      ⚠️ **两个数据点的性质不同，别混**：上面那行是实测，下面那行是**按 TiDB 默认排序规则推断**——
      ⛔ TiDB 禁令期内验不了。但正因为验不了，**建表时就得钉死**，
      而不是等上生产再看。⭐ 这也是「新表钉死 ≠ 债还清」的具体含义：
      新表守住了，存量表还没有，而存量表里就有 `account`（将来的 `users`）。

- [x] ~~**限流键只有 `clientIP`**（`httpauth/ratelimit.go`）~~ →
      **2026-09-06 用户裁定：`ratelimit.go` 整个删除，限流移交集群层，接受当前零限流**
      （口径：「限流暂不考虑（现在基本没人），整体纳入集群层待办」）。
      ⚠️ **是有意的风险接受，不是问题解决了** —— 债的去向是集群层
      （Cilium CEC + Envoy `local_ratelimit`，键取 `CF-Connecting-IP`），
      由 work 记在 `project-cluster-rate-limit-pending`。⛔ 删掉代码 ≠ 债还清。
      执行时机：随 localauth 接入（见 `config/cross-exam/2026-09-06-melete-localauth-acceptance-plan.md` §1.3）。

      ⭐ **这条留下来的两份认知（比结论值钱，别随代码一起删）**：
      ① 模块 `guard.go` 注释：账号维度**故意不省** bcrypt CPU（提前判定会造出
         「被锁账号返回得快」的预言机）⇒ **IP 维度是唯一能省 CPU 的地方**
         ⇒ 轮换 IP + 轮换用户名，两个键都不重复 ⇒ bcrypt 每次都跑，**无需任何凭据**。
         而 dummy-hash 恒定耗时设计**保证**不存在的用户名也要烧满 36ms ——
         ⚠️ 反枚举的代价正是这个洪水在应用层防不住的根因。
      ② 账号维度**不能简单加上去**：全局按账号拒绝/延迟会变成 DoS 面
         （攻击者用自己的失败尝试消耗受害者配额）且是预言机。
         方向：拒绝按 `(来源, 账号)` 且计数与账号是否存在无关；全局按账号只用于告警。

      🔴 **本条附带一个自查教训（2026-09-06）**：我在 localauth 计划 §1.3 里主张
      「保留 `ratelimit.go` 以挡轮换 IP 洪水」—— 而**本条第一行原文就写着
      「换 IP 轮换即可绕开」**。登记册和当下的论证各说各的，两份都是我写的。
      ⚠️ 登记不会自己跳出来反驳你，**得在论证的时候主动去读**。
      与「过滤器不是证据」同族：拿未经核对的中间物当证据。

- [ ] **bcrypt cost 的数据侧缺口**：cost 编码在 hash 串自身，校验耗时由**存的那个 hash** 决定。
      调高 cost 是安全惯例，但存量行改不了（没明文）⇒ legacy 36ms / 新账号 145ms，
      **「快」唯一标识老账号**。⚠️ 改 cost 不是改一个常量，是一次要连带处理存量行的迁移。
      代码侧已做到：dummy 启动时按当前 cost 现算（`account/service.go`）。

- [ ] **`concept` 词表需要归并**：1075 个里 **SAP 独有 1002 个、两库共用仅 70 个**，
      772 个只挂一道题（72%）。而 `concept` 按设计是「全局共享、链到知识条目」——
      现在它没在做它该做的事。⚠️ 这是 P1「AI 自由产出 concept」那个决策记下的账
      （当时就写了「代价：需一轮人工归并」），**是 P5 迁入 atlantis 的前置**。
      ⇒ 教训已应用到 P7：改用封闭词表，让缺陷写不出来。

## 首页重构 ✅ 已实施（2026-09-03/04）

方案页：https://claude.ai/code/artifact/<ARTIFACT_ID>
原则：**每个模块都是入口，不是事实**。12 栏面板；「继续」拆两条轨道（顺序进度 / 上次专项）。

- [x] 「上次专项」的存储：`attempt.context`（用户拍板 2026-09-02）；`/me/resume` 返回 sequential + focus 两条轨道
- [x] 宣言挪去登录页；34% 数据带、打架题删除
- [x] 标签轴文案通用化（`tagTypeLabel(bank.meta, type)`）
- [x] 12 栏面板首页上线（2026-09-02）
- [x] 两页结构（2026-09-03）：首页 = 题库选择台（每卡带进度 / 两条轨道 / 继续按钮 + 跨题库统计带 + 最近会话）；
      `/banks/[slug]` = 学习台（原首页四面板下沉，页头统计带）。所有数据带 `bank` 参数，不再有「第一个题库」假设
- [x] `/me/tag-stats` 加 `bank`；新增 `/me/overview`（今天 / 连续 / 累计）与 `/me/recent`（会话 = 同题库同 context 相邻 ≤30 分钟，从 attempt 现算不存表）
- [x] 🔴 **修掉翻页漏题**（2026-09-04，从 P2 就在）：刷题页翻页用 `offset = i`、
      点下一题 `i+1`，而 due/wrong/unsure/unseen 四个模式的集合**会随作答缩短** ——
      做完 offset 0 那题它就离开集合，再取 offset 1 就漏一题，**不报任何错**。
      实测队列 [3,1,2]：做完 #3 点下一题落到 #2，#1 被跳过。
      修法：答完之后回到 offset 0（队列自己前进了），不答则 offset+1 且按钮改叫「跳过」。
      ⇒ 导航因此从页面搬进 `DrillCard` —— 行为依赖「答没答」，而只有它知道。
      回归测试带**对照组**（旧策略必须仍会漏），一个从没见过红的修复不算修复。
- [ ] 连续天数按 JST 写死（`study/sessions.go` 的 studyTZ）；将来进 account 偏好
- [ ] 题库卡的等级章从名字里正则认（Associate / Professional / Level N）；spec 可加 `level` 进 meta 替代

## 阻塞 / 待定

- [x] ~~GitHub 仓库尚未创建~~ —— 已建，`31f0b36` 已推到 origin/main
- [x] ~~hostname 未定~~ —— `melete.bukahou.com`（web）/ `melete-api.bukahou.com`（iOS 直连），已上线
- [x] ~~P1 的模型未定~~ —— 已定分层策略，见上
- [x] ~~**P1 的 Anthropic API key 未配**（硬阻塞）~~ —— **该阻塞已不存在**：
      `enrich.py` 最终没走 Batches API，改由 Claude Code 会话逐片生成
      （订阅额度闲置，Batches 那笔钱没必要花）。`transcribe.py` 同理。
      ⇒ 整条管道**零 API 成本**，也就不存在预算上限问题
- [x] ~~P1 全量预算上限未定~~ —— 同上，作废

### 待用户拍板（当前）

- [ ] **IPA 的 `topic` 轴**：官方シラバス 23 个中分類（封闭词表，推荐）vs AI 自由产出
- [ ] **其余 14 套 IPA 要不要转**（≈1400 题；建议先抽检 2016 + 2021 两套）
- [ ] **`question.session` 列**（IPA 15 套题号会撞唯一键）—— 属 schema 改动，需批准

## 已知风险

| 风险 | 说明 |
|---|---|
| 题库答案不可信 | 340 道标注与社区不一致。**若照搬标注答案会背错三分之一** —— 这是 P1 存在的根本理由 |
| 104 道无投票数据 | 题号 905–1011，只有单一答案来源，AI 裁决无对照 |
| 仓库可见性 | 代码公开 / 数据私有，边界见根目录 CLAUDE.md。⚠️ 对外演示用 IPA 题库 —— 它是官方公开素材 |
| 迁 TiDB 的隐形差异 | `COLLATE` 未钉死（见技术债）。⚠️ **本地测不出来** —— 本地两种排序规则都折叠大小写 |
| 「第一套跑干净」不构成证据 | IPA 令和8 那套没有被强调的否定，2016 那套有。**只在部分素材出现的缺陷，抽样通过说明不了什么** |
