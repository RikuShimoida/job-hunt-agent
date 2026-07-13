package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // CGO 不要な SQLite ドライバ

	"github.com/RikuShimoida/job-hunt-agent/migrations"
)

// Open は SQLite を開き、未適用のマイグレーションを昇順に適用する。
// path は DATABASE_URL の値（ファイルパス、または file: 形式の DSN）。
func Open(ctx context.Context, path string) (*sql.DB, error) {
	dsn := buildDSN(path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database %s: %w", path, err)
	}

	if err := db.PingContext(ctx); err != nil {
		return nil, closeWith(db, fmt.Errorf("failed to connect database %s: %w", path, err))
	}
	if err := Migrate(ctx, db, migrations.FS); err != nil {
		return nil, closeWith(db, err)
	}
	return db, nil
}

// buildDSN は接続文字列へ pragma を埋め込む。
//
// PRAGMA を db.ExecContext で発行しないのは、database/sql のプールが
// そのとき払い出した1コネクションにしか効かず、2本目以降のコネクションで
// 外部キーが無効に戻るため。DSN に載せると全コネクションへ適用される。
//
// busy_timeout は、定期実行が重なったときの SQLITE_BUSY を防ぐ。
func buildDSN(path string) string {
	dsn := path
	if !strings.HasPrefix(dsn, "file:") {
		dsn = "file:" + dsn
	}

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
}

// closeWith は失敗した Open の後始末をする。close 自体の失敗も握り潰さず、
// 元の原因と併せて返す。
func closeWith(db *sql.DB, cause error) error {
	if err := db.Close(); err != nil {
		return errors.Join(cause, fmt.Errorf("failed to close database: %w", err))
	}
	return cause
}

// Migrate は fsys 直下の *.sql をファイル名の昇順に適用する。
// 適用済みは schema_migrations で管理し、二重適用しない。
//
// golang-migrate を使わないのは、依存を1つ増やすほどの機能を必要としないため
// （ロールバック・分散ロックは現時点で不要）。
func Migrate(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	const createTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		return fmt.Errorf("failed to create schema_migrations: %w", err)
	}

	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return fmt.Errorf("failed to list migrations: %w", err)
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := isApplied(ctx, db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, db, fsys, name); err != nil {
			return err
		}
	}
	return nil
}

func isApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check migration %s: %w", version, err)
	}
	return count > 0, nil
}

func applyMigration(ctx context.Context, db *sql.DB, fsys fs.FS, version string) error {
	body, err := fs.ReadFile(fsys, version)
	if err != nil {
		return fmt.Errorf("failed to read migration %s: %w", version, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin migration %s: %w", version, err)
	}
	defer tx.Rollback() //nolint:errcheck // Commit 済みなら no-op

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("failed to apply migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
		return fmt.Errorf("failed to record migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration %s: %w", version, err)
	}
	return nil
}
