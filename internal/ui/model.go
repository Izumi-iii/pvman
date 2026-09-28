package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tkzzzzzz6/pvman/internal/conda"
	"github.com/tkzzzzzz6/pvman/internal/uv"
)

type appState int

const (
	stateList appState = iota
	stateLoadingDetails
	stateCreate
	stateCreating
	stateDeleteConfirm
	stateDeleting
)

type itemKind int

const (
	kindHeader itemKind = iota
	kindEnv
)

type listItem struct {
	kind    itemKind
	label   string // header label or env name
	envType string // "conda" or "uv"
	idx     int    // index into condaEnvs or uvEnvs
}

// messages
type envsLoadedMsg struct {
	condaEnvs []conda.Env
	uvEnvs    []uv.Env
	err       error
}

type detailsLoadedMsg struct {
	envType string
	idx     int
	cenv    conda.Env
	uenv    uv.Env
}

type envCreatedMsg struct{ err error }
type envDeletedMsg struct{ err error }
type activationFinishedMsg struct{ err error }

type Model struct {
	state     appState
	condaEnvs []conda.Env
	uvEnvs    []uv.Env
	items     []listItem
	cursor    int
	width     int
	height    int
	cwd       string

	spinner spinner.Model

	// create form
	createInputs [2]textinput.Model // 0=name, 1=python version
	createFocus  int

	// status message
	statusMsg string
	statusErr bool

	// delete confirm target
	deleteTarget listItem
}

func New() Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	nameInput := textinput.New()
	nameInput.Placeholder = "env-name"
	nameInput.Focus()
	nameInput.CharLimit = 64

	verInput := textinput.New()
	verInput.Placeholder = "3.12  (leave blank for default)"
	verInput.CharLimit = 16

	cwd, _ := os.Getwd()

	return Model{
		state:        stateList,
		spinner:      sp,
		createInputs: [2]textinput.Model{nameInput, verInput},
		cwd:          cwd,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, loadEnvsCmd(m.cwd))
}

// ── commands ──────────────────────────────────────────────────────────────────

func loadEnvsCmd(cwd string) tea.Cmd {
	return func() tea.Msg {
		cenvs, err := conda.ListEnvs()
		if err != nil {
			cenvs = nil
		}
		uenvs, _ := uv.ScanDir(cwd)
		return envsLoadedMsg{condaEnvs: cenvs, uvEnvs: uenvs, err: err}
	}
}

func loadDetailsCmd(envType string, idx int, cenv conda.Env, uenv uv.Env) tea.Cmd {
	return func() tea.Msg {
		if envType == "conda" {
			conda.LoadDetails(&cenv)
			return detailsLoadedMsg{envType: "conda", idx: idx, cenv: cenv}
		}
		uv.LoadDetails(&uenv)
		return detailsLoadedMsg{envType: "uv", idx: idx, uenv: uenv}
	}
}

func createEnvCmd(cwd, name, ver string) tea.Cmd {
	return func() tea.Msg {
		return envCreatedMsg{err: uv.CreateEnv(cwd, name, ver)}
	}
}

func deleteEnvCmd(item listItem, cenvs []conda.Env, uenvs []uv.Env) tea.Cmd {
	return func() tea.Msg {
		var err error
		if item.envType == "conda" {
			err = conda.DeleteEnv(cenvs[item.idx])
		} else {
			err = uv.DeleteEnv(uenvs[item.idx])
		}
		return envDeletedMsg{err: err}
	}
}

