package database_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/platform/database"
)

// open は一時ディレクトリに実 SQLite を開き、マイグレーションを適用した DB を返す。
func open(t *testing.T) *sql.DB {
	t.Helper()

	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("database.Open() returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close database: %v", err)
		}
	})
	return db
}

// insertJob は job_postings へ1件入れて id を返す。
func insertJob(t *testing.T, db *sql.DB, dedupKey string) int64 {
	t.Helper()

	res, err := db.ExecContext(context.Background(),
		`INSERT INTO job_postings (title, first_seen_at, last_seen_at, dedup_key)
		 VALUES (?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?)`,
		"テスト案件", dedupKey)
	if err != nil {
		t.Fatalf("failed to insert job_postings: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("failed to get last insert id: %v", err)
	}
	return id
}

func TestOpenEnforcesForeignKeys(t *testing.T) {
	t.Parallel()

	db := open(t)
	existing := insertJob(t, db, "url:https://example.test/jobs/1")

	tests := []struct {
		name    string
		jobID   func() int64
		wantErr bool
	}{
		{
			name:    "存在する job_id を参照する job_sources は登録できる",
			jobID:   func() int64 { return existing },
			wantErr: false,
		},
		{
			name:    "存在しない job_id を参照する job_sources は外部キー制約で失敗する",
			jobID:   func() int64 { return 99999 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := db.ExecContext(context.Background(),
				`INSERT INTO job_sources (job_id, source_name, external_id)
				 VALUES (?, ?, ?)`,
				tt.jobID(), "fixture-email", tt.name)

			if tt.wantErr {
				if err == nil {
					t.Fatal("外部キー制約違反を期待したが登録できてしまった（FK が効いていない）")
				}
				return
			}
			if err != nil {
				t.Fatalf("正当な INSERT が失敗した: %v", err)
			}
		})
	}
}

// TestOpenEnablesForeignKeysOnEveryConnection は、PRAGMA が
// プール内の1コネクションではなく全コネクションへ効いていることを確かめる。
//
// PRAGMA foreign_keys を db.ExecContext で発行していた頃は、2本目以降の
// コネクションで 0 に戻り、FK が実質無効だった。
func TestOpenEnablesForeignKeysOnEveryConnection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := open(t)

	// 同時に保持することで、プールへ返却済みの同じコネクションが
	// 使い回されず、確実に2本目が張られるようにする。
	const conns = 3
	held := make([]*sql.Conn, 0, conns)

	for i := range conns {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("failed to get connection %d: %v", i, err)
		}
		held = append(held, c)
	}
	t.Cleanup(func() {
		for _, c := range held {
			if err := c.Close(); err != nil {
				t.Errorf("failed to close connection: %v", err)
			}
		}
	})

	for i, c := range held {
		var enabled int
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil {
			t.Fatalf("connection %d の PRAGMA foreign_keys 取得に失敗: %v", i, err)
		}
		if enabled != 1 {
			t.Errorf("connection %d の foreign_keys = %d, want 1", i, enabled)
		}
	}
}
