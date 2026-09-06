package account

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

var ErrNotFound = errors.New("account not found")

// Repository 是账号数据的读写契约。
type Repository interface {
	FindByUsername(ctx context.Context, username string) (*Account, error)
	FindByID(ctx context.Context, id string) (*Account, error)
	// EstablishFederated 幂等确立联邦账号：按 (provider, subject) 找，找不到则建。
	EstablishFederated(ctx context.Context, provider, subject, display string) (*Account, error)
}

type mysqlRepository struct{ db *sqlx.DB }

func NewMySQLRepository(db *sqlx.DB) Repository { return &mysqlRepository{db: db} }

// userRow 是 users 表到 Account 的搬运层。
// ⚠️ id 在库里是 BINARY(16)，进 Go 才变成 canonical 文本。
type userRow struct {
	ID           []byte         `db:"id"`
	Username     string         `db:"username"`
	PasswordHash sql.NullString `db:"password_hash"`
	DisplayName  sql.NullString `db:"display_name"`
	Status       int            `db:"status"`
}

func (r userRow) toAccount() (*Account, error) {
	id, err := userid.Decode(r.ID)
	if err != nil {
		return nil, fmt.Errorf("账号 id: %w", err)
	}
	a := &Account{ID: id, Username: r.Username, Status: r.Status}
	if r.PasswordHash.Valid {
		a.PasswordHash = &r.PasswordHash.String
	}
	if r.DisplayName.Valid {
		a.Display = &r.DisplayName.String
	}
	return a, nil
}

const userCols = `id, username, password_hash, display_name, status`

func (r *mysqlRepository) FindByUsername(ctx context.Context, username string) (*Account, error) {
	var row userRow
	err := r.db.GetContext(ctx, &row,
		`SELECT `+userCols+` FROM users WHERE username = ? AND deleted_at IS NULL`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询账号 %q: %w", username, err)
	}
	return row.toAccount()
}

func (r *mysqlRepository) FindByID(ctx context.Context, id string) (*Account, error) {
	bin, err := userid.Encode(id)
	if err != nil {
		// ⚠️ id 形态不对不是「没找到」——它说明调用方拿到的 id 来路不明
		// （伪造的 token、旧格式的 sub）。⛔ 不静默成 ErrNotFound。
		return nil, fmt.Errorf("查询账号: %w", err)
	}
	var row userRow
	err = r.db.GetContext(ctx, &row,
		`SELECT `+userCols+` FROM users WHERE id = ? AND deleted_at IS NULL`, bin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询账号 %s: %w", id, err)
	}
	return row.toAccount()
}

