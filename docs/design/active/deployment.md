# 部署与 CI/CD 设计

> 状态：active · 2026-09-02
> 目标：先把 Melete 部署上去跑起来，再慢慢优化。
> 本文只写**设计与决策**，实施步骤见文末「实施顺序」。

## 0. 前提变更：API 要对公网暴露（2026-09-02）

**新约束：后续要开发 iOS 端**（参照 `~/work/github/geass-mobile` 的既有规范）。
这推翻了本文原先「API 不对公网暴露 + BFF 单出口」的设计 —— 原设计的每一条论据
都建立在「唯一客户端是自己的 SSR」之上，多了一个原生客户端后全部失效。

### 这个约束暴露了当前代码的一个真实漏洞

```
/auth/sso 接收 {sub}，而 id_token 的验证在 web 侧
→ API 只是「相信 web 说的这个 sub」
→ 一旦公网暴露，任何人 POST {sub:"别人的sub"} 即可登录任意账号
```

现在被服务间密钥挡着，公网暴露后必须修。而 iOS 也要求同一个修复
（App 自己拿 id_token 后调 API，API 不能信任客户端自称身份）——
**同一处改动同时解决两件事**。

### 改造方向：按 geass-v3 + geass-mobile 的既有规范

调研结论（三个仓库的现状）：

| 来源 | 规范 |
|---|---|
| `geass-v3/internal/auth/DECISIONS.md` | access token **HS256 / TTL 1h**；auth 服务不查库、纯签发；显式拒 `alg=none`；密钥 32+ 字节 |
| `geass-v3` user_sessions 表 | **refresh 不做成 JWT**，落库为会话行（`refresh_token` / `expires_at` / `is_valid` / `device_info`），吊销 = 改 `is_valid` |
| `geass-mobile/CLAUDE.md` | 双 token 存 Keychain；网络层 interceptor：401 → 自动 refresh → 重试原请求 |
| `akasha/internal/client` | 已支持 native app：自定义 scheme（`com.bukahou.app://cb`）精确匹配、**PKCE 对所有客户端强制**、`client_type: public` 免 secret |

### 目标架构

```
认证权威从 web 移到 api —— api 成为 token 的唯一签发方

              ┌─ web (SSR)  拿 id_token ─┐
Akasha OIDC ──┤                          ├─→ POST /auth/sso {idToken}
              └─ iOS (native flow) ──────┘        │
                                                   ↓ api 自己验 Akasha JWKS
                                          {accessToken(1h), refreshToken, expiresIn}
                                                   │
                            web: httpOnly cookie ──┴── iOS: Keychain
```

- **token 格式统一，存储位置各按平台最佳实践**：web 存 httpOnly cookie（SSR 要在服务端
  读到它做登录墙，且 XSS 偷不走）、iOS 存 Keychain。
  v3 前端用 localStorage 是因为它是静态 SPA，melete 是 SSR —— 这一处不照抄，其余全按 v3
- **`X-Melete-Service-Token` 必须去掉**：iOS 反编译即得共享密钥，且 API 公网暴露后
  它防不住来自公网的攻击者。登录端点改用速率限制保护
- **hostname 增加 `melete-api.bukahou.com`**（与 geass-api 同形态，CF 配置见 §2.5）

### 端点契约

```
POST /auth/password  {username, password}  → {accessToken, refreshToken, expiresIn}
POST /auth/sso       {idToken}             → 同上（api 验签 Akasha JWKS 后取 sub）
POST /auth/refresh   {refreshToken}        → 同上（查 account_session 表）
POST /auth/logout    {refreshToken}        → 204（is_valid = 0）
```

新增表 `account_session`（对齐 v3 的 user_sessions，字段裁剪到实际需要的）。

### 实施完成（2026-09-02）

已按上述设计实装并逐项实测：

