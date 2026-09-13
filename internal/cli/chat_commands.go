package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
	harness "github.com/n1tishc/mulch/internal/runtime"
	"github.com/n1tishc/mulch/internal/server"
)

var chatCommands = []struct{ name, description string }{
	{"/help", "Show commands and keyboard shortcuts"},
	{"/new", "Start a fresh session; preserve the previous one"},
	{"/sessions", "List saved sessions"},
	{"/resume", "Reopen a session: /resume ID"},
	{"/session", "Show current session and database"},
	{"/status", "Show model, repair mode, usage, and recorded health"},
	{"/model", "List models or switch this conversation: /model NAME"},
	{"/provider", "List configured providers or switch: /provider NAME"},
	{"/api-key", "Securely update the current provider key"},
	{"/mode", "Set plain, control, intervention, or race for subsequent tasks"},
	{"/rename", "Label this saved session: /rename NAME"},
	{"/history", "Show saved user and assistant messages"},
	{"/diff", "Show tracked Git changes and untracked file names"},
	{"/inspect", "Open this session in the browser; terminal retains control"},
	{"/web", "Open the live browser trace for this conversation"},
	{"/cancel", "Cancel the running task (terminal interface)"},
	{"/exit", "Exit the application"},
}

type chatControl struct {
	store           *event.SQLiteStore
	recorded        *event.Session
	existing        *bool
	config          *harness.Config
	db              string
	inspectorURL    string
	inspectorCancel context.CancelFunc
	inspectorDone   chan error
	provider        string
	providers       []providerProfile
	llmFactory      LLMFactory
	configPath      string
}

func (c *chatControl) fresh(model string) error {
	id, err := event.NewSessionID()
	if err != nil {
		return err
	}
	if c.config.JudgeModel == c.recorded.Model {
		c.config.JudgeModel = model
	}
	*c.recorded = event.Session{ID: id, Model: model, Workdir: c.recorded.Workdir, ContextWindow: c.recorded.ContextWindow}
	*c.existing = false
	c.config.Model = model
	return nil
}

func (c *chatControl) activeProvider() *providerProfile {
	for i := range c.providers {
		if c.providers[i].Name == c.provider {
			return &c.providers[i]
		}
	}
	return nil
}

func (c *chatControl) applyProvider(profile *providerProfile) {
	c.provider = profile.Name
	c.config.Provider = profile.Name
	previous := c.config.Model
	if len(profile.Models) > 0 {
		c.config.Model = profile.Models[0]
	}
	if c.config.JudgeModel == previous {
		c.config.JudgeModel = c.config.Model
	}
	c.config.LLM = func(string, string) provider.LLM { return c.llmFactory(profile.APIKey, profile.BaseURL) }
}

func (c *chatControl) setAPIKey(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("API key cannot be empty")
	}
	profile := c.activeProvider()
	if profile == nil {
		profile = &providerProfile{Name: "default", BaseURL: "", Models: []string{c.config.Model}}
		c.providers = append(c.providers, *profile)
		profile = &c.providers[len(c.providers)-1]
		c.provider = profile.Name
	}
	if c.configPath == "" {
		return errors.New("cannot locate the config file; set HOME, XDG_CONFIG_HOME, or MULCH_CONFIG")
	}
	values, err := readConfig(c.configPath)
	if err != nil {
		return err
	}
	key := "provider." + profile.Name + ".api-key"
	if profile.Name == "default" {
		key = "api-key"
	}
	values[key] = value
	if profile.Name != "default" {
		values["provider"] = profile.Name
	}
	if err = writeConfig(c.configPath, values); err != nil {
		return err
	}
	profile.APIKey = value
	c.applyProvider(profile)
	return nil
}

