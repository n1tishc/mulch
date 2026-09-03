package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/bus"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

const defaultModel = "glm-5.3-flash"

type LLMFactory func(apiKey, baseURL string) provider.LLM
type Options struct {
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	LLMFactory     LLMFactory
}

func Execute(ctx context.Context, args []string, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.LLMFactory == nil {
		opts.LLMFactory = func(key, base string) provider.LLM { return provider.NewOpenAI(key, base) }
	}
	_ = godotenv.Load()
	if len(args) == 0 || args[0] != "run" {
		return errors.New("usage: mulch run [flags] <prompt>")
	}
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	workdir := flags.String("workdir", ".", "working directory")
	model := flags.String("model", envOr(opts.Getenv, "MULCH_MODEL", defaultModel), "model")
	dbPath := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("mulch run requires exactly one prompt")
	}
	key := opts.Getenv("MULCH_PROVIDER_API_KEY")
	if key == "" {
		return errors.New("MULCH_PROVIDER_API_KEY is required (set it in the environment or .env)")
	}
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	b := bus.New()
	store, err := event.Open(context.WithoutCancel(ctx), *dbPath, b)
	if err != nil {
		return err
	}
	defer store.Close()
	id, runErr := agent.Run(ctx, agent.Dependencies{Store: store, LLM: opts.LLMFactory(key, opts.Getenv("MULCH_PROVIDER_BASE_URL")), Model: *model, Workdir: *workdir}, flags.Arg(0), func(text string) { _, _ = io.WriteString(opts.Stdout, text) })
	if id != "" {
		_, _ = fmt.Fprintf(opts.Stderr, "\nsession %s\n", id)
	}
	return runErr
}

func envOr(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}
func defaultDB() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "mulch.db"
	}
	return filepath.Join(home, ".mulch", "mulch.db")
}