| 机制 | 实现 | 验证 |
|---|---|---|
| access token | HS256 / TTL 1h / 不落库，显式限定算法 | `alg=none` 伪造 → 401 |
| refresh token | 不透明随机串，**只存 SHA-256** | 库被读走也无法直接冒用 |
| **refresh 轮换** | 用一次即换新，同一行 UPDATE 带 `is_valid=1` 条件 | 旧 refresh 复用 → 401 |
| id_token 验签 | API 自己验（go-oidc + Akasha JWKS + issuer + audience） | 不再接受客户端自称的 sub |
| 登录限流 | 固定窗口按 IP，默认 10 次/分钟 | 公网唯一可无限试探的入口 |
| web 自动续期 | proxy 见 access 缺失 → 用 refresh 换新 → 重定向回原地址 | access 丢失后访问首页仍 200，用户无感 |

两处实现上值得记的细节：

- **轮换用 `UPDATE ... WHERE id=? AND is_valid=1` 并检查影响行数**：并发刷新时只有一方
  能改到该行，另一方拿到 0 行即视为失效 —— 避免同时存在两个有效 refresh
- **proxy 续期后必须 `redirect` 而非 `next()`**：新 cookie 只在响应头上，
  直接放行的话本次 SSR 渲染仍读不到 token

iOS 端接入时不需要动后端：注册一个 `client_type: public` 的 Akasha client
（自定义 scheme 回调 + PKCE，Akasha 已支持），走完 native flow 后调同一个 `/auth/sso`。

---

## 1. 与 geass-v3 的三个关键差异

Melete 整体照抄 geass-v3 的 CI/CD 模式（GitHub Actions 构建 → 回写 config 仓 → ArgoCD 同步），
但有三处**必须不同**，也是本设计的核心：

### ① ~~melete-api 绝不对公网暴露~~ —— **已于 2026-09-02 推翻，见 §0**

> 以下论证在「唯一客户端是自己的 SSR」前提下成立，iOS 端的加入使前提失效。
> 保留原文是因为它解释了 BFF 模式的价值，也解释了改造要补上哪些防护
> （原本靠网络隔离挡住的攻击面，现在必须靠 API 自己的认证挡住）。

geass-v3 的前端是**静态站**，浏览器直连 `geass-api.bukahou.com`，所以 API 必须有公网入口。

Melete 的 web 是 **SSR + BFF**：浏览器只跟 web 说话，web 在服务端调 api。这带来一个
安全前提——**API 信任 `X-Melete-Account` 头**（身份由 web 层的会话校验保证，API 只做传递）。

```
浏览器 ──HTTPS──> melete-web (SSR/BFF)  ──ClusterIP──> melete-api ──> TiDB
         会话 cookie          校验会话、注入账号头        无认证
```

#### 澄清：OIDC 回调不需要后端域名（2026-09-02）

一个容易误判的点 —— 「Akasha 要重定向，所以后端必须有域名」**不成立**：

```
geass:  redirect_uri → geass-api.bukahou.com/api/auth/oidc/callback  （后端）
melete: redirect_uri → melete.bukahou.com/auth/callback              （前端 SSR）
```

geass 的回调必须走后端，是因为**它的前端是静态站、没有服务端**。
Melete 的 web 是 SSR，`/auth/callback` 就是个 Route Handler，
它自己在服务端换 token、验 id_token、签会话。后端全程不露面。

> **因此：绝不能给 melete-api 建 HTTPRoute 或 Ingress。**
> 一旦它可从公网访问，任何人伪造 `X-Melete-Account: 1` 就能读写他人的学习记录。
> 这条约束要写进清单注释，防止将来「顺手加个域名方便调试」。

副作用是**只需要一个 hostname**，比 geass-v3 少一个；**也不需要 CORS** ——
跨域请求根本不会发生。后端已移除 CORS 中间件与 `MELETE_CORS_ORIGINS` 配置项。

#### 但网络隔离不是唯一防线：api 已加双层认证（2026-09-02）

集群里还跑着 geass-v3 / akasha / atlhyper —— **任一 pod 被攻破即可触达 melete-api**。
所以内网不作为可信边界，api 自己认证：