func (c *chatControl) command(ctx context.Context, input string) (string, bool, error) {
	name, arg, _ := strings.Cut(strings.TrimSpace(input), " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "/inspect", "/web":
		if arg != "" && arg != "--no-open" {
			return "", false, errors.New("usage: /web [--no-open]")
		}
		return c.inspect(ctx, arg == "--no-open")
	case "/exit", "/quit":
		return "", true, nil
	case "/help":
		var out strings.Builder
		for _, command := range chatCommands {
			fmt.Fprintf(&out, "%-12s %s\n", command.name, command.description)
		}
		out.WriteString("\nEnter send · Alt+Enter/Ctrl+J newline · Tab complete command\n↑/↓ prompt history · PgUp/PgDn transcript · Esc/Ctrl+C cancel task\nCtrl+D exit when idle · paste inserts text without sending\n")
		return out.String(), false, nil
	case "/session":
		return fmt.Sprintf("session %s\ndatabase %s\n", c.recorded.ID, c.db), false, nil
	case "/cancel":
		return "No task is running.\n", false, nil
	case "/new", "/clear":
		if err := c.fresh(c.config.Model); err != nil {
			return "", false, err
		}
		return "New session: " + c.recorded.ID + "\nPrevious history remains saved.\n", false, nil
	case "/sessions":
		sessions, err := c.store.Sessions(ctx)
		if err != nil {
			return "", false, err
		}
		var out strings.Builder
		for i := len(sessions) - 1; i >= 0; i-- {
			s := sessions[i]
			fmt.Fprintf(&out, "%s  %-10s  %s  %s\n", s.ID, s.Status, s.Label, clipText(s.Task, 100))
		}
		if len(sessions) == 0 {
			out.WriteString("No saved sessions yet.\n")
		}
		return out.String(), false, nil
	case "/resume":
		if arg == "" {
			return "", false, errors.New("usage: /resume SESSION_ID (use /sessions to list)")
		}
		saved, err := c.store.Session(ctx, arg)
		if err != nil {
			return "", false, err
		}
		if saved.Status == event.StatusRunning {
			return "", false, errors.New("session is running; stop its owner before reopening")
		}
		if c.config.JudgeModel == c.recorded.Model {
			c.config.JudgeModel = saved.Model
		}
		*c.recorded = saved
		*c.existing = true
		c.config.Model, c.config.ContextWindow = saved.Model, saved.ContextWindow
		return fmt.Sprintf("Resumed %s · %s\n%s", saved.ID, saved.Workdir, c.history(ctx)), false, nil
	case "/model":
		if arg == "" {
			var out strings.Builder
			fmt.Fprintf(&out, "Models for %s:\n", c.provider)
			models := []string{c.config.Model}
			if profile := c.activeProvider(); profile != nil && len(profile.Models) > 0 {
				models = profile.Models
			}
			for _, model := range models {
				marker := "  "
				if model == c.config.Model {
					marker = "* "
				}
				fmt.Fprintf(&out, "%s%s\n", marker, model)
			}
			out.WriteString("Switch with /model NAME. The conversation history stays intact.\n")
			return out.String(), false, nil
		}
		previous := c.config.Model
		c.config.Model = arg
		if c.config.JudgeModel == previous {
			c.config.JudgeModel = arg
		}
		return "Now using " + arg + " for the next turn. Conversation history is unchanged.\n", false, nil
	case "/provider":
		if arg == "" {
			if len(c.providers) == 0 {
				return "No saved providers. Configure one with mulch config; /help shows the format.\n", false, nil
			}
			var out strings.Builder
			out.WriteString("Configured providers:\n")
			for _, profile := range c.providers {
				marker := "  "
				if profile.Name == c.provider {
					marker = "* "
				}
				fmt.Fprintf(&out, "%s%s  %d models\n", marker, profile.Name, len(profile.Models))
			}
			out.WriteString("Switch with /provider NAME.\n")
			return out.String(), false, nil
		}
		for i := range c.providers {
			if c.providers[i].Name == arg {
				c.applyProvider(&c.providers[i])
				return fmt.Sprintf("Now using %s with %s. Conversation history is unchanged.\n", arg, c.config.Model), false, nil
			}
		}
		return "", false, fmt.Errorf("provider %q is not configured; use /provider to list choices", arg)
	case "/api-key":
		return "In the interactive terminal, /api-key opens a hidden entry field. For line mode, run mulch config set api-key.\n", false, nil
	case "/mode":
		if arg == "" {
			return "Mode: " + string(c.config.Mode) + "\nChoose plain, control (scoring only), intervention, or race.\n", false, nil
		}
		mode := harness.Mode(arg)
		if !harness.ValidMode(mode) {
			return "", false, errors.New("mode must be plain, control, intervention, or race")
		}
		c.config.Mode = mode
		return "Subsequent tasks use " + arg + ".\n", false, nil
	case "/rename":
		if !*c.existing {
			return "", false, errors.New("send a message before naming this session")
		}
		if arg == "" {
			return "", false, errors.New("usage: /rename NAME")
		}
		if err := c.store.SetLabel(ctx, c.recorded.ID, arg); err != nil {
			return "", false, err
		}
		c.recorded.Label = arg
		return "Session named " + arg + ".\n", false, nil
	case "/history":
		return c.history(ctx), false, nil
	case "/status":
		out := fmt.Sprintf("Session: %s\nProvider: %s\nModel: %s · judge: %s\nMode: %s\nWorkspace: %s\nContext window: %d\n", c.recorded.ID, c.provider, c.config.Model, c.config.JudgeModel, c.config.Mode, c.recorded.Workdir, c.recorded.ContextWindow)
		if !*c.existing {
			return out + "Ready for first message.\n", false, nil
		}
		events, err := c.store.List(ctx, c.recorded.ID, 1)
		if err != nil {
			return "", false, err
		}
		var usage event.SessionEnd
		var health event.ScoreHealth
		partials := 0
		for _, e := range events {
			switch e.Type {
			case event.TypeSessionEnd:
				_ = e.Decode(&usage)
			case event.TypeScoreHealth:
				_ = e.Decode(&health)
			case event.TypeScorePartial:
				partials++
			}
		}
		out += fmt.Sprintf("Last task: %s · cumulative primary tokens: %d input / %d output\n", usage.Status, usage.TotalInputTokens, usage.TotalOutputTokens)
		if health.TurnScored > 0 {
			out += fmt.Sprintf("Recorded health: %.0f at turn %d (may include reused values)\n", health.Composite, health.TurnScored)
		}
		return out + "Incomplete scorer records: " + strconv.Itoa(partials) + "\nPrimary usage excludes judge/summary/candidate costs.\n", false, nil
	case "/diff":
		diffCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var out strings.Builder
		for _, args := range [][]string{{"--no-pager", "diff", "--no-ext-diff", "--no-textconv", "HEAD", "--"}, {"ls-files", "--others", "--exclude-standard"}} {
			cmd := exec.CommandContext(diffCtx, "git", args...)
			cmd.Dir = c.recorded.Workdir
			data, err := cmd.Output()
			if err != nil {
				return "", false, fmt.Errorf("git inspection failed (requires a repository with a commit): %w", err)
			}
			out.WriteString(string(data))
		}
		if out.Len() == 0 {
			return "No tracked changes or untracked files.\n", false, nil
		}
		return clipText(out.String(), 50000), false, nil
	default:
		return "", false, fmt.Errorf("unknown command %q; use /help", name)
	}
}

