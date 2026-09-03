// Package oidcrp 是通用的 OpenID Connect Relying Party —— 把"跟任意 OIDC
// provider 走一遍登录"这件事完整封起来。
//
// # 它【故意】不做的事
//
// 这个包不知道 geass 的存在: 不碰数据库、不建账号、不签 geass 的 token、
// 不认识 users 表。它只负责把一个人送去 provider, 再把验证过的身份交回来。
//
//	oidcrp 负责          →  跑完协议, 产出一个可信的 Identity
//	调用方负责 (回调)     →  这个 Identity 对应我系统里的谁, 以及接下来怎么办
//
// 边界画在这里, 是因为前者对任何应用都一样, 后者对任何应用都不一样。
//
// # 为什么一开始就写成"外部包"
//
// 下游有三个 (geass / atlhyper / atlantis), 它们迟早都要接同一个中枢。
// 本包因此从第一天起就不引用任何 geass 内部类型 —— 第二个消费者出现时,
// 直接 git mv 成独立仓, 改 import 路径即可, 不需要重写。
//
// 这与 pkg/gormdb 的来历相反: 那个是复制三次之后才上提的。这个是【已知会有
// 第二个消费者】, 所以提前把边界画好 —— 抽象的成本在写第一遍时最低。
//
// # 与 akasha 的关系
//
// akasha (github.com/bukahou/akasha) 是本项目的身份中枢, 但本包【不为它定制】。
// 它按标准 OIDC discovery 工作, 换成 Keycloak / Auth0 / Google 同样能跑 ——
// 这不是过度设计: atlhyper 是要给别人部署的产品, 别人填的就不会是 akasha。
//
// # 安全上不可动摇的三条
//
//  1. state / nonce / PKCE verifier 三者必须存在【同一个签名 cookie】里。
//     verifier 的全部价值就是"只有发起这次流程的人拿得出它" —— 放进 URL 或
//     任何不受保护的地方, 截获 code 的人就能一并拿到, PKCE 直接归零。
//  2. 回跳目标必须过白名单。放行任意 next 就是开放重定向:
//     用户在可信域名上正常登录, 却被送去钓鱼站。
//  3. nonce 必须在拿到 id_token 后【逐字比对】。provider 只负责回显它,
//     校验是 RP 自己的责任 —— 不比对等于这个字段不存在。
package oidcrp
