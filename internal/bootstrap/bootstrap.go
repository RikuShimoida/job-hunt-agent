// Package bootstrap は依存を明示的に組み立てる。
//
// DI フレームワークを使わないのは、依存が十数個で手書きの見通しが勝るため。
package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/application"
	"github.com/RikuShimoida/job-hunt-agent/internal/config"
	"github.com/RikuShimoida/job-hunt-agent/internal/connector/fixture"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
	"github.com/RikuShimoida/job-hunt-agent/internal/notifier/stdout"
	"github.com/RikuShimoida/job-hunt-agent/internal/platform/database"
	"github.com/RikuShimoida/job-hunt-agent/internal/platform/logging"
	"github.com/RikuShimoida/job-hunt-agent/internal/repository/sqlite"
)

// Options は組み立てに必要な入力。
type Options struct {
	ProfilePath string
	SourcesPath string
	// SourceFilter が空でなければ、そのソースだけを有効にする。
	SourceFilter string
	// Out は通知の出力先。
	Out io.Writer
	// LogOut はログの出力先。
	LogOut io.Writer
}

// App は組み立て済みのアプリケーション。
type App struct {
	Profile  model.Profile
	Pipeline *application.Pipeline
	Collect  *application.Collector
	Scorer   *application.Scorer
	Notifier *application.Notifier
	Logger   *slog.Logger

	db *sql.DB
}

// Close は保持しているリソースを閉じる。
func (a *App) Close() error {
	if a.db == nil {
		return nil
	}
	if err := a.db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}

// New は設定を読み、DB を開き、パイプラインを組み立てる。
func New(ctx context.Context, opts Options) (*App, error) {
	env := config.LoadEnv()
	logger := logging.New(opts.LogOut, env.LogLevel)

	profile, err := config.LoadProfile(opts.ProfilePath)
	if err != nil {
		return nil, err
	}

	sources, err := config.LoadSources(opts.SourcesPath)
	if err != nil {
		return nil, err
	}

	targets, err := selectSources(sources, opts.SourceFilter)
	if err != nil {
		return nil, err
	}

	connectors, err := buildConnectors(targets)
	if err != nil {
		return nil, err
	}

	db, err := database.Open(ctx, env.DatabaseURL)
	if err != nil {
		return nil, err
	}

	repo := sqlite.New(db)
	notifier := stdout.New(opts.Out)

	collector := application.NewCollector(repo, connectors, logger, time.Now)
	scorer := application.NewScorer(repo, logger)
	notify := application.NewNotifier(repo, notifier, logger)

	return &App{
		Profile:  profile,
		Pipeline: application.NewPipeline(collector, scorer, notify, logger),
		Collect:  collector,
		Scorer:   scorer,
		Notifier: notify,
		Logger:   logger,
		db:       db,
	}, nil
}

func selectSources(sources config.Sources, filter string) ([]config.Source, error) {
	if filter == "" {
		return sources.Enabled(), nil
	}
	src, err := sources.Find(filter)
	if err != nil {
		return nil, err
	}
	return []config.Source{src}, nil
}

func buildConnectors(sources []config.Source) ([]port.Connector, error) {
	connectors := make([]port.Connector, 0, len(sources))
	for _, s := range sources {
		switch s.Type {
		case config.SourceTypeFixtureEmail:
			connectors = append(connectors, fixture.NewEmail(s.Name, s.Path))
		case config.SourceTypeFixtureHTML:
			connectors = append(connectors, fixture.NewHTML(s.Name, s.Path))
		default:
			return nil, fmt.Errorf("%w: source %q has unsupported type %q",
				model.ErrInvalidSource, s.Name, s.Type)
		}
	}
	return connectors, nil
}
