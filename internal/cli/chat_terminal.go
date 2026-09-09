package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-isatty"
)

func chatTerminal(opts Options) bool {
	input, inputOK := opts.Stdin.(*os.File)
	output, outputOK := opts.Stdout.(*os.File)
	return inputOK && outputOK && isatty.IsTerminal(input.Fd()) && isatty.IsTerminal(output.Fd()) && opts.Getenv("TERM") != "dumb"
}

type terminalText string
type terminalDone struct{ err error }
type terminalTick time.Time

type terminalChat struct {
	ctx                   context.Context
	control               *chatControl
	output                *chatOutput
	program               *tea.Program
	workers               sync.WaitGroup
	cancel                context.CancelFunc
	busy, quitting        bool
	width, height, scroll int
	input                 []rune
	cursor                int
	history               []string
	historyIndex          int
	draft                 string
	transcript            string
	queued                []string
	started               time.Time
	frame                 int
	selection             int
}

func runTerminalChat(ctx context.Context, opts Options, control *chatControl, output *chatOutput) error {
	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &terminalChat{ctx: appCtx, control: control, output: output, width: 80, height: 24}
	m.transcript = "Welcome to Mulch\n\nDescribe a coding task. Mulch can read, edit, and run tools in this workspace.\nType / for commands. /help lists controls.\n"
	if *control.existing {
		m.transcript += "\n" + control.history(ctx)
	}
	p := tea.NewProgram(m, tea.WithContext(appCtx), tea.WithInput(opts.Stdin), tea.WithOutput(opts.Stdout), tea.WithAltScreen(), tea.WithoutSignalHandler())
	m.program = p
	output.onText = func(text string) { p.Send(terminalText(text)) }
	_, err := p.Run()
	cancel()
	m.workers.Wait()
	output.onText = nil
	if *control.existing {
		_, _ = fmt.Fprintf(opts.Stdout, "\nSession: %s\nReopen: mulch chat --db %q --resume %s\n", control.recorded.ID, control.db, control.recorded.ID)
	} else {
		_, _ = fmt.Fprintln(opts.Stdout, "\nNo messages sent in this session; nothing new was saved.")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (m *terminalChat) Init() tea.Cmd { return nil }

func tickTerminal() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return terminalTick(t) })
}

func (m *terminalChat) append(text string) {
	// Keep the screen bounded; the complete event log remains in SQLite.
	m.transcript += terminalSafe(text)
	if len(m.transcript) > 200000 {
		cut := len(m.transcript) - 150000
		if newline := strings.IndexByte(m.transcript[cut:], '\n'); newline >= 0 {
			cut += newline + 1
		}
		m.transcript = "[Earlier display omitted; use /history or replay for saved events.]\n" + m.transcript[cut:]
	}
}

func terminalSafe(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, ansi.Strip(text))
}

func (m *terminalChat) submit(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	m.scroll = 0
	m.append("\nYou › " + text + "\n")
	if strings.HasPrefix(text, "/") {
		if m.busy {
			switch text {
			case "/inspect", "/inspect --no-open":
				result, _, err := m.control.command(m.ctx, text)
				if err != nil {
					m.append(err.Error() + "\n")
				} else {
					m.append(result)
				}
			case "/cancel":
				m.cancel()
				m.append("Cancelling task…\n")
			case "/exit", "/quit":
				m.quitting = true
				m.queued = nil
				m.cancel()
			default:
				m.append("Wait for completion or press Esc to stop before changing session controls.\n")
			}
			return nil
		}
		if text == "/cancel" {
			m.append("No task is running.\n")
			return nil
		}
		result, exit, err := m.control.command(m.ctx, text)
		if err != nil {
			m.append(err.Error() + "\n")
		} else {
			m.append(result)
		}
		if exit {
			return tea.Quit
		}
		return nil
	}
	if m.busy {
		m.queued = append(m.queued, text)
		m.append("Queued for the next task.\n")
		return nil
	}
	m.busy = true
	m.started = time.Now()
	runCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	config, recorded, existing := *m.control.config, *m.control.recorded, *m.control.existing
	m.append("\nMulch\n")
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		err := config.Execute(runCtx, recorded, existing, text, nil, m.output.print)
		cancel()
		m.program.Send(terminalDone{err})
	}()
	return tickTerminal()
}

