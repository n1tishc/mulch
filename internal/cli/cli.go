package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/bus"
	meval "github.com/n1tishc/mulch/internal/eval"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/score"
	"github.com/n1tishc/mulch/internal/server"
	"github.com/n1tishc/mulch/internal/session"
	"github.com/n1tishc/mulch/internal/tool"
)

const defaultModel = "glm-5.3-flash"

type LLMFactory func(apiKey, baseURL string) provider.LLM
type EmbedderFactory func(apiKey, baseURL, model string) provider.Embedder
type Options struct {
	Stdout, Stderr  io.Writer
	Getenv          func(string) string
	LLMFactory      LLMFactory
	EmbedderFactory EmbedderFactory
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
	if opts.EmbedderFactory == nil {
		opts.EmbedderFactory = func(key, base, model string) provider.Embedder { return provider.NewVoyage(key, base, model) }
	}
	_ = godotenv.Load()
	if len(args) == 0 {
		return errors.New("usage: mulch <run|eval|replay|resume|branch|tree|label|sessions|serve> [flags]")
	}
	switch args[0] {
	case "run":
		return run(ctx, args[1:], opts)
	case "eval":
		return evalCommand(ctx, args[1:], opts)
	case "replay":
		return replay(ctx, args[1:], opts)
	case "resume":
		return resume(ctx, args[1:], opts)
	case "branch":
		return branch(ctx, args[1:], opts)
	case "tree":
		return tree(ctx, args[1:], opts)
	case "label":
		return label(ctx, args[1:], opts)
	case "sessions":
		return sessions(ctx, args[1:], opts)
	case "serve":
		return serve(ctx, args[1:], opts)
	default:
		return errors.New("usage: mulch <run|eval|replay|resume|branch|tree|label|sessions|serve> [flags]")
	}
}

func serve(ctx context.Context, args []string, opts Options) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	address := flags.String("addr", ":4141", "listen address")
	dbPath := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("mulch serve takes no arguments")
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", *address, err)
	}
	_ = listener.Close()
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	store, err := event.Open(context.WithoutCancel(ctx), *dbPath, nil)
	if err != nil {
		return err
	}
	defer store.Close()
	_, _ = fmt.Fprintf(opts.Stdout, "mulch viewer listening on http://%s\n", viewerAddress(*address))
	return server.New(store).Serve(ctx, *address)
}

func viewerAddress(address string) string {
	if strings.HasPrefix(address, ":") {
		return "localhost" + address
	}
	return address
}

func evalCommand(ctx context.Context, args []string, opts Options) error {
	flags := flag.NewFlagSet("eval", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	runs := flags.Int("runs", 10, "runs per intervention mode")
	concurrency := flags.Int("concurrency", 5, "maximum concurrent sessions")
	output := flags.String("output", ".", "report directory")
	model := flags.String("model", envOr(opts.Getenv, "MULCH_MODEL", defaultModel), "model")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("mulch eval requires exactly one scenario file")
	}
	if *runs < 1 || *concurrency < 1 {
		return errors.New("--runs and --concurrency must be positive")
	}
	key := opts.Getenv("MULCH_PROVIDER_API_KEY")
	if key == "" {
		return errors.New("MULCH_PROVIDER_API_KEY is required (set it in the environment or .env)")
	}
	scenario, err := meval.LoadScenario(flags.Arg(0))
	if err != nil {
		return err
	}
	result, err := meval.Run(ctx, scenario, meval.Options{Runs: *runs, Concurrency: *concurrency, OutputDir: *output, Model: *model, LLM: func() provider.LLM { return opts.LLMFactory(key, opts.Getenv("MULCH_PROVIDER_BASE_URL")) }})
	if err != nil {
		return err
	}
	_, err = io.WriteString(opts.Stdout, result.Markdown())
	return err
}