| 层 | 凭证 | 覆盖 | 作用 |
|---|---|---|---|
| 1 | `X-Melete-Service-Token` | 全部端点 | 证明调用方是 melete-web 本身 |
| 2 | `Authorization: Bearer <会话 JWT>` | 业务端点 | 证明代表哪个用户；**accountId 取自 JWT 的 sub** |

关键变化：**account id 不再由 web 自报**。原来是 web 说「我代表 1 号」api 就信
（`X-Melete-Account` 头）；现在 api 用 `MELETE_SESSION_SECRET` 独立验签，
web 无从冒充他人，绕过 web 直连更伪造不出。该请求头已从契约与代码中彻底移除。

实现要点：
- 验签**显式限定 HS256**（`jwt.WithValidMethods`）—— 否则可用 `alg: none` 绕过验签
- 服务密钥用 constant-time 比较，避免时序侧信道
- 登录端点（`/auth/*`）豁免第 2 层：登录前本就没有会话，但仍需第 1 层
- 路由只挂载一次 + 中间件内路径豁免 —— 向同一 chi 路由器注册两遍相同 pattern 会 panic

实测攻击面（全部 401）：伪造账号头 · `alg=none` 伪 JWT · 篡改 sha 的 JWT ·
只有会话无服务密钥 · 只有服务密钥无会话。

#### 这条约束已落成代码机制，不只是注释（2026-09-02）

光靠注释约束不住人。前端的 API 客户端做了物理隔离：

| 模块 | 内容 | 谁能 import |
|---|---|---|
| `web/src/lib/api.ts` | 内网地址 + fetch，首行 `import "server-only"` | 仅服务端组件 / route handler |
| `web/src/lib/claims.ts` | 类型 + 纯展示函数 | 服务端与客户端都可以 |

客户端组件误 import `api.ts` → **构建期直接失败**（已实测验证）。
同时移除了 `next.config.ts` 里的 `env:` 声明 —— 那个机制会把 `MELETE_API_BASE`
内联进浏览器 bundle，生产环境等于把内网 ClusterIP 地址公开出去。

> 之前它没泄漏，只是因为 tree-shaking 恰好把它摇掉了 ——
> **依赖打包器的优化行为来保证安全，不是保证**。现在是结构上不可能。

### ② 题库内容不在镜像里，需要单独导入

geass-v3 的数据是用户产生的业务数据，天然在库里。Melete 的题库内容来自离线管道，
**镜像里没有题目**，空库部署出来是个空壳。

所以部署流程比 geass-v3 多一步：**建表 + 导入 enriched.json**。
好在 `load.py` 是幂等的，且中间产物 JSON 才是 source of truth（见 data-pipeline.md），
所以这一步可以随时重跑，不需要数据库迁移。

### ③ 单体，不是微服务

geass-v3 有 6 个 Go 服务要 path filter 分别构建。Melete 只有 2 个镜像
（`melete-api` / `melete-web`），path filter 只需两条。

---

## 2. CI 流水线（GitHub Actions）

```
push main
  │
  ├─ path filter ─┬─ backend/** ────> build melete-api  ─┐
  │               └─ web/**     ────> build melete-web  ─┤
  │                                                       │
  │                            buildx → DockerHub bukahou/melete-{api,web}
  │                            tag: v1.0.0-<sha7>（不可变）+ latest
  │                                                       │
  └─────────────────────────────────> 回写 config 仓 <────┘
                                       kustomize edit set image
                                       source-{api,web}.json（审计溯源）
```

照抄 geass-v3 的三个已被踩过坑的细节：

- **`paths-ignore` 排除 `**/*.md` 放在 `on.push`，绝不写进 path filter**
  （geass-v3 实测：filter 里的 `!**/*.md` 会让每个 filter 都命中，全量重建）
