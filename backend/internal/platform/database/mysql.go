// Package database 提供数据库连接。
//
// 纪律（见 CLAUDE.md「MySQL ↔ TiDB 四条纪律」）：
// 无外键 · 不假设 id 连续 · 批量分批提交 · 复杂 JSON 查询放应用层。
package database

import (
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// OpenMySQL 按 DSN 建立连接池并验证可达性。
// DSN 无默认值是有意为之 —— 忘配就启动失败，优于默默连错库。
func OpenMySQL(dsn string, maxOpenConns int) (*sqlx.DB, error) {
	if _, err := mysql.ParseDSN(dsn); err != nil {
		return nil, fmt.Errorf("DSN 格式非法: %w", err)
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxOpenConns / 2)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}