func (m *terminalChat) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(20, msg.Width), max(8, msg.Height)
	case terminalText:
		m.append(string(msg))
	case terminalTick:
		m.frame++
		if m.busy {
			return m, tickTerminal()
		}
	case terminalDone:
		m.busy = false
		if saved, err := m.control.store.Session(m.ctx, m.control.recorded.ID); err == nil {
			*m.control.recorded = saved
			*m.control.existing = true
		}
		if msg.err != nil {
			m.append("\nTask stopped: " + msg.err.Error() + "\n")
		} else {
			m.append("\nTask finished.\n")
		}
		if m.quitting {
			return m, tea.Quit
		}
		if len(m.queued) > 0 {
			next := m.queued[0]
			m.queued = m.queued[1:]
			return m, m.submit(next)
		}
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			if m.busy {
				m.queued = nil
				m.cancel()
				m.append("\nCancelling task…\n")
			} else {
				m.input = nil
				m.cursor = 0
			}
		case tea.KeyCtrlD:
			if m.busy {
				m.quitting = true
				m.queued = nil
				m.cancel()
			} else if len(m.input) == 0 {
				return m, tea.Quit
			}
		case tea.KeyPgUp:
			m.scroll += max(1, m.height/2)
		case tea.KeyPgDown:
			m.scroll = max(0, m.scroll-max(1, m.height/2))
		case tea.KeyTab:
			matches := m.matches()
			if len(matches) > 0 {
				m.input = []rune(matches[min(m.selection, len(matches)-1)])
				m.cursor = len(m.input)
				m.selection = 0
			}
		case tea.KeyEnter:
			if msg.Alt {
				m.insert([]rune{'\n'})
				break
			}
			text := string(m.input)
			if matches := m.matches(); len(matches) > 0 {
				text = matches[min(m.selection, len(matches)-1)]
			}
			m.input = nil
			m.cursor = 0
			if strings.TrimSpace(text) != "" {
				m.history = append(m.history, text)
				m.historyIndex = len(m.history)
				m.draft = ""
			}
			return m, m.submit(text)
		case tea.KeyCtrlJ:
			m.insert([]rune{'\n'})
		case tea.KeyRunes:
			m.insert(msg.Runes)
		case tea.KeySpace:
			m.insert([]rune{' '})
		case tea.KeyLeft:
			m.cursor = max(0, m.cursor-1)
		case tea.KeyRight:
			m.cursor = min(len(m.input), m.cursor+1)
		case tea.KeyHome, tea.KeyCtrlA:
			m.cursor = 0
		case tea.KeyEnd, tea.KeyCtrlE:
			m.cursor = len(m.input)
		case tea.KeyCtrlU:
			m.input = append([]rune(nil), m.input[m.cursor:]...)
			m.cursor = 0
		case tea.KeyCtrlK:
			m.input = m.input[:m.cursor]
		case tea.KeyBackspace, tea.KeyCtrlH:
			if m.cursor > 0 {
				m.input = append(m.input[:m.cursor-1], m.input[m.cursor:]...)
				m.cursor--
			}
		case tea.KeyDelete:
			if m.cursor < len(m.input) {
				m.input = append(m.input[:m.cursor], m.input[m.cursor+1:]...)
			}
		case tea.KeyUp:
			if matches := m.matches(); len(matches) > 0 {
				m.selection = max(0, m.selection-1)
				break
			}
			if m.historyIndex > 0 {
				if m.historyIndex == len(m.history) {
					m.draft = string(m.input)
				}
				m.historyIndex--
				m.input = []rune(m.history[m.historyIndex])
				m.cursor = len(m.input)
			}
		case tea.KeyDown:
			if matches := m.matches(); len(matches) > 0 {
				m.selection = min(len(matches)-1, m.selection+1)
				break
			}
			if m.historyIndex < len(m.history) {
				m.historyIndex++
				text := m.draft
				if m.historyIndex < len(m.history) {
					text = m.history[m.historyIndex]
				}
				m.input = []rune(text)
				m.cursor = len(m.input)
			}
		}
	}
	return m, nil
}

