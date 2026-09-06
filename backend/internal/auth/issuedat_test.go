package auth

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ⛔⛔ access token 的 iat 必须【原样是调用方给的 issuedAt】，
// ⛔ 不得被 time.Now() 覆盖。
//
// ⚠️ 锁住的是 gokit v0.2.0 引入的一条宿主契约：改密流程里模块算出
// `reissueAt = nextSecond(changedAt)` 并要求重签 token 的 iat 正好是它 ——
// 吊销纪元设在 changedAt、判定为「iat <= changedAt 即失效」，
// 于是其它设备同一秒签发的 token 被作废，而重签这张靠 iat > changedAt 幸存。
//
// ⇒ 若签发侧强制覆盖 iat（很可能与 changedAt 同一秒），
//
//	刚重签出来的 token 会【立刻失效】，用户改完密码当场掉线。
//
// ⭐ geass-v3 正是撞在这上面（它的 jwt helper 把 iat 当保留 claim 强制覆盖），
// 因而接不了 v0.2.0。这条测试让同一个缺陷在 melete 上写不出来。
func TestAccessTokenHonoursIssuedAt(t *testing.T) {
	s := &Service{secret: []byte("test-secret")}

	// 刻意取一个【明显不是 now】的时刻 —— 若实现用了 time.Now()，差值会是小时级。
	want := time.Now().Add(-3 * time.Hour).Truncate(time.Second)

	tok, exp, err := s.IssueAccessToken(context.Background(), "00000000-0000-7000-8000-000000000001", "sess-1", want)
	if err != nil {
		t.Fatal(err)
	}

	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(tok, claims); err != nil {
		t.Fatal(err)
	}
	iat, ok := claims["iat"].(float64)
	if !ok {
		t.Fatalf("token 里没有 iat：%v", claims)
	}
	if int64(iat) != want.Unix() {
		t.Fatalf("🔴 iat = %d，期望 %d（差 %v）\n"+
			"⚠️ 签发侧把 issuedAt 覆盖掉了 —— 改密重签出来的 token 会立刻失效，"+
			"用户改完密码当场掉线。这正是 geass-v3 接不了 v0.2.0 的原因。",
			int64(iat), want.Unix(), time.Duration(want.Unix()-int64(iat))*time.Second)
	}

	// exp 也必须从 issuedAt 起算，⛔ 不是从 now 起算。
	if !exp.Equal(want.Add(AccessTTL)) {
		t.Fatalf("exp = %v，期望 %v —— 有效期没有从 issuedAt 起算", exp, want.Add(AccessTTL))
	}
	if e, ok := claims["exp"].(float64); !ok || int64(e) != want.Add(AccessTTL).Unix() {
		t.Fatalf("token 里的 exp = %v，期望 %d", claims["exp"], want.Add(AccessTTL).Unix())
	}

	// 判别力对照：换一个 issuedAt，iat 必须【跟着变】。
	// ⛔ 少了它，一个「iat 恒为某常量」的实现也会让上面全绿。
	other := want.Add(90 * time.Minute)
	tok2, _, err := s.IssueAccessToken(context.Background(), "00000000-0000-7000-8000-000000000001", "sess-1", other)
	if err != nil {
		t.Fatal(err)
	}
	c2 := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(tok2, c2); err != nil {
		t.Fatal(err)
	}
	if int64(c2["iat"].(float64)) != other.Unix() {
		t.Fatal("对照组：换了 issuedAt 而 iat 没跟着变 —— 本测试不具判别力")
	}
}
