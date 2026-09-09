package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
	harness "github.com/n1tishc/mulch/internal/runtime"
)

func TestTerminalEditorPasteUnicodeAndMultiline(t *testing.T) {
	m := &terminalChat{}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix 日本\nthen test"), Paste: true})
	if m.busy || string(m.input) != "fix 日本\nthen test" {
		t.Fatal("paste submitted or corrupted the prompt")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if string(m.input) != "f\nx 日本\nthen test" {
		t.Fatalf("editor: %q", string(m.input))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if len(m.input) != 0 {
		t.Fatal("Ctrl+C did not clear idle editor")
	}
}

func TestTerminalHistoryRestoresDraftAndCommandCompletion(t *testing.T) {
	m := &terminalChat{history: []string{"first", "second"}, historyIndex: 2}
	m.insert([]rune("draft"))
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if string(m.input) != "second" {
		t.Fatal("history selection")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if string(m.input) != "draft" {
		t.Fatal("draft lost")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m.insert([]rune("/res"))
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if string(m.input) != "/resume" {
		t.Fatal("command completion")
	}
}

func TestTerminalCancelClearsQueuedFollowups(t *testing.T) {
	cancelled := false
	m := &terminalChat{busy: true, cancel: func() { cancelled = true }, queued: []string{"next"}}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled || len(m.queued) != 0 {
		t.Fatal("cancel left queued work")
	}
	if !strings.Contains(m.transcript, "Cancelling") {
		t.Fatal("missing feedback")
	}
}

func TestTerminalOutputStripsControlSequences(t *testing.T) {
	got := terminalSafe("normal\x1b[2J\x1b]52;c;ZXZpbA==\a\r\ntext")
	if got != "normal\ntext" {
		t.Fatalf("terminal control escaped filtering: %q", got)
	}
}

type terminalToolLLM struct{ requests chan provider.Request }

func (l terminalToolLLM) Stream(_ context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	l.requests <- req
	if len(req.Messages) == 2 {
		return provider.Response{Blocks: []provider.Block{{Type: "tool_use", CallID: "write-test", Name: "write", Input: `{"path":"result.txt","content":"tool executed"}`}}}, nil
	}
	out <- provider.Delta{Text: "Task complete."}
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: "Task complete."}}}, nil
}

func TestTerminalRunsToolsAndKeepsFollowupContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	router := &harness.Router{}
	output := &chatOutput{writer: io.Discard}
	publisher := &fanoutPublisher{}
	publisher.Add(router)
	publisher.Add(output)
	store, err := event.Open(ctx, filepath.Join(t.TempDir(), "ui.db"), publisher)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	wd := t.TempDir()
	recorded := event.Session{ID: "terminal-test", Model: "fake", Workdir: wd, ContextWindow: 200000}
	existing := false
	requests := make(chan provider.Request, 10)
	config := harness.Config{Store: store, Router: router, Mode: harness.Plain, Model: "fake", ContextWindow: 200000, LLM: func(string, string) provider.LLM { return terminalToolLLM{requests} }}
	m := &terminalChat{ctx: ctx, control: &chatControl{store: store, recorded: &recorded, existing: &existing, config: &config}, output: output, width: 80, height: 24}
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	m.program = p
	output.onText = func(text string) { p.Send(terminalText(text)) }
	finished := make(chan error, 1)
	go func() { _, runErr := p.Run(); finished <- runErr }()
	defer func() { cancel(); p.Kill(); m.workers.Wait() }()
	send := func(text string) {
		p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
		p.Send(tea.KeyMsg{Type: tea.KeyEnter})
	}
	waitForEnds := func(want int) {
		t.Helper()
		for {
			events, listErr := store.List(ctx, "terminal-test", 1)
			if listErr == nil {
				count := 0
				for _, e := range events {
					if e.Type == event.TypeSessionEnd {
						count++
					}
				}
				if count >= want {
					return
				}
			}
			select {
			case <-ctx.Done():
				t.Fatal("terminal task did not finish")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	send("write the result")
	waitForEnds(1)
	send("explain the result")
	waitForEnds(2)
	send("/exit")
	select {
	case runErr := <-finished:
		if runErr != nil {
			t.Fatal(runErr)
		}
	case <-ctx.Done():
		t.Fatal("terminal did not exit")
	}
	m.workers.Wait()
	data, err := os.ReadFile(filepath.Join(wd, "result.txt"))
	if err != nil || string(data) != "tool executed" {
		t.Fatalf("tool did not execute: %q %v", data, err)
	}
	if len(requests) != 3 {
		t.Fatalf("model calls = %d", len(requests))
	}
	<-requests
	<-requests
	followup := <-requests
	if len(followup.Messages) < 6 || followup.Messages[len(followup.Messages)-1].Blocks[0].Text != "explain the result" {
		t.Fatal("follow-up lost context")
	}
	if !strings.Contains(m.transcript, "write: done") || !strings.Contains(m.transcript, "Task complete.") {
		t.Fatal("tool and response did not reach transcript")
	}
}
