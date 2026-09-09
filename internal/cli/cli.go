package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/bus"
	meval "github.com/n1tishc/mulch/internal/eval"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
	harness "github.com/n1tishc/mulch/internal/runtime"
	"github.com/n1tishc/mulch/internal/score"
	"github.com/n1tishc/mulch/internal/server"
	"github.com/n1tishc/mulch/internal/session"
	"github.com/n1tishc/mulch/internal/tool"
)

const defaultModel = "glm-5.3-flash"

type LLMFactory func(apiKey, baseURL string) provider.LLM
type EmbedderFactory func(apiKey, baseURL, model string) provider.Embedder
type Options struct {
	Stdin           io.Reader
	Interrupts      <-chan os.Signal
	Stdout, Stderr  io.Writer
	Getenv          func(string) string
	LLMFactory      LLMFactory
	EmbedderFactory EmbedderFactory
	Version         string
}

func Execute(ctx context.Context, args []string, opts Options) error {
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		_, err := fmt.Fprintln(opts.Stdout, "Mulch — terminal coding agent and context-repair harness\n\n  mulch [chat flags]       Open the interactive terminal\n  mulch chat --help        Show interactive launch options\n  mulch run PROMPT         Execute a single task\n  mulch web                Open the local coding workspace\n  mulch serve              Start the browser server\n  mulch sessions           List saved sessions\n  mulch resume ID PROMPT   Continue a saved task once\n  mulch replay ID          Replay a saved trace\n  mulch branch ID --at N   Branch a session\n  mulch eval SCENARIO      Run correctness evaluation\n  mulch version           Show version\n\nInside the terminal: type / to browse commands or /help for controls.")
		return err
	}
	if len(args) == 1 && args[0] == "version" {
		version := opts.Version
		if version == "" {
			version = "dev"
		}
		_, err := fmt.Fprintf(opts.Stdout, "mulch %s\n", version)
		return err
	}
	if opts.LLMFactory == nil {
		opts.LLMFactory = func(key, base string) provider.LLM { return provider.NewOpenAI(key, base) }
	}
	if opts.EmbedderFactory == nil {
		opts.EmbedderFactory = func(key, base, model string) provider.Embedder { return provider.NewVoyage(key, base, model) }
	}
	_ = godotenv.Load()
	if len(args) == 0 {
		return chat(ctx, nil, opts)
	}
	if strings.HasPrefix(args[0], "-") && args[0] != "--help" && args[0] != "-h" {
		return chat(ctx, args, opts)
	}
	switch args[0] {
	case "chat":
		return chat(ctx, args[1:], opts)
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
	case "web":
		return serveMode(ctx, args[1:], opts, true)
	default:
		return usageError()
	}
}

func usageError() error {
	return errors.New("usage: mulch <chat|run|eval|replay|resume|branch|tree|label|sessions|serve|version> [flags]")
}

func serve(ctx context.Context, args []string, opts Options) error {
	return serveMode(ctx, args, opts, false)
}