- **tag 用不可变的 `v1.0.0-<sha7>`**，`latest` 只作附带
- **回写 config 仓用 5 次 retry**（两个 job 并发 push 会冲突）

### 前端的 build-time 变量

`NEXT_PUBLIC_*` 是构建期烙进 bundle 的，运行时改 env 无效。但 Melete 的前端
**没有 `NEXT_PUBLIC_*`** —— API 地址只在服务端用（`MELETE_API_BASE`），
浏览器只请求同源的 `/api/*`。所以前端镜像是环境无关的，同一个镜像可跑本地和生产。

---

## 2.5 Cloudflare 与入口链路

### 链路全貌（调研 2026-09-02 的实际状态）

```
浏览器 / iOS
   │  https://melete.bukahou.com  |  https://melete-api.bukahou.com
   ▼
Cloudflare（Proxied 🟠）
   │  DNS: *.bukahou.com CNAME → <TUNNEL_UUID>.cfargotunnel.com
   ▼
cloudflared（集群内 2 副本，outbound-only，无入站端口）
   │  转发到 public-ingress 的 ClusterIP
   ▼
Cilium Gateway API（ingress ns 的共享 Gateway）
   │  按 hostname 分流
   ▼
HTTPRoute（各业务 ns 跨 ns parentRef）
```

**关键发现：wildcard 已覆盖，DNS 不建也能通。** 现有的 `geass.bukahou.com`、
`geass-api.bukahou.com` 就没有显式 DNS 记录，全靠 `*.bukahou.com` 兜底。
但按 config 仓 CLAUDE.md 的规矩，**业务正常上线必须显式建 DNS** ——
wildcard 只是兜底，显式记录才是「这个域名有主」的声明。

### 要建的两条 DNS

```bash
# 与既有记录同形态：CNAME → tunnel UUID，Proxied（橙云）
flarectl dns create --zone bukahou.com --name melete   --type CNAME --content <TUNNEL_UUID>.cfargotunnel.com --proxy
flarectl dns create --zone bukahou.com --name melete-api   --type CNAME --content <TUNNEL_UUID>.cfargotunnel.com --proxy
```

> `flarectl` 本机未安装（`CF_API_TOKEN` 在 config 仓的 `infra.env` 里有）。
> 装不上时可直接调 CF API，zone id 见 config 仓 CLAUDE.md。

### cloudflared 不需要改

它用 `--token` 运行（远程管理模式），ingress 规则在 Cloudflare 侧，
且已配置为接收整个 `*.bukahou.com` 转发到 `public-ingress`。
**新增 hostname 不需要动 cloudflared，也不需要重启它** ——
分流完全由集群内的 HTTPRoute 决定。

### API 走 CF 的两个风险（已核实，结论是安全的）

API 对公网暴露后，它的流量要经过 CF，这带来两个 SSR 站点不会遇到的问题：

**① 个人化响应被 CDN 缓存 = 数据泄漏。** `/me/progress` 是 GET，若 CF 把
A 用户的响应缓存下来发给 B 用户，就是严重事故。

实测 `geass-api.bukahou.com` 的响应头是 `cf-cache-status: DYNAMIC` ——
CF 默认对未在缓存规则中命中的动态内容不缓存，且带 `Authorization` 头的请求
本就不进缓存。zone 内**无任何 Page Rule**，不存在误配的缓存规则。
geass-api 在同一套配置下已运行 94 天，可作旁证。

> 仍建议后端对 `/me/*` 显式返回 `Cache-Control: no-store` ——
> 不依赖「CF 默认行为恰好是对的」，而是自己声明意图。这是零成本的纵深防御。

**② Bot 防护可能挡住原生 App。** CF 的 Bot Fight Mode 会对无浏览器指纹的
请求做挑战，而 iOS App 的 URLSession 正是这种请求。当前 zone 的 bot_management
接口返回权限不足（免费版无该功能），且 geass-mobile 已在同一 zone 下工作 ——
**当前配置不会挡**。若将来开启了 Bot 防护，需要为 `melete-api.bukahou.com`
加一条跳过规则。

