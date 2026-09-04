package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/n1tishc/mulch/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.Execute(ctx, os.Args[1:], cli.Options{Version: version}); err != nil {
		fmt.Fprintln(os.Stderr, "mulch:", err)
		os.Exit(1)
	}
}
