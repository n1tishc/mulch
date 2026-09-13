package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/n1tishc/mulch/internal/event"
	harness "github.com/n1tishc/mulch/internal/runtime"
)

// chat keeps the conversation alive while each task uses the shared runtime.
func chat(ctx context.Context, args []string, opts Options) error {
	flags := flag.NewFlagSet("chat", flag.ContinueOnError)
	flags.SetOutput(opts.Stderr)
	db := flags.String("db", envOr(opts.Getenv, "MULCH_DB", defaultDB()), "event database")
	workdir := flags.String("workdir", ".", "working directory for a new session")
	resumeID := flags.String("resume", "", "reopen a saved session")
	model := flags.String("model", envOr(opts.Getenv, "MULCH_MODEL", defaultModel), "model for a new session")
	window := flags.Int("context-window", 200000, "context window for a new session")
	policyPath := flags.String("policy", "", "intervention policy JSON file")
	race := flags.Bool("race", false, "compare repairs")
	observe := flags.Bool("no-intervene", false, "score without interventions")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("mulch chat takes no positional arguments; enter messages at the prompt")
	}
	if *race && *observe {
		return errors.New("--race cannot be combined with --no-intervene")
	}
	if *window <= 0 {
		return errors.New("--context-window must be positive")
	}
	key := opts.Getenv("MULCH_PROVIDER_API_KEY")
	profiles, activeProvider, err := configuredProviderProfiles(opts.Getenv)
	if err != nil {
		return err
	}
	policy, err := configuredPolicy(*policyPath)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(*db), 0700); err != nil {
		return err
	}
	router := &harness.Router{}
	// Provider deltas and committed tool events can arrive concurrently.
	output := &chatOutput{writer: opts.Stdout}
	publisher := &fanoutPublisher{}
	publisher.Add(router)
	publisher.Add(output)
	store, err := event.Open(context.WithoutCancel(ctx), *db, publisher)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	wd, err := filepath.Abs(*workdir)
	if err != nil {
		return err
	}
	recorded := event.Session{Model: *model, Workdir: wd, ContextWindow: *window}
	existing := *resumeID != ""
	if existing {
		recorded, err = store.Session(ctx, *resumeID)
		if err != nil {
			return err
		}
		if recorded.Status == event.StatusRunning {
			return errors.New("session is already running; stop its owner before reopening it")
		}
	} else {
		info, statErr := os.Stat(wd)
		if statErr != nil {
			return statErr
		}
		if !info.IsDir() {
			return errors.New("--workdir must be a directory")
		}
		wd, err = filepath.EvalSymlinks(wd)
		if err != nil {
			return err
		}
		recorded.Workdir = wd
		recorded.ID, err = event.NewSessionID()
		if err != nil {
			return err
		}
	}
	mode := harness.Repair
	if *observe {
		mode = harness.Observe
	}
	if *race {
		mode = harness.Race
	}
	config := runtimeConfig(store, router, opts, key, recorded.Model, recorded.ContextWindow, mode, policy)
	config.Provider = activeProvider
	control := &chatControl{store: store, recorded: &recorded, existing: &existing, config: &config, db: *db, provider: activeProvider, providers: profiles, llmFactory: opts.LLMFactory, configPath: configPath(opts.Getenv)}
	defer control.closeInspector()
	if chatTerminal(opts) {
		return runTerminalChat(ctx, opts, control, output)
	}
	output.print(fmt.Sprintf("Mulch chat · %s\nWorking directory: %s\n/help for commands; Ctrl+C interrupts a task; /exit or Ctrl+D exits.\n", recorded.ID, recorded.Workdir))
	// One reader owns stdin. Request-driven reads avoid consuming follow-ups
	// while the agent is working. A blocked terminal read ends with the process.
	readCtx, stopReader := context.WithCancel(ctx)
	defer stopReader()
	requests := make(chan struct{})
	type input struct {
		text string
		err  error
	}
	lines := make(chan input)
	go func() {
		scanner := bufio.NewScanner(opts.Stdin)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for {
			select {
			case <-readCtx.Done():
				return
			case <-requests:
			}
			value := input{}
			if scanner.Scan() {
				value.text = scanner.Text()
			} else {
				value.err = scanner.Err()
				if value.err == nil {
					value.err = io.EOF
				}
			}
			select {
			case <-readCtx.Done():
				return
			case lines <- value:
			}
			if value.err != nil {
				return
			}
		}
	}()
	for {
		output.print("\nmulch> ")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case requests <- struct{}{}:
		}
		var value input
	read:
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-opts.Interrupts:
				output.print("\nUse /exit or Ctrl+D to leave.\nmulch> ")
			case value = <-lines:
				break read
			}
		}
		if errors.Is(value.err, io.EOF) {
			output.print("\n")
			return output.err()
		}
		if value.err != nil {
			return value.err
		}
		task := strings.TrimSpace(value.text)
		if strings.HasPrefix(task, "/") {
			text, exit, commandErr := control.command(ctx, task)
			if commandErr != nil {
				output.print(fmt.Sprintf("%v\n", commandErr))
			} else {
				output.print(text)
			}
			if exit {
				return output.err()
			}
			continue
		}
		switch task {
		case "":
			continue
		}
		if profile := control.activeProvider(); control.llmFactory != nil && (profile == nil || profile.APIKey == "") {
			output.print("Provider API key is missing. Use /api-key in the interactive terminal or run mulch config set api-key.\n")
			continue
		}
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- config.Execute(runCtx, recorded, existing, task, nil, output.print) }()
		var runErr error
	working:
		for {
			select {
			case runErr = <-done:
				break working
			case <-opts.Interrupts:
				cancel()
			case <-ctx.Done():
				cancel()
				runErr = <-done
				break working
			}
		}
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if saved, loadErr := store.Session(ctx, recorded.ID); loadErr == nil {
			recorded = saved
			existing = true
		}
		if runErr != nil {
			output.print(fmt.Sprintf("\nTask stopped: %v\n", runErr))
		}
		if err = output.err(); err != nil {
			return err
		}
	}
}

type chatOutput struct {
	mu      sync.Mutex
	writer  io.Writer
	failure error
	onText  func(string)
}

func (o *chatOutput) print(text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.onText != nil {
		o.onText(text)
		return
	}
	if o.failure == nil {
		_, o.failure = io.WriteString(o.writer, text)
	}
}

func (o *chatOutput) err() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.failure
}

func (o *chatOutput) Publish(e event.Event) {
	if e.Type == event.TypeToolStart {
		var start event.ToolStart
		if e.Decode(&start) == nil {
			o.print(fmt.Sprintf("\n[tool: %s]\n", start.Name))
		}
	}
	if e.Type == event.TypeToolResult {
		var result event.ToolResult
		if e.Decode(&result) == nil {
			status := "done"
			if result.IsError || result.Cancelled || result.TimedOut {
				status = "failed"
			}
			o.print(fmt.Sprintf("\n[%s: %s · %dms]\n%s\n", result.Name, status, result.DurationMS, clipText(result.Output, 1200)))
		}
	}
	if e.Type == event.TypeScorePartial {
		var partial event.ScorePartial
		if e.Decode(&partial) == nil {
			o.print(fmt.Sprintf("\n[scoring incomplete: %s · reused previous: %t]\n", partial.Name, partial.UsedPrevious))
		}
	}
	if e.Type == event.TypeInterveneFire {
		var fire event.InterveneFire
		if e.Decode(&fire) == nil {
			o.print(fmt.Sprintf("\n[repair: %s · %s]\n", fire.Action, fire.Reason))
		}
	}
}