### 与 geass 的一处差异：不需要 CF Pages

`geassv3.bukahou.com` 指向 `geass-v3-web.pages.dev`（CF Pages 静态托管）。
Melete 的 web 是 SSR + 全站登录墙，**没有静态导出**，只有集群里的 SSR 镜像 ——
所以不涉及 Pages，两条 DNS 都指向 tunnel。

### Dockerfile（2026-09-02 完成并实测）

```
backend/deploy/docker/Dockerfile   melete-api   11.2 MB
web/Dockerfile                     melete-web    237 MB
```

**后端**照抄 geass-v3 的形态，其中最关键的一行是
`FROM --platform=$BUILDPLATFORM golang:...`：让编译在**构建机的原生架构**上跑、
再交叉编译到目标架构。不加这一行，buildx 会在 QEMU 模拟的 arm64 里跑 `go build`，
慢 5-10 倍 —— 双架构构建的绝大部分时间都会耗在那里。Go 的交叉编译是一等公民，
没有理由用模拟。运行时用 `distroless/static:nonroot`（无 shell、无包管理器、非 root），
它自带根证书，正好满足「访问 Akasha JWKS 验 id_token」的 HTTPS 需求。

**前端**与 geass-v3-web 有两处刻意的不同：

- **没有任何 `NEXT_PUBLIC_*` build-arg** —— API 地址只在服务端用（`server-only` 隔离），
  所以镜像是**环境无关**的：同一个镜像跑本地与生产，改 API 地址只需改 ConfigMap
  重启，不必重新构建。geass-v3 因为前端是静态站，地址烙在 bundle 里，改地址必须重建
- **以 `node` 非 root 用户运行**，且**不拷 `public/`** —— 本项目没有这个目录，
  照抄会构建失败

实测结论：两个镜像本地构建成功 → 起容器 → 前后端联调（登录墙 / 认证 / 页面渲染 /
1018 题数据）全部通过 → 后端双架构（amd64 + arm64）构建成功。

## 3. CD（ArgoCD）

```
config 仓 clusters/requiem/apps/melete/
  ├─ namespace.yaml
  ├─ config.yaml        ConfigMap（非敏感）+ Secret（敏感，明文，随 config 私有仓）
  ├─ api.yaml           Deployment + Service（ClusterIP，无 Ingress）
  ├─ web.yaml           Deployment + Service
  ├─ ingress.yaml       HTTPRoute，仅 web，跨 ns parentRef ingress/public-ingress
  ├─ kustomization.yaml image tag 锁定（CI 自动改）
  └─ source-{api,web}.json
```

ArgoCD Application 放 `config/clusters/local_init/k0s/addons/argocd/applications/melete.yaml`，
与 akasha / geass-v3 并列。

---

## 4. 配置与凭证

| 变量 | 类型 | 生产值来源 |
|---|---|---|
| `MELETE_DB_DSN` | 凭证 | TiDB Cloud，K8s Secret |
| `MELETE_OIDC_CLIENT_SECRET` | 凭证 | Akasha clients 表已注册，K8s Secret |
| `MELETE_SESSION_SECRET` | 凭证 | 新生成，K8s Secret。**web 与 api 必须一致** —— web 签、api 验 |
| `MELETE_SERVICE_TOKEN` | 凭证 | 新生成，K8s Secret。**web 与 api 必须一致** —— 服务间认证 |
| `MELETE_OIDC_ISSUER` | 非敏感 | `https://akasha.bukahou.com`，ConfigMap |
| `MELETE_WEB_ORIGIN` | 非敏感 | `https://melete.bukahou.com`，ConfigMap |
| `MELETE_API_BASE` | 非敏感 | `http://melete-api:8080/api/v1`（集群内），ConfigMap |
| `MELETE_ADDR` | 非敏感 | `:8080`，ConfigMap |

