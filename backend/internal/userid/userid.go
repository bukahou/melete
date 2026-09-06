// Package userid 是账号标识的唯一定义处：生成、编解码、以及可直接
// 当 SQL 参数用的类型。
//
// ⚠️ 为什么它【不在】 internal/localauthx 里（2026-09-07 从那里搬出来）：
// 账号 id 不是认证适配层的东西 —— study / question / httpapi 都要用它，
// 而它们与共享认证模块毫无关系。把 id 原语留在 localauthx 会逼着
// 业务包 import 一个认证适配器，分层就歪了。
//
// ⭐ 搬出来还有一个更硬的收益：`UserID` 这个类型可以进业务包的函数签名，
// 于是「把一个普通 string 当账号 id 传进 SQL」变成【编译错误】，
// 而不是一次静默的空结果。见下方 UserID 的注释。
package userid

import (
	"database/sql/driver"
	"fmt"

	"github.com/google/uuid"
)

// ⭐ 账号 id 的两种形态，以及为什么必须【只有这一个文件】做转换。
//
// 存储形态  users.id BINARY(16)          —— §22 定稿，UUIDv7
// 内存形态  string，canonical UUID 文本   —— "01936d4e-...-8a3f"
//
// 内存形态选 canonical 文本而不是 hex / base64，三个理由：
//   · 它要进 access token 的 sub —— 文本形态必须可读、可 grep
//   · 它要进日志与审计事件 —— 模块的 AuditEvent 里 userID 是 string
//   · 模块【从不解释】它（SessionRecord.UserID 的注释明说不假设宿主的 id 形态），
//     所以唯一的约束是：CredentialStore / SessionStore / AccountCreator
//     三处拿到的必须是同一个串
//
// ⛔⛔ 编解码只允许存在于本文件。
//
// ⚠️ 这不是洁癖 —— 它是这个方案唯一的失败模式。散落的转换里只要有一处
// 用了不同的字面形式（大写 hex、去掉连字符、base64），它产生的 string
// 与别处的就不再相等，而**类型系统完全看不出来**：两边都是 string，
// 编译通过，运行时表现为「查不到这个用户」而不是报错。
//
// 守门用这条命令，它必须为 0 行：
//
//	git grep 'uuid\.Parse\|uuid\.UUID\|\[16\]byte' -- backend/ | grep -v localauthx/uuid.go
//
// ⚠️ 并且【不做 int64 ↔ string 双向映射】。那会留下一个必须永远保持同步的
// 翻译层，而翻译层是 bug 的产地。melete 的 int64 账号 id 在阶段 2 直接消失，
// ⛔ 不保留兼容路径 —— 保留兼容路径等于让两种 id 长期并存，
// 而「两个都对但不一样」比「只有一个」难查得多。

// New 生成一个新的账号 id。
//
// ⭐ 用 v7 而不是 v4：v7 的高位是毫秒时间戳，因此**索引上是时间有序的**，
// 插入总在 B+ 树右端，避免 v4 的随机插入造成页分裂与索引膨胀。
// 用户 2026-09-05 选 UUID 的直接理由是「TiDB 跳号严重」——
// ⚠️ 而在 TiDB 上，v4 的随机性还会额外造成写热点分散过度、
// v7 的单调性正好与它的 range 分片对齐。
func New() (string, error) {
	u, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 只在熵源失败时报错，与 bcrypt 的 dummy 生成同源：
		// 那是进程级不可用，⛔ 不该被当成一次普通的请求失败吞掉。
		return "", fmt.Errorf("生成账号 id: %w", err)
	}
	return u.String(), nil
}

// Encode 文本 → BINARY(16)，写库前用。
//
// ⭐ 导出（原本是包内私有）：internal/account 等包也要读写 users.id，
// 而「编解码只有一处」这条规则要求它们【调这个函数】而不是各自实现。
// ⚠️ 守门测试因此仍然成立 —— 它查的是谁 import 了 github.com/google/uuid，
// 而调用方只 import localauthx，⛔ 碰不到 uuid 包。
// ⇒ 导出反而让规则更硬：以前别的包想转换只能自己写（守门会红），
//
//	现在有一条合法的路，⛔ 没有理由再自己写。
//
// ⚠️ 严格解析：uuid.Parse 接受多种宽松写法（带花括号、带 urn: 前缀、无连字符），
// 这里【不做归一化后放行】，而是要求调用方给的就是 canonical 形式 ——
// 因为宽松解析意味着同一个 id 有多种文本形态，而那正是上面警告的失败模式。
func Encode(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("账号 id %q 不是合法 UUID: %w", s, err)
	}
	if u.String() != s {
		return nil, fmt.Errorf("账号 id %q 不是 canonical 形式（应为 %q）", s, u.String())
	}
	b := u[:]
	out := make([]byte, 16)
	copy(out, b)
	return out, nil
}

// Decode BINARY(16) → 文本，读库后用。
func Decode(b []byte) (string, error) {
	if len(b) != 16 {
		// ⚠️ 长度不对多半意味着列类型被改过（或查错了列）。
		// 报出实际长度，⛔ 别静默截断 —— 截断会产生一个「看起来像 id 的 id」。
		return "", fmt.Errorf("账号 id 应为 16 字节，实得 %d 字节", len(b))
	}
	var u uuid.UUID
	copy(u[:], b)
	return u.String(), nil
}

// UserID 让 canonical UUID 文本可以直接当 SQL 参数用。
//
// ⭐ 存在理由：业务代码里账号 id 是 string（进 JWT、进日志、进 API），
// 而库里是 BINARY(16)。若每个查询点各自调一次 Encode，就会散出几十处
// 「转换 + 处理转换错误」的样板 —— 而样板正是不一致的温床。
//
// ⚠️ 它没有绕开「编解码只有一处」：转换仍然发生在下面这个 Value() 里，
// 而 Value() 调的是本文件的 Encode。⛔ 别的包拿到的是一个可以直接
// 塞进 SQL 参数的值，碰不到编码细节。
//
// ⚠️ 转换失败会以【查询错误】的形式冒出来，而不是编译错误。这是可接受的，
// 因为 id 的形态在入口就被 token.ParseAccessToken 校验过了 ——
// 到这一层还不是合法 UUID，说明有一条路径绕过了入口校验，
// ⭐ 那种情况本来就该整个查询失败，而不是悄悄查出空集。
type UserID string

func (u UserID) Value() (driver.Value, error) { return Encode(string(u)) }
