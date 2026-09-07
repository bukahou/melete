package localauthx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bukahou/gokit/localauth"
)

// logMailer 把验证码打进日志。
//
// ⛔⛔ 仅供开发。⚠️ 此通道不适用于生产：验证码不经安全信道投递 ——
// 而找回密码的全部安全性就建立在「只有邮箱主人看得到那个码」上。
//
// 用户 2026-09-06 裁决 ③ 定了三条闸门，⛔ 缺一不可：
//  1. 只在 MELETE_MAILER=log 时构造 —— 显式选择，⛔ 无默认值
//  2. 构造时打一条 WARN 级启动日志（部署者不读代码，但一定看启动日志）
//  3. ⭐ MELETE_MAILER 未配置时【启动失败】，⛔ 不静默降级
//     —— 「忘了配」与「故意用 log」必须区分得开，而静默降级会让生产
//     在无人察觉下用上 ①。⚠️ 这一条是三条里最重要、也最容易在实现时
//     被"顺手"改成默认值的那条。
type logMailer struct{ log *slog.Logger }

var _ localauth.MessageSender = (*logMailer)(nil)

// ErrMailerNotConfigured 是闸门 ③ 的载体。
//
// ⚠️ 它必须在【启动时】炸，⛔ 不能等到第一次发码 —— 那时用户已经在等邮件了，
// 而错误会以「发送失败」的形式出现，看起来像临时故障。
var ErrMailerNotConfigured = errors.New(
	"MELETE_MAILER 未配置：凭证类配置无默认值是有意为之。开发用 log（⛔ 验证码会明文进日志），生产必须配真实通道")

// NewMailer 按配置选一个发信通道。
func NewMailer(kind string, log *slog.Logger) (localauth.MessageSender, error) {
	switch kind {
	case "log":
		log.Warn("⛔ 发信通道 = log：验证码不经安全信道投递，"+
			"不适用于生产。",
			"mailer", "log")
		return &logMailer{log: log}, nil
	case "":
		return nil, ErrMailerNotConfigured
	default:
		// ⚠️ 未知取值也是失败，⛔ 不回退到 log ——
		// 打错一个字母不该悄悄把生产降级成明文日志。
		return nil, fmt.Errorf("MELETE_MAILER=%q 不是已知的发信通道（当前支持：log）", kind)
	}
}

func (m *logMailer) SendVerification(_ context.Context, msg localauth.VerificationMessage) error {
	if msg.Notice != "" {
		// 通知类消息不含验证码，登记即可。
		m.log.Info("【开发用发信】通知", "to", msg.To, "notice", msg.Notice, "new_address", msg.NewAddress)
		return nil
	}
	m.log.Warn("【开发用发信】验证码（⛔ 明文，生产禁用）",
		"to", msg.To, "purpose", msg.Purpose, "code", msg.Code)
	return nil
}
