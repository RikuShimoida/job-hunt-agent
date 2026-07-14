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

	"golang.org/x/oauth2"

	"github.com/RikuShimoida/job-hunt-agent/internal/application"
	"github.com/RikuShimoida/job-hunt-agent/internal/config"
	"github.com/RikuShimoida/job-hunt-agent/internal/connector/fixture"
	"github.com/RikuShimoida/job-hunt-agent/internal/connector/gmail"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
	"github.com/RikuShimoida/job-hunt-agent/internal/notifier/noop"
	"github.com/RikuShimoida/job-hunt-agent/internal/notifier/slack"
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
	// DryRun が true なら Slack へ送らず、送信予定の内容を Out へ書く。
	DryRun bool
	// NeedsConnectors が true のときだけコネクタを組み立てる（collect / run）。
	//
	// score / notify でも組み立てると、gmail ソースを有効にしているだけで
	// これらのコマンドまで ErrMissingGoogleCredentials で起動時に停止する。
	// リフレッシュトークンが失効したとき、Gmail に一切触らない notify の再送経路まで
	// 巻き添えで止まってしまう（collect / score が SLACK_WEBHOOK_URL を要求しないのと同じ非対称）。
	NeedsConnectors bool
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

	var connectors []port.Connector
	if opts.NeedsConnectors {
		connectors, err = buildConnectors(ctx, targets, env)
		if err != nil {
			return nil, err
		}
	}

	// 通知先の確定を DB より先に置く。Webhook 未設定で停止するなら、
	// DB ファイルを作る前に止めたい。
	notifier, errorNotifier, err := buildNotifiers(opts, env)
	if err != nil {
		return nil, err
	}

	db, err := database.Open(ctx, env.DatabaseURL)
	if err != nil {
		return nil, err
	}

	repo := sqlite.New(db)

	collector := application.NewCollector(repo, connectors, errorNotifier, logger, time.Now)
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

// buildNotifiers は dry-run か実送信かで通知先を切り替える。
//
// SLACK_WEBHOOK_URL 未設定時に標準出力へフォールバックしないのは、
// 「送ったつもりで誰にも届いていない」事故になるため。起動時に停止する。
func buildNotifiers(opts Options, env config.Env) (port.Notifier, port.ErrorNotifier, error) {
	if opts.DryRun {
		return stdout.New(opts.Out), stdout.NewErrorNotifier(opts.Out), nil
	}

	if env.SlackWebhookURL == "" {
		return nil, nil, fmt.Errorf("%w: SLACK_WEBHOOK_URL is required without --dry-run",
			model.ErrMissingWebhookURL)
	}

	// エラー通知先の未設定は正常系。ソース失敗は構造化ログに残る。
	if env.SlackErrorWebhookURL == "" {
		return slack.New(env.SlackWebhookURL), noop.NewErrorNotifier(), nil
	}
	return slack.New(env.SlackWebhookURL), slack.NewErrorNotifier(env.SlackErrorWebhookURL), nil
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

// buildConnectors はソース設定からコネクタを組み立てる。
//
// GOOGLE_* 未設定で gmail ソースが有効なら起動時に停止する。黙って0件成功にすると
// 「収集したつもりで1件も取れていない」事故になるため（SLACK_WEBHOOK_URL 未設定で
// 停止するのと同じ方針）。
func buildConnectors(ctx context.Context, sources []config.Source, env config.Env) ([]port.Connector, error) {
	connectors := make([]port.Connector, 0, len(sources))
	for _, s := range sources {
		switch s.Type {
		case config.SourceTypeFixtureEmail:
			connectors = append(connectors, fixture.NewEmail(s.Name, s.Path))
		case config.SourceTypeFixtureHTML:
			connectors = append(connectors, fixture.NewHTML(s.Name, s.Path))
		case config.SourceTypeGmail:
			if !env.HasGoogleCredentials() {
				return nil, fmt.Errorf("%w: source %q requires GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET / GOOGLE_REFRESH_TOKEN",
					model.ErrMissingGoogleCredentials, s.Name)
			}
			connectors = append(connectors, gmail.New(
				s.Name,
				s.Senders,
				s.GmailNewerThan(),
				s.GmailMaxResults(),
				googleTokenSource(ctx, env),
			))
		default:
			return nil, fmt.Errorf("%w: source %q has unsupported type %q",
				model.ErrInvalidSource, s.Name, s.Type)
		}
	}
	return connectors, nil
}

// googleTokenSource はリフレッシュトークンからアクセストークンを供給する。
// TokenSource は自動で更新し、期限切れを呼び出し側が気にしなくて済む。
func googleTokenSource(ctx context.Context, env config.Env) oauth2.TokenSource {
	cfg := &oauth2.Config{
		ClientID:     env.GoogleClientID,
		ClientSecret: env.GoogleClientSecret,
		Endpoint:     gmail.Endpoint,
		Scopes:       []string{gmail.ScopeReadonly},
	}
	return cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: env.GoogleRefreshToken})
}
