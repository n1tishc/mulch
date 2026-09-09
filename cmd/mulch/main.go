package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/n1tishc/mulch/internal/cli"
)

var version = "dev"

func main() {
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM}
	var interrupts chan os.Signal
	if len(os.Args) == 1 || os.Args[1] == "chat" || strings.HasPrefix(os.Args[1], "-") {
		signals = []os.Signal{syscall.SIGTERM}
		interrupts = make(chan os.Signal, 1)
		signal.Notify(interrupts, os.Interrupt)
		defer signal.Stop(interrupts)
	}
	ctx, stop := signal.NotifyContext(context.Background(), signals...)
	defer stop()
	if err := cli.Execute(ctx, os.Args[1:], cli.Options{Version: version, Interrupts: interrupts}); err != nil {
		fmt.Fprintln(os.Stderr, "mulch:", err)
		os.Exit(1)
	}
}
