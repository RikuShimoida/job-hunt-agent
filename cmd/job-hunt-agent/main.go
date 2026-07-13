package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/RikuShimoida/job-hunt-agent/internal/cli"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run を main から分けているのは、os.Exit が defer を実行しないため。
// stop() を確実に呼ぶ必要がある。
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return cli.NewRootCommand().ExecuteContext(ctx)
}