func (m *terminalChat) insert(runes []rune) {
	m.selection = 0
	clean := []rune(terminalSafe(string(runes)))
	if len(m.input)+len(clean) > 100000 {
		m.append("\nPrompt exceeds 100,000 characters; attach context through file-reading instructions.\n")
		return
	}
	tail := append([]rune(nil), m.input[m.cursor:]...)
	m.input = append(append(m.input[:m.cursor], clean...), tail...)
	m.cursor += len(clean)
}

func (m *terminalChat) matches() []string {
	text := string(m.input)
	if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, " \n") {
		return nil
	}
	var matches []string
	for _, command := range chatCommands {
		if strings.HasPrefix(command.name, text) {
			matches = append(matches, command.name)
		}
	}
	return matches
}

func (m *terminalChat) View() string {
	width := max(16, m.width-4)
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	state := "ready"
	if m.busy {
		state = fmt.Sprintf("%s working %s · Esc stop · %d queued", []string{"◐", "◓", "◑", "◒"}[m.frame%4], time.Since(m.started).Round(time.Second), len(m.queued))
	}
	header := accent.Bold(true).Render("MULCH") + "  " + terminalSafe(m.control.recorded.Model) + "  ·  " + string(m.control.config.Mode)
	header = ansi.Truncate(header, width, "…") + "\n" + dim.Render(ansi.Truncate(terminalSafe(m.control.recorded.Workdir), width, "…"))
	before := terminalSafe(string(m.input[:m.cursor]))
	after := terminalSafe(string(m.input[m.cursor:]))
	editor := before + lipgloss.NewStyle().Reverse(true).Render(" ") + after
	if len(m.input) == 0 {
		editor += dim.Render(" Describe a task or type / for commands")
	}
	inputLines := strings.Split(ansi.Hardwrap(editor, width-2, true), "\n")
	// Show the region around the cursor when composing a long multiline prompt.
	cursorLine := len(strings.Split(ansi.Hardwrap(before+" ", width-2, true), "\n")) - 1
	start := max(0, cursorLine-3)
	end := min(len(inputLines), start+5)
	inputLines = inputLines[start:end]
	editor = accent.Render("› ") + strings.Join(inputLines, "\n  ")
	suggestions := "Enter send · Alt+Enter newline · Tab commands · PgUp/PgDn scroll · Ctrl+D exit"
	menu := ""
	menuLines := 0
	if matches := m.matches(); len(matches) > 0 {
		selected := min(m.selection, len(matches)-1)
		first := max(0, selected-3)
		for _, name := range matches[first:min(len(matches), first+4)] {
			description := ""
			for _, command := range chatCommands {
				if command.name == name {
					description = command.description
					break
				}
			}
			line := ansi.Truncate(fmt.Sprintf("%-12s %s", name, description), width, "…")
			if name == matches[selected] {
				line = accent.Reverse(true).Render(line)
			}
			menu += line + "\n"
			menuLines++
		}
		suggestions = "↑/↓ choose · Tab complete · Enter run · Esc dismiss"
	}
	footer := dim.Render(ansi.Truncate(suggestions, width, "…"))
	bodyHeight := max(1, m.height-8-len(inputLines)-menuLines)
	lines := strings.Split(ansi.Hardwrap(m.transcript, width, true), "\n")
	scroll := min(m.scroll, max(0, len(lines)-bodyHeight))
	bottom := len(lines) - scroll
	top := max(0, bottom-bodyHeight)
	body := strings.Join(lines[top:bottom], "\n")
	for n := bottom - top; n < bodyHeight; n++ {
		body += "\n"
	}
	return lipgloss.NewStyle().Padding(0, 2).Render(header + "\n" + dim.Render(strings.Repeat("─", width)) + "\n" + body + "\n" + accent.Render(state) + "\n" + dim.Render(strings.Repeat("─", width)) + "\n" + editor + "\n" + menu + footer)
}