func (c *chatControl) inspect(ctx context.Context, noOpen bool) (string, bool, error) {
	if _, err := c.store.Session(ctx, c.recorded.ID); err != nil {
		return "", false, errors.New("send a message before inspecting this session")
	}
	if c.inspectorURL == "" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", false, err
		}
		serveCtx, cancel := context.WithCancel(ctx)
		c.inspectorCancel, c.inspectorDone = cancel, make(chan error, 1)
		c.inspectorURL = "http://" + listener.Addr().String()
		viewer := server.New(c.store).WithConfig(server.Config{Workspace: c.recorded.Workdir, Model: c.recorded.Model, Mode: string(c.config.Mode), ReadOnlyReason: "Terminal inspection: send messages and control tasks in the terminal. This viewer closes when the terminal exits."})
		go func() { c.inspectorDone <- viewer.ServeListener(serveCtx, listener) }()
	}
	address := c.inspectorURL + "?session=" + url.QueryEscape(c.recorded.ID) + "&view=trace"
	message := "Inspect this session: " + address + "\nTerminal retains execution control.\n"
	if !noOpen {
		if err := openBrowser(address); err != nil {
			message += "Open the URL manually: " + err.Error() + "\n"
		}
	}
	return message, false, nil
}

func (c *chatControl) closeInspector() {
	if c.inspectorCancel != nil {
		c.inspectorCancel()
		<-c.inspectorDone
	}
}

func (c *chatControl) history(ctx context.Context) string {
	if !*c.existing {
		return "No saved messages yet.\n"
	}
	events, err := c.store.List(ctx, c.recorded.ID, 1)
	if err != nil {
		return "Could not load history: " + err.Error() + "\n"
	}
	var out strings.Builder
	for _, e := range events {
		switch e.Type {
		case event.TypeUserMessage:
			var m event.UserMessage
			if e.Decode(&m) == nil {
				fmt.Fprintf(&out, "\nYou\n%s\n", m.Text)
			}
		case event.TypeAssistantMessage:
			var m event.AssistantMessage
			if e.Decode(&m) == nil {
				fmt.Fprintf(&out, "\nMulch\n%s\n", m.Text)
			}
		}
	}
	return clipText(out.String(), 50000)
}

func clipText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "\n… (display truncated)"
	}
	return text
}
