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
	secretInput           bool
}

func runTerminalChat(ctx context.Context, opts Options, control *chatControl, output *chatOutput) error {
	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &terminalChat{ctx: appCtx, control: control, output: output, width: 80, height: 24}
	m.transcript = "Describe a task to get started.\n\nMulch can read, edit, and run tools in this directory.\nOpen /web during a task to follow the execution trace.\n"
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
	if m.scroll > 0 {
		width := max(16, min(m.width-4, 110))
		before := strings.Count(ansi.Hardwrap(m.transcript, width, true), "\n")
		after := strings.Count(ansi.Hardwrap(m.transcript+terminalSafe(text), width, true), "\n")
		m.scroll += after - before
	}
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
	if strings.HasPrefix(text, "/") {
		m.append("\nYou › " + text + "\n")
		if m.busy {
			switch text {
			case "/inspect", "/inspect --no-open", "/web", "/web --no-open":
				result, _, err := m.control.command(m.ctx, text)
				if err != nil {
					m.append(err.Error() + "\n")
				} else {
					m.append(result)
				}
			case "/cancel":
				m.queued = nil
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
		if text == "/api-key" {
			m.secretInput = true
			m.append("Enter the API key for " + m.control.provider + ". Input is hidden; press Enter to save or Esc to cancel.\n")
			return nil
		}
		result, exit, err := m.control.command(m.ctx, text)
		if err != nil {
			m.append(err.Error() + "\n")
		} else {
			if text == "/new" || text == "/clear" {
				m.transcript = ""
			}
			m.append(result)
		}
		if exit {
			return tea.Quit
		}
		return nil
	}
	m.append("\nYou › " + text + "\n")
	if m.busy {
		m.queued = append(m.queued, text)
		m.append("Queued for the next task.\n")
		return nil
	}
	if profile := m.control.activeProvider(); m.control.llmFactory != nil && (profile == nil || profile.APIKey == "") {
		m.append("No API key is configured for this provider. Use /api-key.\n")
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
				if m.secretInput {
					m.secretInput = false
					m.append("API key entry cancelled.\n")
				}
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
			if m.secretInput {
				m.input = nil
				m.cursor = 0
				m.secretInput = false
				if err := m.control.setAPIKey(text); err != nil {
					m.append("Could not save API key: " + err.Error() + "\n")
				} else {
					m.append("API key saved for " + m.control.provider + " and applied.\n")
				}
				return m, nil
			}
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
	width := max(16, min(m.width-4, 110))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("108"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	state := "ready"
	if m.busy {
		state = fmt.Sprintf("%s working %s · Esc stop · %d queued", []string{"◐", "◓", "◑", "◒"}[m.frame%4], time.Since(m.started).Round(time.Second), len(m.queued))
	}
	identity := m.control.config.Model
	if m.control.provider != "" {
		identity = m.control.provider + "/" + identity
	}
	header := accent.Bold(true).Render("mulch") + "  " + terminalSafe(identity) + dim.Render("  ·  "+string(m.control.config.Mode))
	header = ansi.Truncate(header, width, "…") + "\n" + dim.Render(ansi.Truncate(terminalSafe(m.control.recorded.Workdir), width, "…"))
	before := terminalSafe(string(m.input[:m.cursor]))
	after := terminalSafe(string(m.input[m.cursor:]))
	if m.secretInput {
		before = strings.Repeat("•", len([]rune(before)))
		after = strings.Repeat("•", len([]rune(after)))
	}
	cursor := " "
	if len(after) > 0 {
		chars := []rune(after)
		if chars[0] != '\n' {
			cursor = string(chars[0])
			after = string(chars[1:])
		}
	}
	editor := before + lipgloss.NewStyle().Reverse(true).Render(cursor) + after
	if len(m.input) == 0 {
		placeholder := " Describe a task or type / for commands"
		if m.secretInput {
			placeholder = " Paste or type API key (hidden)"
		}
		editor += dim.Render(placeholder)
	}
	inputLines := strings.Split(ansi.Hardwrap(editor, width-2, true), "\n")
	// Show the region around the cursor when composing a long multiline prompt.
	cursorLine := len(strings.Split(ansi.Hardwrap(before+" ", width-2, true), "\n")) - 1
	editorHeight := min(5, max(1, m.height-10))
	start := max(0, cursorLine-editorHeight+1)
	end := min(len(inputLines), start+editorHeight)
	inputLines = inputLines[start:end]
	editor = accent.Render("› ") + strings.Join(inputLines, "\n  ")
	suggestions := "Enter send · Alt+Enter newline · / commands · /web trace"
	menu := ""
	menuLines := 0
	if matches := m.matches(); len(matches) > 0 {
		selected := min(m.selection, len(matches)-1)
		first := max(0, selected-3)
		menuHeight := min(4, max(0, m.height-8-len(inputLines)))
		for _, name := range matches[first:min(len(matches), first+menuHeight)] {
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
	if m.scroll > 0 {
		state += " · reading history · PgDn for latest"
	}
	footer := dim.Render(ansi.Truncate(suggestions, width, "…"))
	bodyHeight := max(1, m.height-7-len(inputLines)-menuLines)
	lines := strings.Split(ansi.Hardwrap(m.transcript, width, true), "\n")
	scroll := min(m.scroll, max(0, len(lines)-bodyHeight))
	bottom := len(lines) - scroll
	top := max(0, bottom-bodyHeight)
	body := renderTerminalTranscript(strings.Join(lines[top:bottom], "\n"), accent, dim)
	for n := bottom - top; n < bodyHeight; n++ {
		body += "\n"
	}
	return lipgloss.NewStyle().Padding(0, 2).Render(header + "\n" + dim.Render(strings.Repeat("─", width)) + "\n" + body + "\n" + accent.Render(ansi.Truncate(state, width, "…")) + "\n" + dim.Render(strings.Repeat("─", width)) + "\n" + editor + "\n" + menu + footer)
}

func renderTerminalTranscript(text string, accent, dim lipgloss.Style) string {
	assistant := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	user := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("108"))
	code := lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Background(lipgloss.Color("236"))
	lines := strings.Split(text, "\n")
	inCode := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			inCode = !inCode
			lines[i] = dim.Render(strings.Repeat("─", min(32, max(8, len(trimmed)+8))))
		case inCode:
			lines[i] = code.Render(line)
		case strings.HasPrefix(line, "You ›"):
			lines[i] = user.Render("YOU") + accent.Render("  › ") + strings.TrimSpace(strings.TrimPrefix(line, "You ›"))
		case trimmed == "Mulch":
			lines[i] = assistant.Render("MULCH")
		case strings.HasPrefix(trimmed, "[tool:") || strings.HasPrefix(trimmed, "[done:") || strings.HasPrefix(trimmed, "[failed:"):
			lines[i] = dim.Render(trimmed)
		case strings.HasPrefix(trimmed, "### "):
			lines[i] = assistant.Render(strings.TrimPrefix(trimmed, "### "))
		case strings.HasPrefix(trimmed, "## "):
			lines[i] = assistant.Bold(true).Render(strings.TrimPrefix(trimmed, "## "))
		case strings.HasPrefix(trimmed, "# "):
			lines[i] = accent.Bold(true).Render(strings.TrimPrefix(trimmed, "# "))
		case strings.HasPrefix(trimmed, "- "):
			lines[i] = accent.Render("•") + " " + renderTerminalInline(strings.TrimPrefix(trimmed, "- "))
		default:
			lines[i] = renderTerminalInline(line)
		}
	}
	return strings.Join(lines, "\n")
}

func renderTerminalInline(line string) string {
	strong := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	inlineCode := lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("238"))
	var out strings.Builder
	for len(line) > 0 {
		boldAt, codeAt := strings.Index(line, "**"), strings.Index(line, "`")
		if boldAt >= 0 && (codeAt < 0 || boldAt < codeAt) {
			out.WriteString(line[:boldAt])
			line = line[boldAt+2:]
			if end := strings.Index(line, "**"); end >= 0 {
				out.WriteString(strong.Render(line[:end]))
				line = line[end+2:]
				continue
			}
			out.WriteString("**")
			continue
		}
		if codeAt >= 0 {
			out.WriteString(line[:codeAt])
			line = line[codeAt+1:]
			if end := strings.Index(line, "`"); end >= 0 {
				out.WriteString(inlineCode.Render(" " + line[:end] + " "))
				line = line[end+1:]
				continue
			}
			out.WriteString("`")
			continue
		}
		out.WriteString(line)
		break
	}
	return out.String()
}
