# 题库权限：三档用户 + 公开 / 私有题库

> 状态：active · 2026-10-08 设计定稿（待用户审阅后实施）
> 决策过程见 `docs/tasks/active/tracker.md` 的 P9 裁决 #27–#30。

## 1. 要达到的效果

用户分三档：

| 档位 | 谁 | 能看的题库 | 怎么变 |
|---|---|---|---|
| **普通** | 任何人用 Akasha 注册即是 | 公开题库 | admin 可以把他**升级**为高级 |
| **高级** | 由 admin 升级 | 公开 + **全部**私有题库 | admin 可以把他**降级**为普通 |
| **admin** | 由超级用户授予 | 全部 | 只有超级用户能改 |

- 「私有」的意思是**普通用户看不到**，⛔ 不是谁都看不到。
- 没有按题库的名单：高级用户看得到**所有**私有题库。新增一个私有题库，所有高级用户和 admin 立刻都能看到。
- 降级后看不到私有题库，但**做过的题的记录一条不删**；再升级回来，记录原样回来。
- 登录仍然是必须的：没有「游客」，公开题库也要登录后才能看。

举例：陌生人从 App Store 下载、用 Akasha 注册 → 普通用户，只看到 IPA。
admin 在 web 上把他升级 → 他的 iPhone 下次打开就多出 SAA、SAP。

## 2. 超级用户与两条硬规则

**超级用户 ⛔ 不是应用里的账号，就是直接操作数据库的人。** 它负责授予 / 撤销 admin。

1. **应用里（web、iOS、API）没有任何入口能授予或撤销 admin。** admin 身份一旦设定，只有超级用户能改。
2. **admin 不能操作任何 admin 账号，包括自己。** admin 只能升级 / 降级普通与高级用户。

> 为什么超级用户不做成可登录的账号：最高权限的操作不在应用里开口子，就不存在一个需要防爆破、需要限流的登录入口。
> 授予 admin 是几个月一次的事，走数据库完全够用。以后需要再加应用内入口。

## 3. 数据模型

沿用与 geass 相同的账号模板：`users` 无角色列，档位就是 `user_permissions` 里的权限行（白名单常量，格式 `<资源>:<动作>`）。

| 档位 | `user_permissions` 里有 |
|---|---|
| 普通 | 什么都没有（空集是正常状态） |
| 高级 | `content:private` —— 可以看私有题库 |
| admin | `user:manage` —— 可以升降级普通 / 高级用户，并能看全部 |

- 一个账号只按「最高那一档」算：同时有 `user:manage` 和 `content:private` 也只是 admin。
- 升级 = 写一行 `content:private`；降级 = 删掉这一行。过程写进应用日志（哪个 admin、对谁、升还是降）。
- `user_permissions` 新增 `granted_by` 列（geass 有，melete 当初没建），记录是哪个 admin 升的级；超级用户直接写库时留空。

题库新增一列：

```sql
ALTER TABLE bank ADD COLUMN visibility VARCHAR(16) NOT NULL DEFAULT 'private';  -- public | private
ALTER TABLE user_permissions ADD COLUMN granted_by BINARY(16) NULL;
```

- `visibility` 缺省是 **private**：新题库忘了写标记时，宁可普通用户看不到，⛔ 不能默认公开。
- 遵守 TiDB 纪律：不建外键，关系约束放在应用层。

## 4. 「能不能看」只有一个入口

```
能看(用户, 题库) = 题库是公开的 ∨ 用户有 content:private ∨ 用户有 user:manage
```

- 后端新增一个包 `access`，所有和题库有关的地方**只经过它**：
  - `VisibleBankIDs(ctx, userID) → []bankID`：用来过滤列表、汇总统计
  - `CanSeeBank(ctx, userID, bankID) → bool`：用来检查单个对象