func resume(ctx context.Context, args []string, opts Options) error {
	flags := flag.NewFlagSet("resume", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	dbPath := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	jsonMode := flags.Bool("json", false, "write committed events as JSONL")
	policyPath := flags.String("policy", "", "intervention policy JSON file")
	noIntervene := flags.Bool("no-intervene", false, "score health without intervening")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 || flags.NArg() > 2 {
		return errors.New("mulch resume requires a session ID and optional prompt")
	}
	return continueCLI(ctx, *dbPath, flags.Arg(0), optionalArg(flags.Args(), 1), *jsonMode, *policyPath, *noIntervene, opts)
}

func branch(ctx context.Context, args []string, opts Options) error {
	args = intersperseBranchFlags(args)
	flags := flag.NewFlagSet("branch", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	dbPath := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	at := flags.Int64("at", -1, "parent sequence to branch from")
	jsonMode := flags.Bool("json", false, "write committed events as JSONL")
	policyPath := flags.String("policy", "", "intervention policy JSON file")
	noIntervene := flags.Bool("no-intervene", false, "score health without intervening")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 || flags.NArg() > 2 || *at < 0 {
		return errors.New("mulch branch requires a session ID, --at sequence, and optional prompt")
	}
	if opts.Getenv("MULCH_PROVIDER_API_KEY") == "" {
		return errors.New("MULCH_PROVIDER_API_KEY is required (set it in the environment or .env)")
	}
	store, err := event.Open(context.WithoutCancel(ctx), *dbPath, nil)
	if err != nil {
		return err
	}
	child, err := store.Branch(ctx, flags.Arg(0), *at)
	closeErr := store.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return continueCLI(ctx, *dbPath, child.ID, optionalArg(flags.Args(), 1), *jsonMode, *policyPath, *noIntervene, opts)
}

func continueCLI(ctx context.Context, dbPath, id, prompt string, jsonMode bool, policyPath string, noIntervene bool, opts Options) error {
	key := opts.Getenv("MULCH_PROVIDER_API_KEY")
	if key == "" {
		return errors.New("MULCH_PROVIDER_API_KEY is required (set it in the environment or .env)")
	}
	publisher := &fanoutPublisher{}
	var jsonOutput *jsonlPublisher
	var healthOutput *healthPublisher
	if jsonMode {
		jsonOutput = &jsonlPublisher{writer: opts.Stdout}
		publisher.Add(jsonOutput)
	} else {
		healthOutput = &healthPublisher{writer: opts.Stderr}
		publisher.Add(healthOutput)
	}
	store, err := event.Open(context.WithoutCancel(ctx), dbPath, publisher)
	if err != nil {
		return err
	}
	defer store.Close()
	session, err := store.Session(ctx, id)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	executor := tool.NewExecutor([]tool.Tool{tool.NewRead(session.Workdir), tool.NewWrite(session.Workdir), tool.NewEdit(session.Workdir), tool.NewBash(session.Workdir)})
	history, err := store.List(ctx, id, 1)
	if err != nil {
		return err
	}
	judge := opts.LLMFactory(key, opts.Getenv("MULCH_PROVIDER_BASE_URL"))
	coherence := score.NewCoherenceWithModel(judge, envOr(opts.Getenv, "MULCH_JUDGE_MODEL", session.Model))
	scoring := score.NewRunnerWithWeights(store, session, healthScorers(opts, store, coherence), healthWeights(opts), history...)
	publisher.Add(scoring)
	hooks := []hook.Hook{scoring}
	if !noIntervene {
		policy, policyErr := configuredPolicy(policyPath)
		if policyErr != nil {
			return policyErr
		}
		ladder := intervene.NewConfigured(store, policy, coherence).WithSummarizer(intervene.NewLLMSummarizer(judge, envOr(opts.Getenv, "MULCH_JUDGE_MODEL", session.Model)))
		for _, recorded := range history {
			ladder.OnEvent(ctx, recorded)
		}
		publisher.Add(ladder)
		hooks = append(hooks, ladder)
	}
	emit := func(text string) { _, _ = io.WriteString(opts.Stdout, text) }
	if jsonMode {
		emit = func(string) {}
	}
	_, runErr := agent.Resume(ctx, agent.Dependencies{Store: store, LLM: opts.LLMFactory(key, opts.Getenv("MULCH_PROVIDER_BASE_URL")), Tools: executor, Model: session.Model, Workdir: session.Workdir, ContextWindow: session.ContextWindow, Hooks: hooks}, id, prompt, emit)
	if !jsonMode {
		_, _ = fmt.Fprintf(opts.Stderr, "\nsession %s\n", id)
	}
	if jsonOutput != nil {
		return errors.Join(runErr, jsonOutput.Err())
	}
	if healthOutput != nil {
		return errors.Join(runErr, healthOutput.err)
	}
	return runErr
}

func sessions(ctx context.Context, args []string, opts Options) error {
	flags := flag.NewFlagSet("sessions", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	dbPath := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("mulch sessions takes no arguments")
	}
	store, err := event.Open(context.WithoutCancel(ctx), *dbPath, nil)
	if err != nil {
		return err
	}
	defer store.Close()
	all, err := store.Sessions(ctx)
	if err != nil {
		return err
	}
	return writeSessions(opts.Stdout, all)
}

func tree(ctx context.Context, args []string, opts Options) error {
	flags := flag.NewFlagSet("tree", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	dbPath := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("mulch tree requires exactly one session ID")
	}
	store, err := event.Open(context.WithoutCancel(ctx), *dbPath, nil)
	if err != nil {
		return err
	}
	defer store.Close()
	node, err := store.Tree(ctx, flags.Arg(0))
	if err != nil {
		return fmt.Errorf("load session tree: %w", err)
	}
	return writeTree(opts.Stdout, node)
}

func label(ctx context.Context, args []string, opts Options) error {
	flags := flag.NewFlagSet("label", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	dbPath := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("mulch label requires a session ID and label")
	}
	store, err := event.Open(context.WithoutCancel(ctx), *dbPath, nil)
	if err != nil {
		return err
	}
	defer store.Close()
	return store.SetLabel(ctx, flags.Arg(0), flags.Arg(1))
}

func optionalArg(args []string, index int) string {
	if len(args) > index {
		return args[index]
	}
	return ""
}

func intersperseBranchFlags(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--json" || args[i] == "--no-intervene" {
			flags = append(flags, args[i])
			continue
		}
		if args[i] == "--at" || args[i] == "--db" || args[i] == "--policy" {
			if i+1 < len(args) {
				flags = append(flags, args[i], args[i+1])
				i++
				continue
			}
		}
		positional = append(positional, args[i])
	}
	return append(flags, positional...)
}

