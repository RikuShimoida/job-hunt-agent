// Package cli は CLI サブコマンドを定義する。
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/RikuShimoida/job-hunt-agent/internal/bootstrap"
	"github.com/RikuShimoida/job-hunt-agent/internal/config"
)

const (
	defaultProfilePath = "config/profile.yaml"
	defaultSourcesPath = "config/sources.yaml"
)

// ErrDryRunRequired は Phase 1 で --dry-run 以外の通知が使えないことを示す。
var ErrDryRunRequired = errors.New("slack notification is not implemented yet; use --dry-run")

type globalFlags struct {
	profilePath string
	sourcesPath string
}

// NewRootCommand はルートコマンドを組み立てる。
func NewRootCommand() *cobra.Command {
	g := &globalFlags{}

	root := &cobra.Command{
		Use:           "job-hunt-agent",
		Short:         "案件を収集・評価して通知するエージェント",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&g.profilePath, "profile", defaultProfilePath, "プロフィール設定ファイル")
	root.PersistentFlags().StringVar(&g.sourcesPath, "sources", defaultSourcesPath, "ソース設定ファイル")

	root.AddCommand(
		newInitCommand(),
		newProfileCommand(g),
		newCollectCommand(g),
		newScoreCommand(g),
		newNotifyCommand(g),
		newRunCommand(g),
	)
	return root
}

func newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "設定ファイルのひな形を生成する",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pairs := [][2]string{
				{"config/profile.example.yaml", defaultProfilePath},
				{"config/sources.example.yaml", defaultSourcesPath},
				{".env.example", ".env"},
			}
			for _, p := range pairs {
				if err := copyIfAbsent(cmd.OutOrStdout(), p[0], p[1]); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// copyIfAbsent は dst が既にあれば上書きしない。
// 実値を入れた設定を誤って消さないため、上書きは行わずスキップを報告する。
func copyIfAbsent(out io.Writer, src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		if _, err := fmt.Fprintf(out, "スキップ: %s は既に存在します\n", dst); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat %s: %w", dst, err)
	}

	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", src, err)
	}
	if dir := filepath.Dir(dst); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create dir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(dst, body, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}
	if _, err := fmt.Fprintf(out, "作成: %s\n", dst); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}
	return nil
}

func newProfileCommand(g *globalFlags) *cobra.Command {
	profile := &cobra.Command{
		Use:   "profile",
		Short: "プロフィール設定を操作する",
	}
	profile.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "プロフィール設定を検証する",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := config.LoadProfile(g.profilePath); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s は妥当です\n", g.profilePath)
			return err
		},
	})
	return profile
}

func newCollectCommand(g *globalFlags) *cobra.Command {
	var source string

	cmd := &cobra.Command{
		Use:   "collect",
		Short: "有効なソースから案件を収集する",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := newApp(cmd, g, source)
			if err != nil {
				return err
			}
			defer app.Close() //nolint:errcheck // 終了時の close 失敗は報告済みのログで足りる

			summary, err := app.Collect.Collect(cmd.Context(), app.Profile)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(),
				"取得 %d件 / 新規 %d件 / 重複 %d件 / 失敗ソース %v\n",
				summary.FetchedCount, summary.NewCount, summary.DuplicateCount, summary.FailedSources)
			return err
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "指定したソースのみ収集する")
	return cmd
}

func newScoreCommand(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "score",
		Short: "保存済み案件を再評価する",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := newApp(cmd, g, "")
			if err != nil {
				return err
			}
			defer app.Close() //nolint:errcheck // 終了時の close 失敗は報告済みのログで足りる

			summary, err := app.Scorer.Score(cmd.Context(), app.Profile)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "採点 %d件 / 除外 %d件\n",
				summary.ScoredCount, summary.RejectedCount)
			return err
		},
	}
}

func newNotifyCommand(g *globalFlags) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "notify",
		Short: "閾値以上の案件を通知する",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !dryRun {
				return ErrDryRunRequired
			}
			app, err := newApp(cmd, g, "")
			if err != nil {
				return err
			}
			defer app.Close() //nolint:errcheck // 終了時の close 失敗は報告済みのログで足りる

			if _, err := app.Notifier.Notify(cmd.Context(), app.Profile); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "送信せず標準出力に表示する")
	return cmd
}

func newRunCommand(g *globalFlags) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "収集・採点・通知を順に実行する",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !dryRun {
				return ErrDryRunRequired
			}
			app, err := newApp(cmd, g, "")
			if err != nil {
				return err
			}
			defer app.Close() //nolint:errcheck // 終了時の close 失敗は報告済みのログで足りる

			if _, err := app.Pipeline.Run(cmd.Context(), app.Profile); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "送信せず標準出力に表示する")
	return cmd
}

func newApp(cmd *cobra.Command, g *globalFlags, source string) (*bootstrap.App, error) {
	return bootstrap.New(cmd.Context(), bootstrap.Options{
		ProfilePath:  g.profilePath,
		SourcesPath:  g.sourcesPath,
		SourceFilter: source,
		Out:          cmd.OutOrStdout(),
		LogOut:       cmd.ErrOrStderr(),
	})
}