// ── update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case envsLoadedMsg:
		m.condaEnvs = msg.condaEnvs
		m.uvEnvs = msg.uvEnvs
		m.rebuildItems()
		if m.cursor >= len(m.items) {
			m.cursor = 0
		}
		m.skipHeaders()
		return m, m.triggerDetailLoad()

	case detailsLoadedMsg:
		if msg.envType == "conda" {
			m.condaEnvs[msg.idx] = msg.cenv
		} else {
			m.uvEnvs[msg.idx] = msg.uenv
		}
		if m.state == stateLoadingDetails {
			m.state = stateList
		}
		return m, nil

	case envCreatedMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
		} else {
			m.statusMsg = "Environment created."
			m.statusErr = false
		}
		m.state = stateList
		return m, loadEnvsCmd(m.cwd)

	case envDeletedMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
		} else {
			m.statusMsg = "Environment deleted."
			m.statusErr = false
		}
		m.state = stateList
		return m, loadEnvsCmd(m.cwd)

	case activationFinishedMsg:
		if msg.err != nil {
			m.statusMsg = "Activation failed: " + msg.err.Error()
			m.statusErr = true
		} else {
			m.statusMsg = ""
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {

	case stateDeleteConfirm:
		switch {
		case key.Matches(msg, keys.Confirm):
			m.state = stateDeleting
			return m, deleteEnvCmd(m.deleteTarget, m.condaEnvs, m.uvEnvs)
		case key.Matches(msg, keys.Cancel) || msg.String() == "n":
			m.state = stateList
		}
		return m, nil

	case stateCreate:
		switch {
		case key.Matches(msg, keys.Cancel):
			m.state = stateList
			m.resetCreateForm()
			return m, nil

		case key.Matches(msg, keys.Tab):
			m.createFocus = (m.createFocus + 1) % 2
			m.createInputs[0].Blur()
			m.createInputs[1].Blur()
			m.createInputs[m.createFocus].Focus()
			return m, nil

		case msg.String() == "enter":
			name := strings.TrimSpace(m.createInputs[0].Value())
			if name == "" {
				m.statusMsg = "Name cannot be empty."
				m.statusErr = true
				return m, nil
			}
			ver := strings.TrimSpace(m.createInputs[1].Value())
			m.state = stateCreating
			return m, createEnvCmd(m.cwd, name, ver)
		}

		var cmd tea.Cmd
		m.createInputs[m.createFocus], cmd = m.createInputs[m.createFocus].Update(msg)
		return m, cmd

	case stateList, stateLoadingDetails:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Up):
			m.moveCursor(-1)
			return m, m.triggerDetailLoad()

		case key.Matches(msg, keys.Down):
			m.moveCursor(1)
			return m, m.triggerDetailLoad()

		case key.Matches(msg, keys.Activate):
			if sel := m.selectedItem(); sel != nil && sel.kind == kindEnv {
				return m, m.activateCmd(*sel)
			}
			return m, nil

		case key.Matches(msg, keys.New):
			m.resetCreateForm()
			m.state = stateCreate
			return m, nil

		case key.Matches(msg, keys.Delete):
			if sel := m.selectedItem(); sel != nil && sel.kind == kindEnv {
				m.deleteTarget = *sel
				m.state = stateDeleteConfirm
			}
			return m, nil

		case key.Matches(msg, keys.Refresh):
			m.statusMsg = ""
			return m, loadEnvsCmd(m.cwd)
		}
	}

	return m, nil
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	switch m.state {
	case stateCreate:
		return m.viewCreate()
	case stateDeleteConfirm:
		return m.viewDeleteConfirm()
	case stateCreating, stateDeleting:
		action := "Creating"
		if m.state == stateDeleting {
			action = "Deleting"
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			m.spinner.View()+" "+action+"...")
	}

	return m.viewMain()
}

func (m Model) viewMain() string {
	leftW := 36
	if m.width < 80 {
		leftW = m.width / 2
	}
	rightW := m.width - leftW - 3 // 3 for borders gap

	panelH := m.height - 3 // leave room for status bar

	left := m.renderList(leftW, panelH)
	right := m.renderDetail(rightW, panelH)

	cols := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	status := m.renderStatusBar()
	return lipgloss.JoinVertical(lipgloss.Left, cols, status)
}

func (m Model) renderList(w, h int) string {
	innerW := w - 2
	innerH := h - 2

	var sb strings.Builder
	for i, item := range m.items {
		if item.kind == kindHeader {
			line := sectionHeaderStyle.Width(innerW).Render("  " + item.label)
			sb.WriteString(line + "\n")
			continue
		}

		var name, ver, activeMarker string
		if item.envType == "conda" && item.idx < len(m.condaEnvs) {
			e := m.condaEnvs[item.idx]
			name = e.Name
			ver = e.PythonVer
			if e.Active {
				activeMarker = activeMarkerStyle.Render("*")
			}
		} else if item.envType == "uv" && item.idx < len(m.uvEnvs) {
			e := m.uvEnvs[item.idx]
			name = e.Name
			ver = e.PythonVer
			if e.Active {
				activeMarker = activeMarkerStyle.Render("*")
			}
		}

		verStr := ""
		if ver != "" && ver != "unknown" {
			// Show only major.minor
			parts := strings.Split(ver, ".")
			if len(parts) >= 2 {
				verStr = parts[0] + "." + parts[1]
			} else {
				verStr = ver
			}
		}

		nameW := innerW - 8
		if nameW < 10 {
			nameW = 10
		}

		line := fmt.Sprintf("%-*s %s %s", nameW, name, verStr, activeMarker)

		if i == m.cursor {
			line = selectedItemStyle.Width(innerW).Render(" " + line)
		} else {
			line = itemStyle.Width(innerW).Render(line)
		}
		sb.WriteString(line + "\n")
	}

	content := sb.String()
	// Trim to fit height
	lines := strings.Split(content, "\n")
	if len(lines) > innerH {
		// Simple scroll: keep cursor visible
		start := 0
		if m.cursor > innerH-1 {
			start = m.cursor - innerH + 1
		}
		end := start + innerH
		if end > len(lines) {
			end = len(lines)
		}
		lines = lines[start:end]
	}
	// Pad to fill panel
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	content = strings.Join(lines[:innerH], "\n")

	style := panelStyle.Width(w).Height(h)
	if m.state == stateList {
		style = panelActiveStyle.Width(w).Height(h)
	}
	return style.Render(content)
}

