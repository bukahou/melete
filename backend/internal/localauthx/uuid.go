// Package localauthx 是 melete 与共享认证模块之间的【适配层】。
//
// 它的唯一职责是把模块定义的接口翻译成 melete 的存储，⛔ 不含任何决策 ——
// 退避策略、轮换语义、吊销时机全在模块里，这里只负责读写。
//
// ⚠️ 为什么不叫 `auth`：那个名字会诱使后人把业务逻辑塞回来。
// 名字里的 x = adapter，读到它的人应该立刻知道「这里没有逻辑」。
package localauthx

import (
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

// NewUserID 生成一个新的账号 id。
//
// ⭐ 用 v7 而不是 v4：v7 的高位是毫秒时间戳，因此**索引上是时间有序的**，
// 插入总在 B+ 树右端，避免 v4 的随机插入造成页分裂与索引膨胀。
// 用户 2026-09-05 选 UUID 的直接理由是「TiDB 跳号严重」——
// ⚠️ 而在 TiDB 上，v4 的随机性还会额外造成写热点分散过度、
// v7 的单调性正好与它的 range 分片对齐。
func NewUserID() (string, error) {
	u, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 只在熵源失败时报错，与 bcrypt 的 dummy 生成同源：
		// 那是进程级不可用，⛔ 不该被当成一次普通的请求失败吞掉。
		return "", fmt.Errorf("生成账号 id: %w", err)
	}
	return u.String(), nil
}

// encodeID 文本 → BINARY(16)，写库前用。
//
// ⚠️ 严格解析：uuid.Parse 接受多种宽松写法（带花括号、带 urn: 前缀、无连字符），
// 这里【不做归一化后放行】，而是要求调用方给的就是 canonical 形式 ——
// 因为宽松解析意味着同一个 id 有多种文本形态，而那正是上面警告的失败模式。
func encodeID(s string) ([]byte, error) {
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

// decodeID BINARY(16) → 文本，读库后用。
func decodeID(b []byte) (string, error) {
	if len(b) != 16 {
		// ⚠️ 长度不对多半意味着列类型被改过（或查错了列）。
		// 报出实际长度，⛔ 别静默截断 —— 截断会产生一个「看起来像 id 的 id」。
		return "", fmt.Errorf("账号 id 应为 16 字节，实得 %d 字节", len(b))
	}
	var u uuid.UUID
	copy(u[:], b)
	return u.String(), nil
}