- **看不到 = 不存在**：一律返回 404，⛔ 不返回 403。普通用户连「有这个题库」都无从得知。
- **查询出错时当作看不到**（fail-closed）。少显示的代价远小于越权。
- ⛔ 不在进程内缓存权限：升降级要**立刻**生效。每次请求查一次库（量很小）。
- 权限 ⛔ 不放进登录令牌：否则降级要等令牌过期才生效。

### 4.1 要经过检查的地方

| 类别 | 接口 | 题库从哪来 |
|---|---|---|
| 列表 | `GET /banks` | 按 `VisibleBankIDs` 过滤 |
| 按题库名 | `/banks/{slug}`、`/tags`、`/questions`、`/questions/summary`、`/terms` | 路径里的 slug |
| 按对象编号 | `GET /questions/{id}`、`GET /terms/{id}`、`POST /attempts`、`PATCH /attempts/{id}`、`PUT/DELETE /me/bookmarks/{questionId}` | 编号 → 所属题库。⚠️ 路径里没有题库名，最容易漏，必须逐个覆盖并有测试 |
| 我的数据 | `/me/progress`、`/me/tag-stats`、`/me/resume`（带或不带 `?bank=`） | 参数，或当前题库 |
| 当前题库 | `GET/PUT /me/bank` | PUT 只接受看得到的题库 |
| 跨题库汇总 | `/me/overview`、`/me/recent` | 只统计 / 列出看得到的题库 |

### 4.2 当前题库变得看不到时（被降级）

解析顺序不变，每一步都只考虑看得到的题库：
**自己选的** → **最近作答过的** → **第一个看得到的题库** → 无。

- ⛔ 不改写 `users.current_bank_id`：再升级回来后，自动回到原来选的那个。

## 5. 公开 / 私有的来源

在开发阶段定死，写在题库配置 `pipeline/banks/<slug>/enrich_spec.json`：

```json
{ "visibility": "public" }
```

- `load.py` 导入时写进 `bank.visibility`；配置里没写 ⇒ `private`。
- 要改就改配置、重新导入，改动留在 git 历史里。⛔ 应用里没有切换的地方。
- 初始值：IPA 公开；两个 AWS 题库私有。

## 6. admin 的管理页面（web）

- 路由 `/admin`。只有 admin 能进；侧栏入口只对 admin 显示。⚠️ 隐藏入口只是方便，**真正的门在后端**。
- 页面：分页列出用户，每行显示 Akasha 带来的显示名与邮箱（只用于辨认，⛔ 不用于认证）、当前档位、注册时间。
- 操作：普通用户行有「升级为高级」，高级用户行有「降级为普通」；admin 行**只显示档位，没有按钮**。

接口（都要求 `user:manage`，否则 404）：

| 接口 | 作用 |
|---|---|
| `GET /admin/users?page=` | 用户列表（含档位） |
| `PUT /admin/users/{userId}/tier` | 设为 `basic` 或 `advanced`（幂等） |

- 目标账号是 admin（持有 `user:manage` 或 `*`）⇒ 拒绝（409）：admin 不能操作 admin，包括自己。
- ⛔ 不接受 `admin` 作为目标档位：应用里没有授予 admin 的入口。

## 7. 上线

1. 迁移：`bank.visibility`（缺省 private）+ `user_permissions.granted_by`。
   旧镜像不读这两处，线上不受影响。
2. 超级用户写库（**先于新镜像**）：
   - 给指定的 Akasha 账号授予 `user:manage`；
   - 现有的作答用户按需授予 `content:private`（升为高级）。
3. 重新导入三个题库，写入 `visibility`（IPA 变为 public）。
4. 上新镜像。

按这个顺序，新镜像一上线，每个人看到的就是最终状态。

## 8. 明确不做

- 按题库的授权名单（高级用户一律看全部私有题库）。
- 应用内的超级用户账号与登录页（以后需要再加）。
- 在应用里切换公开 / 私有。
- 游客模式。