func (m Model) renderDetail(w, h int) string {
	innerW := w - 2

	sel := m.selectedItem()
	if sel == nil || sel.kind == kindHeader {
		return panelStyle.Width(w).Height(h).Render(
			lipgloss.Place(innerW, h-2, lipgloss.Center, lipgloss.Center,
				statusBarStyle.Render("Select an environment")),
		)
	}

	var name, path, pythonVer, envType string
	var pkgCount int
	var sizeBytes int64
	var loaded bool
	var activateCmd string

	if sel.envType == "conda" && sel.idx < len(m.condaEnvs) {
		e := m.condaEnvs[sel.idx]
		name, path, pythonVer = e.Name, e.Path, e.PythonVer
		pkgCount, sizeBytes, loaded = e.PkgCount, e.SizeBytes, e.Loaded
		envType = "conda"
		activateCmd = conda.ActivateCmd(e)
	} else if sel.envType == "uv" && sel.idx < len(m.uvEnvs) {
		e := m.uvEnvs[sel.idx]
		name, path, pythonVer = e.Name, e.Path, e.PythonVer
		pkgCount, sizeBytes, loaded = e.PkgCount, e.SizeBytes, e.Loaded
		envType = "uv"
		activateCmd = uv.ActivateCmd(e)
	}

	// Shorten path for display
	home, _ := os.UserHomeDir()
	displayPath := strings.Replace(path, home, "~", 1)

	var typeBadge string
	if envType == "conda" {
		typeBadge = condaBadgeStyle.Render("conda")
	} else {
		typeBadge = uvBadgeStyle.Render("uv")
	}

	var sb strings.Builder
	sb.WriteString(detailTitleStyle.Render(name) + "  " + typeBadge + "\n\n")

	row := func(k, v string) string {
		return detailKeyStyle.Render(k) + detailValStyle.Render(v) + "\n"
	}

	if loaded {
		sb.WriteString(row("Python", pythonVer))
		sb.WriteString(row("Packages", fmt.Sprintf("%d", pkgCount)))
		sb.WriteString(row("Size", formatSize(sizeBytes)))
	} else {
		sb.WriteString(m.spinner.View() + " loading details...\n")
	}

	sb.WriteString(row("Path", ""))
	// Wrap path across full width
	sb.WriteString("  " + statusBarStyle.Render(displayPath) + "\n")

	sb.WriteString("\n")
	sb.WriteString(detailKeyStyle.Render("Activate") + "\n")
	sb.WriteString(activeCmdStyle.Render(activateCmd) + "\n")

	return panelStyle.Width(w).Height(h).Render(
		lipgloss.NewStyle().Width(innerW).Render(sb.String()),
	)
}