func run(ctx context.Context, args []string, opts Options) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	workdir := flags.String("workdir", ".", "working directory")
	model := flags.String("model", envOr(opts.Getenv, "MULCH_MODEL", defaultModel), "model")
	contextWindow := flags.Int("context-window", 200000, "model context window in tokens")
	dbPath := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	jsonMode := flags.Bool("json", false, "write committed events as JSONL")
	policyPath := flags.String("policy", "", "intervention policy JSON file")
	noIntervene := flags.Bool("no-intervene", false, "score health without intervening")
	if err := flags.Parse(args); err != nil {
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
	publisher := &fanoutPublisher{}
	eventBus := bus.New()
	publisher.Add(eventBus)
	var jsonOutput *jsonlPublisher
	var healthOutput *healthPublisher
	if *jsonMode {
		jsonOutput = &jsonlPublisher{writer: opts.Stdout}
		publisher.Add(jsonOutput)
	} else {
		healthOutput = &healthPublisher{writer: opts.Stderr}
		publisher.Add(healthOutput)
	}
	store, err := event.Open(context.WithoutCancel(ctx), *dbPath, publisher)
	if err != nil {
		return err
	}
	defer store.Close()
	executor := tool.NewExecutor([]tool.Tool{tool.NewRead(*workdir), tool.NewWrite(*workdir), tool.NewEdit(*workdir), tool.NewBash(*workdir)})
	judge := opts.LLMFactory(key, opts.Getenv("MULCH_PROVIDER_BASE_URL"))
	coherence := score.NewCoherenceWithModel(judge, envOr(opts.Getenv, "MULCH_JUDGE_MODEL", *model))
	scoring := score.NewRunnerWithWeights(store, event.Session{Task: flags.Arg(0), Model: *model, Workdir: *workdir, ContextWindow: *contextWindow}, healthScorers(opts, store, coherence), healthWeights(opts))
	publisher.Add(scoring)
	hooks := []hook.Hook{scoring}
	if !*noIntervene {
		policy, policyErr := configuredPolicy(*policyPath)
		if policyErr != nil {
			return policyErr
		}
		ladder := intervene.NewConfigured(store, policy, coherence).WithSummarizer(intervene.NewLLMSummarizer(judge, envOr(opts.Getenv, "MULCH_JUDGE_MODEL", *model)))
		publisher.Add(ladder)
		hooks = append(hooks, ladder)
	}
	emit := func(text string) { _, _ = io.WriteString(opts.Stdout, text) }
	if *jsonMode {
		emit = func(string) {}
	}
	manager := session.New(session.Options{Bus: eventBus, Run: func(runCtx context.Context, id, task string, _ session.RunOpts, steering *session.Steering) error {
		sessionHooks := append(append([]hook.Hook(nil), hooks...), steering.Bind(store))
		_, runErr := agent.RunSession(runCtx, agent.Dependencies{Store: store, LLM: opts.LLMFactory(key, opts.Getenv("MULCH_PROVIDER_BASE_URL")), Tools: executor, Model: *model, Workdir: *workdir, ContextWindow: *contextWindow, Hooks: sessionHooks}, id, task, emit)
		return runErr
	}})
	defer manager.Close()
	id, runErr := manager.StartWith(ctx, flags.Arg(0), session.RunOpts{Workdir: *workdir})
	if runErr == nil {
		runErr = manager.Wait(ctx, id)
	}
	if id != "" {
		if !*jsonMode {
			_, _ = fmt.Fprintf(opts.Stderr, "\nsession %s\n", id)
		}
	}
	if jsonOutput != nil {
		return errors.Join(runErr, jsonOutput.Err())
	}
	if healthOutput != nil {
		return errors.Join(runErr, healthOutput.err)
	}
	return runErr
}

