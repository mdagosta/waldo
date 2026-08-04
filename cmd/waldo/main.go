package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/openwaldo/waldo-new/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.RunContext(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