func (m Model) viewCreate() string {
	var sb strings.Builder
	sb.WriteString(detailTitleStyle.Render("New uv Environment") + "\n\n")
	sb.WriteString(inputLabelStyle.Render("Name") + "\n")
	sb.WriteString(m.createInputs[0].View() + "\n\n")
	sb.WriteString(inputLabelStyle.Render("Python Version") + "\n")
	sb.WriteString(m.createInputs[1].View() + "\n\n")
	sb.WriteString(statusBarStyle.Render("tab") + " next  " +
		statusBarStyle.Render("enter") + " create  " +
		statusBarStyle.Render("esc") + " cancel")

	if m.statusMsg != "" {
		sb.WriteString("\n\n")
		if m.statusErr {
			sb.WriteString(errorStyle.Render(m.statusMsg))
		} else {
			sb.WriteString(successStyle.Render(m.statusMsg))
		}
	}

	box := panelActiveStyle.Width(50).Padding(1, 2).Render(sb.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) viewDeleteConfirm() string {
	item := m.deleteTarget
	var envName string
	if item.envType == "conda" && item.idx < len(m.condaEnvs) {
		envName = m.condaEnvs[item.idx].Name
	} else if item.envType == "uv" && item.idx < len(m.uvEnvs) {
		envName = m.uvEnvs[item.idx].Name
	}

	msg := confirmStyle.Render("Delete ") +
		dangerStyle.Render(envName) +
		confirmStyle.Render("?") + "\n\n" +
		keyStyle.Render("y") + " confirm  " +
		keyStyle.Render("esc") + " cancel"

	box := panelActiveStyle.Width(40).Padding(1, 2).Render(msg)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderStatusBar() string {
	var hints string
	switch m.state {
	case stateList:
		hints = keyStyle.Render("↵") + " activate  " +
			keyStyle.Render("n") + " new  " +
			keyStyle.Render("d") + " delete  " +
			keyStyle.Render("r") + " refresh  " +
			keyStyle.Render("q") + " quit"
	default:
		hints = ""
	}

	var status string
	if m.statusMsg != "" {
		if m.statusErr {
			status = errorStyle.Render("  " + m.statusMsg)
		} else {
			status = successStyle.Render("  " + m.statusMsg)
		}
	}

	bar := statusBarStyle.Width(m.width).Render(" pvman  " + hints + status)
	return bar
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (m *Model) rebuildItems() {
	m.items = nil
	if len(m.condaEnvs) > 0 {
		m.items = append(m.items, listItem{kind: kindHeader, label: "conda"})
		for i := range m.condaEnvs {
			m.items = append(m.items, listItem{kind: kindEnv, envType: "conda", idx: i, label: m.condaEnvs[i].Name})
		}
	}
	if len(m.uvEnvs) > 0 {
		m.items = append(m.items, listItem{kind: kindHeader, label: "uv  (" + m.cwd + ")"})
		for i := range m.uvEnvs {
			m.items = append(m.items, listItem{kind: kindEnv, envType: "uv", idx: i, label: m.uvEnvs[i].Name})
		}
	}
}

func (m *Model) skipHeaders() {
	for m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].kind == kindHeader {
		m.cursor++
	}
}

func (m *Model) moveCursor(dir int) {
	if len(m.items) == 0 {
		m.cursor = 0
		return
	}

	n := len(m.items)
	next := m.cursor + dir
	if next < 0 {
		next = n - 1
	} else if next >= n {
		next = 0
	}

	// Skip headers, wrapping around if needed.
	for m.items[next].kind == kindHeader {
		next += dir
		if next < 0 {
			next = n - 1
		} else if next >= n {
			next = 0
		}
		// Guard against an all-header list.
		if next == m.cursor {
			break
		}
	}

	m.cursor = next
}

func (m Model) activateCmd(item listItem) tea.Cmd {
	var activate string
	switch {
	case item.envType == "conda" && item.idx < len(m.condaEnvs):
		activate = conda.ActivateCmd(m.condaEnvs[item.idx])
	case item.envType == "uv" && item.idx < len(m.uvEnvs):
		activate = uv.ActivateCmd(m.uvEnvs[item.idx])
	default:
		return nil
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		if os.Getenv("PSModulePath") != "" {
			cmd = exec.Command("powershell", "-NoExit", "-ExecutionPolicy", "Bypass", "-Command", activate)
		} else {
			cmd = exec.Command("cmd", "/K", activate)
		}
	default:
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "bash"
		}
		shellName := filepath.Base(shell)
		// Run activation in a non-interactive shell (avoids loading .zshrc/.bashrc twice),
		// then exec an interactive shell so the user keeps their prompt/config.
		if item.envType == "conda" {
			hook := fmt.Sprintf(`eval "$(conda shell.%s hook)"`, shellName)
			cmd = exec.Command(shell, "-c", hook+"; "+activate+"; exec "+shell+" -i")
		} else {
			cmd = exec.Command(shell, "-c", activate+"; exec "+shell+" -i")
		}
	}

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return activationFinishedMsg{err: err}
	})
}

func (m *Model) selectedItem() *listItem {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		item := m.items[m.cursor]
		return &item
	}
	return nil
}

func (m Model) triggerDetailLoad() tea.Cmd {
	sel := m.selectedItem()
	if sel == nil || sel.kind == kindHeader {
		return nil
	}
	if sel.envType == "conda" && sel.idx < len(m.condaEnvs) && !m.condaEnvs[sel.idx].Loaded {
		m.state = stateLoadingDetails
		return loadDetailsCmd("conda", sel.idx, m.condaEnvs[sel.idx], uv.Env{})
	}
	if sel.envType == "uv" && sel.idx < len(m.uvEnvs) && !m.uvEnvs[sel.idx].Loaded {
		m.state = stateLoadingDetails
		return loadDetailsCmd("uv", sel.idx, conda.Env{}, m.uvEnvs[sel.idx])
	}
	return nil
}

func (m *Model) resetCreateForm() {
	m.createInputs[0].Reset()
	m.createInputs[1].Reset()
	m.createFocus = 0
	m.createInputs[0].Focus()
	m.createInputs[1].Blur()
	m.statusMsg = ""
}
