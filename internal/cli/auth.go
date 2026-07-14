package cli

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/RikuShimoida/job-hunt-agent/internal/config"
	"github.com/RikuShimoida/job-hunt-agent/internal/connector/gmail"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// ErrAuthFailed は初回認証が完了しなかったことを示す。
var ErrAuthFailed = errors.New("gmail auth failed")

// 認可コードの受け取りを待つ上限。ブラウザを閉じたまま放置されても
// プロセスが residual に残らないよう区切る。
const authTimeout = 3 * time.Minute

func newAuthCommand() *cobra.Command {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "外部サービスの初回認証を行う",
	}
	auth.AddCommand(newAuthGmailCommand())
	return auth
}

func newAuthGmailCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "gmail",
		Short: "Gmail の読み取り専用トークンを取得する",
		Long: "ブラウザで Google の認可画面を開き、リフレッシュトークンを表示する。\n" +
			"要求するスコープは gmail.readonly のみ（削除・返信・ラベル変更は行わない）。\n" +
			"GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET を .env に設定してから実行する。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthGmail(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func runAuthGmail(ctx context.Context, out io.Writer) error {
	env := config.LoadEnv()
	if env.GoogleClientID == "" || env.GoogleClientSecret == "" {
		return fmt.Errorf("%w: GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are required",
			model.ErrMissingGoogleCredentials)
	}

	// ポートを 0 で開いて OS に空きを選ばせる。固定ポートだと他プロセスと
	// 衝突したときに原因が分かりにくい。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("%w: failed to open local port: %w", ErrAuthFailed, err)
	}
	defer func() { _ = listener.Close() }()

	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)
	cfg := &oauth2.Config{
		ClientID:     env.GoogleClientID,
		ClientSecret: env.GoogleClientSecret,
		Endpoint:     gmail.Endpoint,
		Scopes:       []string{gmail.ScopeReadonly},
		RedirectURL:  redirectURL,
	}

	state, err := randomState()
	if err != nil {
		return err
	}

	// AccessTypeOffline と ApprovalForce が無いとリフレッシュトークンが返らない
	// （2回目以降の認可では Google が refresh_token を省略する）。
	authURL := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
	)

	if err := writeLines(out,
		"以下の URL をブラウザで開き、Gmail の読み取りを許可してください。",
		"",
		authURL,
		"",
		"認可を待っています...",
	); err != nil {
		return err
	}

	code, err := waitForCode(ctx, listener, state)
	if err != nil {
		return err
	}

	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		// エラー全体を載せないのは、oauth2 の *RetrieveError が
		// リクエストボディ（認可コード・クライアントシークレット）を含むため。
		// 原因の切り分けに要るエラーコード（invalid_grant など）だけを残す。
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.ErrorCode != "" {
			return fmt.Errorf("%w: failed to exchange authorization code (%s)",
				ErrAuthFailed, re.ErrorCode)
		}
		return fmt.Errorf("%w: failed to exchange authorization code", ErrAuthFailed)
	}
	if token.RefreshToken == "" {
		return fmt.Errorf("%w: google did not return a refresh token", ErrAuthFailed)
	}

	return writeLines(out,
		"",
		"認証に成功しました。以下を .env に追記してください。",
		"",
		"GOOGLE_REFRESH_TOKEN="+token.RefreshToken,
	)
}

func writeLines(out io.Writer, lines ...string) error {
	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
	}
	return nil
}

// randomState は CSRF 検証用の state を作る。
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("%w: failed to generate state", ErrAuthFailed)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// waitForCode はローカルサーバーで認可コールバックを1回だけ受け取る。
func waitForCode(ctx context.Context, listener net.Listener, state string) (string, error) {
	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		if errMsg := q.Get("error"); errMsg != "" {
			http.Error(w, "認可が拒否されました", http.StatusBadRequest)
			done <- result{err: fmt.Errorf("%w: authorization denied", ErrAuthFailed)}
			return
		}
		// state を照合しないと、第三者が仕込んだコールバックで
		// 別アカウントのトークンを掴まされうる（CSRF）。
		if q.Get("state") != state {
			http.Error(w, "state が一致しません", http.StatusBadRequest)
			done <- result{err: fmt.Errorf("%w: state mismatch", ErrAuthFailed)}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "認可コードがありません", http.StatusBadRequest)
			done <- result{err: fmt.Errorf("%w: no authorization code", ErrAuthFailed)}
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// ブラウザへの応答が書けなくても、認可コードは受け取れている。
		// ここで失敗を返すと取得できたトークンを捨てることになる。
		_, _ = fmt.Fprintln(w, "認証が完了しました。ターミナルへ戻ってください。")
		done <- result{code: code}
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		// Serve はリスナーが閉じるまでブロックする。閉じたときの
		// ErrServerClosed は正常終了なので握って良い。
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			done <- result{err: fmt.Errorf("%w: local server: %v", ErrAuthFailed, err)}
		}
	}()
	defer func() { _ = srv.Close() }()

	timeout := time.NewTimer(authTimeout)
	defer timeout.Stop()

	select {
	case r := <-done:
		return r.code, r.err
	case <-timeout.C:
		return "", fmt.Errorf("%w: timed out waiting for authorization", ErrAuthFailed)
	case <-ctx.Done():
		return "", fmt.Errorf("%w: canceled", ErrAuthFailed)
	}
}