func configuredPolicy(path string) (intervene.Policy, error) {
	if path == "" {
		return intervene.DefaultPolicy(), nil
	}
	return intervene.LoadPolicy(path)
}

func healthScorers(opts Options, cache provider.EmbeddingCache, coherence *score.Coherence) []score.Scorer {
	scorers := []score.Scorer{score.Saturation{}, score.Staleness{}, coherence}
	if key := opts.Getenv("MULCH_EMBEDDING_API_KEY"); key != "" {
		embedder := opts.EmbedderFactory(key, opts.Getenv("MULCH_EMBEDDING_BASE_URL"), envOr(opts.Getenv, "MULCH_EMBEDDING_MODEL", "voyage-4-lite"))
		scorers = append(scorers, score.NewRelevance(embedder, cache))
	}
	return scorers
}

func healthWeights(opts Options) score.Weights {
	d := score.DefaultWeights()
	d.Saturation = envFloat(opts.Getenv, "MULCH_HEALTH_WEIGHT_SATURATION", d.Saturation)
	d.Staleness = envFloat(opts.Getenv, "MULCH_HEALTH_WEIGHT_STALENESS", d.Staleness)
	d.Relevance = envFloat(opts.Getenv, "MULCH_HEALTH_WEIGHT_RELEVANCE", d.Relevance)
	d.Coherence = envFloat(opts.Getenv, "MULCH_HEALTH_WEIGHT_COHERENCE", d.Coherence)
	return d
}
func envFloat(getenv func(string) string, key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(getenv(key), 64)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func replay(ctx context.Context, args []string, opts Options) error {
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	dbPath := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	jsonMode := flags.Bool("json", false, "write recorded events as JSONL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("mulch replay requires exactly one session ID")
	}
	store, err := event.Open(context.WithoutCancel(ctx), *dbPath, nil)
	if err != nil {
		return err
	}
	defer store.Close()
	if _, err := store.Session(ctx, flags.Arg(0)); err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	events, err := store.List(ctx, flags.Arg(0), 1)
	if err != nil {
		return err
	}
	if *jsonMode {
		return writeJSONL(opts.Stdout, events)
	}
	return replayTerminal(opts.Stdout, events)
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