> 两份共享密钥要**同时注入 web 与 api 两个 Deployment**，用同一个 K8s Secret 即可。
> 不一致的表现是登录返回 502「认证服务配置错误」（已刻意与「密码错误」区分开 ——
> 否则配置故障会伪装成用户输入错误，极难排查）。

Secret 明文存 config 私有仓（与 akasha 同实践 —— 仓库私有是前提）。

**⚠️ 部署前必须做的一件事**：Akasha 的 `melete` client 目前只注册了 localhost 回调，
上生产要把 `https://melete.bukahou.com/auth/callback` 加进 `redirect_uris`、
把 `https://melete.bukahou.com/` 加进 `post_logout_redirect_uris`
（**追加而非替换** —— 保留 localhost 才能继续本地开发）。

---

## 5. 数据库与内容导入

```
① 建表    mysql < db/schema.sql        对 TiDB 跑一次（tracker 的待办，正好在这里完成）
② 导内容  load.py --dsn <TiDB DSN>     幂等，富化推进后可反复重跑
```

**这一步不进 CI**，理由：内容更新频率极低（富化完成后基本不变），且导入要连生产库、
耗时随题量增长；做成 CI 步骤等于每次 push 代码都碰生产数据。
起步用本地手动跑，将来若需要自动化，做成 ArgoCD PreSync Hook 的 K8s Job。

---

## 6. 已定决策（2026-09-02，用户拍板）

| 决策 | 结论 | 理由 |
|---|---|---|
| **hostname** | `melete.bukahou.com` | 与仓库同名；只需一个域名（API 不对公网暴露） |
| **镜像架构** | **amd64 + arm64 双架构** | 与 geass-v3 实践一致，全集群可调度、不需要 nodeSelector 绑定。代价是 arm64 走 QEMU 模拟、CI 更慢，接受 |
| **CD 同步** | **起步 manual，跑顺再放开** | 与 akasha 一致：CI 改完 tag 后 ArgoCD 只标 OutOfSync，人工看 diff 再点 SYNC。新应用初期清单容易有错，自动 sync 会把错误直接推上去。稳定后按 geass-v3 的演进路径加 `automated.selfHeal` |

### CI 耗时的预期与后手

双架构下 Next.js 镜像的 arm64 构建（QEMU 模拟）是主要耗时项。若 private 仓的
Actions 额度成为问题，按以下顺序处理，**不要先牺牲架构覆盖**：

1. `cache-from/to: type=gha`（已在 geass-v3 模式里，第一次之后大幅加速）
2. web 与 api 的 path filter 确保只重建改动的那个
3. 真不够用时才考虑 web 镜像降为 amd64-only + nodeSelector

## 7. 实施顺序

```
1. 定 hostname、拿 TiDB 建表验证（同时清掉 tracker 的老待办）
2. 写两个 Dockerfile + 本地 buildx 验证能起来
3. 写 config 仓部署清单 + ArgoCD Application
4. 更新 Akasha client 的生产回调地址
5. 手动导一次内容 → 手动 apply 一次 → 确认能访问
6. 最后才接 GitHub Actions（CI 是加速器，不是前置条件）
```

⚠️ **顺序刻意把 CI 放最后**：先用手动把「镜像能跑起来、清单是对的、域名通了」验证完，
再自动化。反过来做的话，CI 失败时分不清是流水线问题还是应用本身的问题。

## 8. 被否决的方案（留档）

| 方案 | 否决理由 |
|---|---|
| 给 melete-api 配公网 hostname | API 信任 `X-Melete-Account` 头，暴露即越权漏洞。见 §1① |
| 前端走 CF Pages 静态导出（同 geassv3） | Melete 全站登录墙 + 个人化数据，必须 SSR |
| 内容导入做成 CI 步骤 | 每次 push 代码都碰生产数据；内容更新频率与代码完全不同步 |
| 用 AtlHyper Deployer 做 CD | 集群实际在跑的是 ArgoCD（akasha/geass-v3 都归它管），跟随现状 |