// EstablishFederated 按 (provider, subject) 找账号，找不到就建。
//
// ⛔ 找不到时【直接建新账号】，⛔ 不去按邮箱找已有账号 ——
// 自动按邮箱认亲等于把安全责任委托给上游（形状照 geass-v3 的
// LoginWithIdentity，案卷裁决 ①(b) 指定）。
//
// ⚠️ 建 users 与建 identities 必须同一事务：两步同生共死，⛔ 不留孤儿 user 行。
func (r *mysqlRepository) EstablishFederated(ctx context.Context, provider, subject, display string) (*Account, error) {
	if a, err := r.findByIdentity(ctx, provider, subject); err == nil {
		return a, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	username, err := r.generateFederatedUsername(ctx, provider, display)
	if err != nil {
		return nil, err
	}
	id, err := userid.New()
	if err != nil {
		return nil, fmt.Errorf("生成账号 id: %w", err)
	}
	bin, err := userid.Encode(id)
	if err != nil {
		return nil, err
	}

	err = r.inTx(ctx, func(tx *sqlx.Tx) error {
		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO users (id, username, status, created_at, updated_at, display_name)
			VALUES (?, ?, ?, ?, ?, ?)`,
			bin, username, StatusActive, now, now, nullStr(display)); err != nil {
			return fmt.Errorf("建立联邦账号: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO identities (provider, subject, user_id, created_at)
			VALUES (?, ?, ?, ?)`, provider, subject, bin, now); err != nil {
			return fmt.Errorf("建立身份关联: %w", err)
		}
		return nil
	})
	if err != nil {
		// ⭐ 并发首登：另一个请求抢先建好了同一个上游身份，我们这笔撞唯一索引
		// 整体回滚。正确行为是【复用它建好的那个账号】而不是把 500 抛给用户 ——
		// 两个请求本来就在为同一个人建号，谁先谁后无所谓。
		//
		// ⚠️ ⛔ 不去匹配驱动特定的错误码（那随 MySQL/TiDB 版本漂移），
		// 而是直接重查：撞唯一索引的一方会被数据库阻塞到先来者提交为止，
		// 所以拿到错误的那一刻，赢家的事务（含 identities 行）已经提交完毕，
		// 重查必然命中。（形状照 geass-v3 federated_service.go:137）
		//
		// ⚠️ melete 旧实现靠 `INSERT ... ON DUPLICATE KEY UPDATE` 天然幂等，
		// 换成两表同事务之后【那个天然性没有了】，必须显式补这一段。
		if a, qerr := r.findByIdentity(ctx, provider, subject); qerr == nil {
			return a, nil
		}
		return nil, err
	}
	return &Account{ID: id, Username: username, Display: nullPtr(display), Status: StatusActive}, nil
}

func (r *mysqlRepository) findByIdentity(ctx context.Context, provider, subject string) (*Account, error) {
	var row userRow
	err := r.db.GetContext(ctx, &row, `
		SELECT u.id, u.username, u.password_hash, u.display_name, u.status
		  FROM identities i JOIN users u ON u.id = i.user_id
		 WHERE i.provider = ? AND i.subject = ? AND u.deleted_at IS NULL`, provider, subject)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("按上游身份查账号: %w", err)
	}
	return row.toAccount()
}

var usernameCleaner = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// generateFederatedUsername 给联邦账号造一个 username。
//
// ⚠️ users.username 是 NOT NULL（§22：「oidc 可能没邮箱，但 username 必须有」），
// 而上游给的 display 可能是中文 —— 清洗后会变成空串。
// ⇒ 兜底用 provider 前缀 + 随机后缀。⛔ 不用 subject 派生：
// subject 是 pairwise 的身份信息，不该出现在一个会被展示的字段里。
func (r *mysqlRepository) generateFederatedUsername(ctx context.Context, provider, display string) (string, error) {
	base := usernameCleaner.ReplaceAllString(display, "")
	if len(base) > 32 {
		base = base[:32]
	}
	if base == "" {
		base = provider + "_user"
	}
	if taken, err := r.usernameTaken(ctx, base); err != nil {
		return "", err
	} else if !taken {
		return base, nil
	}
	// ⚠️ 只试 3 次就放弃：撞 3 次说明要么基名极其常见、要么有人在刷，
	// ⛔ 无限重试会把一次登录变成一个可被拖住的循环。
	for range 3 {
		n, err := rand.Int(rand.Reader, big.NewInt(10000))
		if err != nil {
			return "", fmt.Errorf("生成用户名后缀: %w", err)
		}
		cand := fmt.Sprintf("%s_%04d", base, n.Int64())
		if taken, err := r.usernameTaken(ctx, cand); err != nil {
			return "", err
		} else if !taken {
			return cand, nil
		}
	}
	return "", errors.New("用户名生成冲突次数过多，请重试登录")
}

func (r *mysqlRepository) usernameTaken(ctx context.Context, name string) (bool, error) {
	var one int
	err := r.db.GetContext(ctx, &one, `SELECT 1 FROM users WHERE username = ?`, name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("查用户名是否占用: %w", err)
	}
	return true, nil
}

func (r *mysqlRepository) inTx(ctx context.Context, fn func(*sqlx.Tx) error) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务: %w", err)
	}
	return nil
}

func nullStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