func serveMode(ctx context.Context, args []string, opts Options, web bool) error {
	command := "serve"
	if web {
		command = "web"
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	defaultAddr := "127.0.0.1:4141"
	if web {
		defaultAddr = "127.0.0.1:0"
	}
	address := flags.String("addr", defaultAddr, "listen address")
	noOpen := flags.Bool("no-open", false, "print URL without opening the browser")
	selected := flags.String("session", "", "session to inspect")
	workdir := flags.String("workdir", ".", "initial workspace")
	policyPath := flags.String("policy", "", "intervention policy JSON file")
	raceMode := flags.Bool("race", false, "compare repairs for dashboard sessions")
	noIntervene := flags.Bool("no-intervene", false, "score dashboard sessions without interventions")
	dbPath := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("mulch %s takes no arguments", command)
	}
	workspace, err := filepath.Abs(*workdir)
	if err != nil {
		return err
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("workspace must be a directory")
	}
	if *raceMode && *noIntervene {
		return errors.New("--race cannot be combined with --no-intervene")
	}
	policy, err := configuredPolicy(*policyPath)
	if err != nil {
		return err
	}
	mode := harness.Repair
	if *raceMode {
		mode = harness.Race
	}
	if *noIntervene {
		mode = harness.Observe
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", *address, err)
	}
	defer func() { _ = listener.Close() }()
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	eventBus := bus.New()
	router := &harness.Router{}
	publisher := &fanoutPublisher{}
	publisher.Add(eventBus)
	publisher.Add(router)
	store, err := event.Open(context.WithoutCancel(ctx), *dbPath, publisher)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	if *selected != "" {
		if _, err := store.Session(ctx, *selected); err != nil {
			return fmt.Errorf("session: %w", err)
		}
	}
	var control server.Control
	var manager *session.Manager
	if key := opts.Getenv("MULCH_PROVIDER_API_KEY"); key != "" {
		model := envOr(opts.Getenv, "MULCH_MODEL", defaultModel)
		execute := func(resume bool) session.Runner {
			return func(runCtx context.Context, id, task string, run session.RunOpts, steering *session.Steering) error {
				workdir := run.Workdir
				if workdir == "" {
					workdir = "."
				}
				recorded := event.Session{ID: id, Task: task, Model: model, Workdir: workdir, ContextWindow: 200000}
				if resume {
					var err error
					recorded, err = store.Session(runCtx, id)
					if err != nil {
						return err
					}
				}
				config := runtimeConfig(store, router, opts, key, recorded.Model, recorded.ContextWindow, mode, policy)
				return config.Execute(runCtx, recorded, resume, task, []hook.Hook{steering.Bind(store)}, nil)
			}
		}
		manager = session.New(session.Options{Bus: eventBus, Run: execute(false), Resume: execute(true)})
		control = server.NewControl(manager, store)
		defer func() { _ = manager.Close() }()
	}
	url := "http://" + viewerAddress(listener.Addr().String())
	if *selected != "" {
		url += "?session=" + *selected
	}
	_, _ = fmt.Fprintf(opts.Stdout, "mulch viewer listening on %s\n", url)
	if web && !*noOpen && opts.Getenv("SSH_CONNECTION") == "" && opts.Getenv("SSH_TTY") == "" {
		if err := openBrowser(url); err != nil {
			_, _ = fmt.Fprintf(opts.Stderr, "Open the URL above in your browser (%v).\n", err)
		}
	}
	return server.New(store, control).WithConfig(server.Config{Workspace: workspace, Model: envOr(opts.Getenv, "MULCH_MODEL", defaultModel), Mode: string(mode), Policy: policy, Ready: control != nil}).ServeListener(ctx, listener)
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
	runs := flags.Int("runs", 2, "runs per evaluation mode")
	modesFlag := flags.String("modes", "plain,control,intervention,race", "comma-separated evaluation modes")
	seed := flags.Int64("seed", 1, "reproducible mode-order seed")
	tokenBudget := flags.Int("token-budget", 40000, "per-run token budget across agent, judge, summary, and candidates")
	concurrency := flags.Int("concurrency", 5, "maximum concurrent sessions")
	output := flags.String("output", ".", "report directory")
	model := flags.String("model", envOr(opts.Getenv, "MULCH_MODEL", defaultModel), "model")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("mulch eval requires exactly one scenario file")
	}
	if *runs < 1 || *concurrency < 1 || *tokenBudget < 1 {
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
	var modes []harness.Mode
	for _, m := range strings.Split(*modesFlag, ",") {
		modes = append(modes, harness.Mode(strings.TrimSpace(m)))
	}
	result, err := meval.Run(ctx, scenario, meval.Options{Modes: modes, Seed: *seed, TokenBudget: *tokenBudget, Runs: *runs, Concurrency: *concurrency, OutputDir: *output, Model: *model, LLM: func() provider.LLM { return opts.LLMFactory(key, opts.Getenv("MULCH_PROVIDER_BASE_URL")) }})
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
	defer func() { _ = store.Close() }()
	release, err := store.AcquireExecution(ctx, id)
	if err != nil {
		return err
	}
	defer release()
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
	defer func() { _ = store.Close() }()
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
	defer func() { _ = store.Close() }()
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
	defer func() { _ = store.Close() }()
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
	raceMode := flags.Bool("race", false, "race prune and reanchor candidates for confirmed interventions")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("mulch run requires exactly one prompt")
	}
	if *raceMode && *noIntervene {
		return errors.New("--race cannot be combined with --no-intervene")
	}
	daemonEligible := !*jsonMode && *policyPath == "" && !*noIntervene && !*raceMode && *model == envOr(opts.Getenv, "MULCH_MODEL", defaultModel) && *contextWindow == 200000 && *dbPath == envOr(opts.Getenv, "MULCH_DB", defaultDB())
	if id, detected, daemonErr := submitToDaemon(ctx, opts, flags.Arg(0), *workdir, daemonEligible); detected {
		if daemonErr != nil {
			return daemonErr
		}
		if id != "" {
			_, _ = fmt.Fprintf(opts.Stdout, "session %s\n", id)
		}
		return nil
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
	defer func() { _ = store.Close() }()
	policy := intervene.DefaultPolicy()
	if !*noIntervene {
		var policyErr error
		policy, policyErr = configuredPolicy(*policyPath)
		if policyErr != nil {
			return policyErr
		}
	}
	emit := func(text string) { _, _ = io.WriteString(opts.Stdout, text) }
	if *jsonMode {
		emit = func(string) {}
	}
	router := &harness.Router{}
	publisher.Add(router)
	runMode := harness.Repair
	if *noIntervene {
		runMode = harness.Observe
	}
	if *raceMode {
		runMode = harness.Race
	}
	config := runtimeConfig(store, router, opts, key, *model, *contextWindow, runMode, policy)
	manager := session.New(session.Options{Bus: eventBus, Run: func(runCtx context.Context, id, task string, _ session.RunOpts, steering *session.Steering) error {
		recorded := event.Session{ID: id, Task: task, Model: *model, Workdir: *workdir, ContextWindow: *contextWindow}
		return config.Execute(runCtx, recorded, false, task, []hook.Hook{steering.Bind(store)}, emit)
	}})
	defer func() { _ = manager.Close() }()
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

func runtimeConfig(store *event.SQLiteStore, router *harness.Router, opts Options, key, model string, window int, mode harness.Mode, policy intervene.Policy) harness.Config {
	return harness.Config{Store: store, Router: router, Model: model, JudgeModel: envOr(opts.Getenv, "MULCH_JUDGE_MODEL", model), ContextWindow: window, Mode: mode, Policy: policy, Weights: healthWeights(opts),
		LLM: func(id, role string) provider.LLM {
			return opts.LLMFactory(key, opts.Getenv("MULCH_PROVIDER_BASE_URL"))
		},
		Scorers: func(coherence *score.Coherence) []score.Scorer { return healthScorers(opts, store, coherence) }}
}

func submitToDaemon(ctx context.Context, opts Options, task, workdir string, eligible bool) (string, bool, error) {
	if !eligible {
		return "", false, nil
	}
	baseURL := strings.TrimRight(envOr(opts.Getenv, "MULCH_DAEMON_URL", "http://127.0.0.1:4141"), "/")
	probeCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	probe, err := http.NewRequestWithContext(probeCtx, http.MethodGet, baseURL+"/api/metrics", nil)
	if err != nil {
		return "", false, nil
	}
	response, err := http.DefaultClient.Do(probe)
	if err != nil {
		return "", false, nil
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", false, nil
	}
	request := server.StartRequest{Task: task}
	request.Opts.Workdir = workdir
	body, err := json.Marshal(request)
	if err != nil {
		return "", false, nil
	}
	start, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/sessions", bytes.NewReader(body))
	if err != nil {
		return "", true, err
	}
	start.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(start)
	if err != nil {
		return "", true, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusAccepted {
		return "", true, fmt.Errorf("daemon start: %s", strings.TrimSpace(readResponse(response.Body)))
	}
	var result server.SessionResponse
	if json.NewDecoder(response.Body).Decode(&result) != nil || result.ID == "" {
		return "", true, errors.New("daemon start returned an invalid session ID")
	}
	return result.ID, true, nil
}

func readResponse(reader io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(reader, 4096))
	return string(data)
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
	defer func() { _ = store.Close() }()
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
