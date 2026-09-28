package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
	statePackageList
	statePackageDeleteConfirm
	statePackageDeleting
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
type packagesLoadedMsg struct {
	envType string
	idx     int
	pkgs    []string
	// deps[p] is what p needs, dependents[p] is what needs p. Both are keyed by
	// the names in pkgs. A nil map means the graph could not be built.
	deps       map[string][]string
	dependents map[string][]string
	err        error
}
type packageDeletedMsg struct {
	count int
	err   error
}

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

	// package management view
	pkgEnvType  string
	pkgIdx      int
	packages    []string
	pkgCursor   int
	pkgSelected map[int]bool
	pkgLoaded   bool

	// dependency graph of the environment being browsed
	deps       map[string][]string
	dependents map[string][]string

	// how the current selection connects to the rest of the environment;
	// computed when the delete confirmation opens so the dialog and the delete
	// command agree on the same set.
	rel relation
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
		// The lists are captured at confirm time and can be replaced by a
		// refresh before the user answers, so re-check the index here.
		var err error
		switch {
		case item.envType == "conda" && item.idx < len(cenvs):
			err = conda.DeleteEnv(cenvs[item.idx])
		case item.envType == "uv" && item.idx < len(uenvs):
			err = uv.DeleteEnv(uenvs[item.idx])
		default:
			err = fmt.Errorf("environment is no longer in the list")
		}
		return envDeletedMsg{err: err}
	}
}

func loadPackagesCmd(envType string, idx int, cenvs []conda.Env, uenvs []uv.Env) tea.Cmd {
	return func() tea.Msg {
		msg := packagesLoadedMsg{envType: envType, idx: idx}
		switch {
		case envType == "conda" && idx < len(cenvs):
			msg.pkgs, msg.err = conda.ListPackages(cenvs[idx])
			if msg.err == nil {
				// The graph is a nicety: if it cannot be built the list still
				// works, it just shows no relations.
				msg.deps, msg.dependents, _ = conda.Dependencies(cenvs[idx])
			}
		case envType == "uv" && idx < len(uenvs):
			msg.pkgs, msg.err = uv.ListPackages(uenvs[idx])
			if msg.err == nil {
				msg.deps, msg.dependents, _ = uv.Dependencies(uenvs[idx])
			}
		}
		return msg
	}
}

// deletePackagesCmd removes exactly the named packages. The caller decides
// whether that set includes the related packages, so there is a single place
// where the user's choice is applied.
func deletePackagesCmd(envType string, idx int, cenvs []conda.Env, uenvs []uv.Env, names []string) tea.Cmd {
	return func() tea.Msg {
		if len(names) == 0 {
			return packageDeletedMsg{err: fmt.Errorf("no packages selected")}
		}
		var (
			count int
			err   error
		)
		switch {
		case envType == "conda" && idx < len(cenvs):
			count, err = conda.RemovePackage(cenvs[idx], names...)
		case envType == "uv" && idx < len(uenvs):
			count, err = uv.RemovePackage(uenvs[idx], names...)
		default:
			err = fmt.Errorf("environment is no longer in the list")
		}
		return packageDeletedMsg{count: count, err: err}
	}
}

// selectedNames returns the ticked package names in list order.
func (m Model) selectedNames() []string {
	names := make([]string, 0, len(m.pkgSelected))
	for i, name := range m.packages {
		if m.pkgSelected[i] {
			names = append(names, name)
		}
	}
	return names
}

// relation describes how the current selection connects to the rest of the
// environment, in both directions.
type relation struct {
	// Breaks holds the transitive dependents of the selection: packages that
	// would be left with a requirement nothing satisfies.
	Breaks []string
	// Orphans holds the selection's own dependencies that nothing else in the
	// environment needs, so they would be left unused.
	Orphans []string
}

// Empty reports whether the selection is connected to nothing else.
func (r relation) Empty() bool { return len(r.Breaks) == 0 && len(r.Orphans) == 0 }

// All returns every package the relation names, for use as the delete set.
func (r relation) All() []string {
	out := make([]string, 0, len(r.Breaks)+len(r.Orphans))
	seen := make(map[string]bool, cap(out))
	for _, group := range [][]string{r.Breaks, r.Orphans} {
		for _, n := range group {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// deleteSet is the package list the confirmation dialog would submit: what was
// ticked, plus — when the user asked for them — every related package.
func (m Model) deleteSet(includeRelated bool) []string {
	names := m.selectedNames()
	if includeRelated {
		names = append(names, m.rel.All()...)
	}
	return names
}

// selectionRelation analyses the ticked packages against the dependency graph.
func (m Model) selectionRelation() relation {
	selected := make(map[string]bool, len(m.pkgSelected))
	for i, name := range m.packages {
		if m.pkgSelected[i] {
			selected[name] = true
		}
	}
	if len(selected) == 0 {
		return relation{}
	}

	installed := make(map[string]bool, len(m.packages))
	for _, name := range m.packages {
		installed[name] = true
	}

	// Walk the reverse edges transitively: whatever needs something in the
	// selection, and whatever needs that, and so on.
	breaks := make(map[string]bool)
	queue := make([]string, 0, len(selected))
	for n := range selected {
		queue = append(queue, n)
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, d := range m.dependents[n] {
			// A dependent that is going away anyway does not break, and the
			// visited set also stops dependency cycles from looping forever.
			if selected[d] || breaks[d] || !installed[d] {
				continue
			}
			breaks[d] = true
			queue = append(queue, d)
		}
	}

	// A package is orphaned when the selection needs it and nothing that stays
	// behind does. Removing an orphan can orphan its own dependencies, so this
	// runs to a fixpoint. A package the selection does not reach is left alone:
	// it is something the user installed deliberately, not leftover.
	orphans := make(map[string]bool)
	for changed := true; changed; {
		changed = false
		for _, pkg := range m.packages {
			if selected[pkg] || orphans[pkg] {
				continue
			}
			reachedByDoomed, allDependentsDoomed := false, true
			for _, d := range m.dependents[pkg] {
				if !installed[d] {
					continue
				}
				if selected[d] || orphans[d] {
					reachedByDoomed = true
				} else {
					allDependentsDoomed = false
				}
			}
			if reachedByDoomed && allDependentsDoomed {
				orphans[pkg] = true
				changed = true
			}
		}
	}

	return relation{Breaks: sortedKeys(breaks), Orphans: sortedKeys(orphans)}
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
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
		// A confirmation dialog holds an index into the old lists. Cancel it
		// rather than let "y" act on an environment that may be gone.
		if m.state == stateDeleteConfirm {
			m.state = stateList
			m.statusMsg = ""
		}
		return m, m.triggerDetailLoad()

	case detailsLoadedMsg:
		// The environment may have disappeared while its details were loading.
		if msg.envType == "conda" && msg.idx < len(m.condaEnvs) {
			m.condaEnvs[msg.idx] = msg.cenv
		} else if msg.envType == "uv" && msg.idx < len(m.uvEnvs) {
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

	case packagesLoadedMsg:
		if m.state == statePackageList && msg.envType == m.pkgEnvType && msg.idx == m.pkgIdx {
			if msg.err != nil {
				m.statusMsg = "Failed to load packages: " + msg.err.Error()
				m.statusErr = true
				m.packages = nil
			} else {
				m.packages = msg.pkgs
				m.statusMsg = ""
			}
			// The list may have shrunk since the last render (a delete, or a
			// refresh after one), so drop cursor/selection entries that now
			// point past the end.
			m.clampPkgCursor()
			m.pkgLoaded = true
			// The graph is keyed by these very package names, so it is stored
			// only alongside the list it was built from: a load resolving for an
			// environment the user has already left must not repoint the panel.
			m.deps, m.dependents = msg.deps, msg.dependents
		}
		return m, nil

	case packageDeletedMsg:
		if msg.err != nil {
			m.statusMsg = "Failed to delete packages: " + msg.err.Error()
			m.statusErr = true
			m.state = statePackageList
		} else {
			// conda's solver removes more than it was asked to, so report what
			// actually happened rather than what was requested.
			m.statusMsg = fmt.Sprintf("Deleted %d package(s).", msg.count)
			m.statusErr = false
			m.pkgSelected = make(map[int]bool)
			m.pkgCursor = 0
			m.pkgLoaded = false
			m.rel = relation{}
			m.state = statePackageList
			// refresh packages and env details
			if m.pkgEnvType == "conda" && m.pkgIdx < len(m.condaEnvs) {
				m.condaEnvs[m.pkgIdx].Loaded = false
				return m, tea.Batch(
					loadPackagesCmd(m.pkgEnvType, m.pkgIdx, m.condaEnvs, m.uvEnvs),
					loadDetailsCmd("conda", m.pkgIdx, m.condaEnvs[m.pkgIdx], uv.Env{}),
				)
			}
			if m.pkgEnvType == "uv" && m.pkgIdx < len(m.uvEnvs) {
				m.uvEnvs[m.pkgIdx].Loaded = false
				return m, tea.Batch(
					loadPackagesCmd(m.pkgEnvType, m.pkgIdx, m.condaEnvs, m.uvEnvs),
					loadDetailsCmd("uv", m.pkgIdx, conda.Env{}, m.uvEnvs[m.pkgIdx]),
				)
			}
			return m, nil
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

	case statePackageList:
		// The list can shrink under us (a delete finished, a refresh returned
		// fewer packages), so re-anchor the cursor and drop dead selections
		// before any key touches them.
		m.clampPkgCursor()
		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit

		case key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Packages), msg.String() == "q":
			m.state = stateList
			m.packages = nil
			m.pkgSelected = nil
			// The graph belongs to the environment being left; keeping it would
			// let the next environment's list show stale relations.
			m.deps, m.dependents = nil, nil
			m.rel = relation{}
			m.pkgLoaded = false
			return m, nil

		case key.Matches(msg, keys.Up):
			// Wrapping matches the environment list, and takes the long way
			// round from the top of a several-hundred package list to the
			// bottom, which is quicker than holding the key down.
			if n := len(m.packages); n > 0 {
				m.pkgCursor = (m.pkgCursor - 1 + n) % n
			}
			return m, nil

		case key.Matches(msg, keys.Down):
			if n := len(m.packages); n > 0 {
				m.pkgCursor = (m.pkgCursor + 1) % n
			}
			return m, nil

		case key.Matches(msg, keys.Toggle):
			// Without these guards an empty or not-yet-loaded list would record
			// a selection for a package that does not exist, and `d` would then
			// offer to delete it.
			if !m.pkgLoaded || len(m.packages) == 0 {
				return m, nil
			}
			if m.pkgSelected == nil {
				m.pkgSelected = make(map[int]bool)
			}
			// Unticking removes the entry rather than storing false, so that
			// len(m.pkgSelected) is always the number of ticked packages.
			i := m.pkgCursor
			if m.pkgSelected[i] {
				delete(m.pkgSelected, i)
			} else {
				m.pkgSelected[i] = true
			}
			return m, nil

		case key.Matches(msg, keys.All):
			if !m.pkgLoaded || len(m.packages) == 0 {
				return m, nil
			}
			if m.pkgSelected == nil {
				m.pkgSelected = make(map[int]bool)
			}
			// If everything is already selected, clear; otherwise select all.
			if len(m.pkgSelected) == len(m.packages) {
				m.pkgSelected = make(map[int]bool)
			} else {
				m.pkgSelected = make(map[int]bool, len(m.packages))
				for i := range m.packages {
					m.pkgSelected[i] = true
				}
			}
			return m, nil

		case key.Matches(msg, keys.Delete):
			if !m.pkgLoaded {
				m.statusMsg = "Still loading packages..."
				m.statusErr = true
				return m, nil
			}
			if len(m.pkgSelected) == 0 {
				m.statusMsg = "No packages selected. Use space to select, a to select all."
				m.statusErr = true
				return m, nil
			}
			// Freeze the analysis now: the dialog and the command must both act
			// on the same set even if a refresh lands in between.
			m.rel = m.selectionRelation()
			m.state = statePackageDeleteConfirm
			return m, nil
		}
		return m, nil

	case statePackageDeleteConfirm, statePackageDeleting:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.state == statePackageDeleting {
			return m, nil // wait for the delete to finish
		}
		switch {
		case key.Matches(msg, keys.Confirm):
			// y: the selection plus everything the analysis flagged with it.
			m.state = statePackageDeleting
			return m, deletePackagesCmd(m.pkgEnvType, m.pkgIdx, m.condaEnvs, m.uvEnvs, m.deleteSet(true))
		case msg.String() == "n":
			// n: only what was ticked, leaving the rest as it falls.
			m.state = statePackageDeleting
			return m, deletePackagesCmd(m.pkgEnvType, m.pkgIdx, m.condaEnvs, m.uvEnvs, m.deleteSet(false))
		case key.Matches(msg, keys.Cancel):
			m.state = statePackageList
			m.rel = relation{}
		}
		return m, nil

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

		case key.Matches(msg, keys.Packages):
			sel := m.selectedItem()
			if sel == nil || sel.kind != kindEnv {
				return m, nil
			}
			m.pkgEnvType = sel.envType
			m.pkgIdx = sel.idx
			m.packages = nil
			m.pkgSelected = make(map[int]bool)
			m.pkgCursor = 0
			m.pkgLoaded = false
			// Drop the previous environment's graph so the panel cannot pair its
			// relations with this environment's list while the load is in flight.
			m.deps, m.dependents = nil, nil
			m.rel = relation{}
			m.statusMsg = ""
			m.state = statePackageList
			return m, loadPackagesCmd(sel.envType, sel.idx, m.condaEnvs, m.uvEnvs)

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
	case statePackageList:
		return m.viewPackages()
	case statePackageDeleteConfirm:
		return m.viewPackageDeleteConfirm()
	case stateCreating, stateDeleting:
		action := "Creating"
		if m.state == stateDeleting {
			action = "Deleting"
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			m.spinner.View()+" "+action+"...")
	case statePackageDeleting:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			m.spinner.View()+" Deleting packages...")
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

		// The marker belongs beside the name, not out past the version. Its
		// column is reserved on every row — blank when the environment is not
		// the active one — so the versions still line up.
		marker := "  "
		if activeMarker != "" {
			marker = activeMarker + " "
		}

		nameW := innerW - 10
		if nameW < 10 {
			nameW = 10
		}

		line := marker + fmt.Sprintf("%-*s %s", nameW, name, verStr)

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

// clampPkgCursor keeps the package cursor and selection within the current list.
func (m *Model) clampPkgCursor() {
	if m.pkgCursor >= len(m.packages) {
		m.pkgCursor = len(m.packages) - 1
	}
	if m.pkgCursor < 0 {
		m.pkgCursor = 0
	}
	for i := range m.pkgSelected {
		if i >= len(m.packages) {
			delete(m.pkgSelected, i)
		}
	}
}

// pkgEnvName returns the display name of the environment whose packages are shown.
func (m Model) pkgEnvName() string {
	if m.pkgEnvType == "conda" && m.pkgIdx < len(m.condaEnvs) {
		return m.condaEnvs[m.pkgIdx].Name
	}
	if m.pkgEnvType == "uv" && m.pkgIdx < len(m.uvEnvs) {
		return m.uvEnvs[m.pkgIdx].Name
	}
	return "?"
}

// viewPackages renders the package list, shrinking the visible window until the
// box fits the terminal. The chrome around the list (border, padding, title,
// hint and the optional status lines) adds a variable number of rows, so the
// height is measured rather than assumed.
func (m Model) viewPackages() string {
	// Two columns need room for both; below that the list gets the whole width.
	if len(m.packages) > 0 && m.width >= 72 {
		return m.viewPackagesTwoColumn()
	}

	maxRows := len(m.packages)
	if maxRows < 1 {
		maxRows = 1
	}

	box := m.renderPackagesBox(maxRows)
	if h := lipgloss.Height(box); h > m.height && len(m.packages) > 0 {
		// Every row dropped removes exactly one line.
		maxRows -= h - m.height
		if maxRows < 1 {
			maxRows = 1
		}
		box = m.renderPackagesBox(maxRows)
	}
	// On a very short terminal even the chrome does not fit; clip rather than
	// write a panel taller than the screen.
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, clipHeight(box, m.height))
}

// viewPackagesTwoColumn shows the package list beside the highlighted package's
// relations, mirroring the environment view's list/detail split.
func (m Model) viewPackagesTwoColumn() string {
	leftW := (m.width - 3) * 2 / 5
	if leftW < 30 {
		leftW = 30
	}
	if leftW > 56 {
		leftW = 56
	}
	rightW := m.width - leftW - 3
	panelH := m.height - 3

	cols := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderPackageList(leftW, panelH), " ", m.renderDepPanel(rightW, panelH))
	status := clipHeight(m.renderPackageStatusBar(), 1)
	return lipgloss.JoinVertical(lipgloss.Left, cols, status)
}

func (m Model) renderPackageList(w, h int) string {
	innerW, innerH := w-2, h-2

	// title, its blank line, and the footer
	maxRows := innerH - 3
	if maxRows < 1 {
		maxRows = 1
	}

	var sb strings.Builder
	title := fmt.Sprintf("%s  %d packages", m.pkgEnvName(), len(m.packages))
	sb.WriteString(detailTitleStyle.Render(truncate(title, innerW)) + "\n\n")

	start := 0
	if m.pkgCursor >= maxRows {
		start = m.pkgCursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.packages) {
		end = len(m.packages)
	}

	for i := start; i < end; i++ {
		mark := "[ ]"
		if m.pkgSelected[i] {
			mark = "[x]"
		}
		line := truncate(fmt.Sprintf("%s %s", mark, m.packages[i]), innerW)
		switch {
		case i == m.pkgCursor:
			line = selectedItemStyle.Render(" " + line)
		case m.pkgSelected[i]:
			line = successStyle.Render("  " + line)
		default:
			line = itemStyle.Render(line)
		}
		sb.WriteString(line + "\n")
	}

	var footer string
	if len(m.packages) > maxRows {
		footer = fmt.Sprintf("%d-%d of %d", start+1, end, len(m.packages))
	}
	if n := len(m.pkgSelected); n > 0 {
		if footer != "" {
			footer += "  "
		}
		footer += fmt.Sprintf("%d selected", n)
	}
	sb.WriteString(statusBarStyle.Render(truncate("  "+footer, innerW)))

	return panelActiveStyle.Width(innerW).Height(innerH).Render(sb.String())
}

// renderDepPanel lists what the highlighted package needs and what needs it.
// Only packages present in the environment are shown, since those are the only
// ones a delete could act on.
func (m Model) renderDepPanel(w, h int) string {
	innerW, innerH := w-2, h-2
	if innerW < 8 || innerH < 4 {
		return panelStyle.Width(max(innerW, 1)).Height(max(innerH, 1)).Render("")
	}
	if m.pkgCursor < 0 || m.pkgCursor >= len(m.packages) {
		return panelStyle.Width(innerW).Height(innerH).Render("")
	}

	name := m.packages[m.pkgCursor]

	var sb strings.Builder
	sb.WriteString(detailTitleStyle.Render(truncate(name, innerW)) + "\n\n")

	if m.deps == nil && m.dependents == nil {
		sb.WriteString(statusBarStyle.Render("  dependency data unavailable"))
		return panelStyle.Width(innerW).Height(innerH).Render(sb.String())
	}

	needs := m.installedOnly(m.deps[name])
	usedBy := m.installedOnly(m.dependents[name])

	// The title, its blank line and the blank between the sections are already
	// spent; the sections divide what is left, each counting its own header.
	lines := max(innerH-3, 2)
	needLines, usedLines := (lines+1)/2, lines/2
	switch {
	case len(needs) == 0 && len(usedBy) == 0:
		needLines, usedLines = 1, 1
	case len(needs) == 0:
		needLines, usedLines = 1, lines-1
	case len(usedBy) == 0:
		needLines, usedLines = lines-1, 1
	}

	out := depSection("needs", needs, needLines, innerW)
	out = append(out, "")
	out = append(out, depSection("needed by", usedBy, usedLines, innerW)...)

	return panelStyle.Width(innerW).Height(innerH).Render(strings.Join(out, "\n"))
}

// depSection renders one titled group of package names, marking any that did
// not fit. maxLines is the total the section may occupy, header included, so
// the caller can budget rows exactly rather than leaving them to be clipped.
func depSection(title string, names []string, maxLines, width int) []string {
	maxLines = max(maxLines, 1)
	// The header states the true total, which is what keeps a list cut to a
	// single line honest. The style indents it by one column, so the text has
	// one column less than the budget, and the title gives way before the
	// count: a clipped number reads as a smaller, wrong one.
	room := width - 1
	suffix := fmt.Sprintf(" (%d)", len(names))
	header := truncate(truncate(title, max(room-len([]rune(suffix)), 0))+suffix, room)
	lines := []string{sectionHeaderStyle.Render(header)}
	if maxLines == 1 {
		return lines
	}
	if len(names) == 0 {
		return append(lines, statusBarStyle.Render("  -"))
	}

	// A truncated list needs a line for the count of what did not fit.
	show := len(names)
	if room := maxLines - 1; show > room {
		show = room - 1
	}
	shown := 0
	for _, n := range names {
		if shown == show {
			break
		}
		lines = append(lines, itemStyle.Render(truncate(n, width-2)))
		shown++
	}
	if rest := len(names) - shown; rest > 0 {
		lines = append(lines, statusBarStyle.Render(fmt.Sprintf("  +%d more", rest)))
	}
	return lines
}

// installedOnly drops names absent from the current list.
func (m Model) installedOnly(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	installed := make(map[string]bool, len(m.packages))
	for _, n := range m.packages {
		installed[n] = true
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if installed[n] {
			out = append(out, n)
		}
	}
	return out
}

// truncate shortens s to at most w runes, marking the cut with an ellipsis.
// It runs before styling, so plain rune arithmetic is enough.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return string(r[:1])
	}
	return string(r[:w-1]) + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// clipHeight drops trailing lines so s is at most h lines tall.
func clipHeight(s string, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= h {
		return s
	}
	return strings.Join(lines[:h], "\n")
}

// renderPackagesBox builds the package panel showing at most maxRows list rows.
func (m Model) renderPackagesBox(maxRows int) string {
	var badge string
	if m.pkgEnvType == "conda" {
		badge = condaBadgeStyle.Render("conda")
	} else {
		badge = uvBadgeStyle.Render("uv")
	}

	selected := len(m.pkgSelected)

	var sb strings.Builder
	sb.WriteString(detailTitleStyle.Render(m.pkgEnvName()) + "  " + badge + "  " +
		statusBarStyle.Render(fmt.Sprintf("%d packages", len(m.packages))) + "\n\n")

	if !m.pkgLoaded {
		sb.WriteString(m.spinner.View() + " loading packages...\n")
	} else if len(m.packages) == 0 {
		sb.WriteString(statusBarStyle.Render("No packages found.") + "\n")
	} else {
		start := 0
		if m.pkgCursor >= maxRows {
			start = m.pkgCursor - maxRows + 1
		}
		end := start + maxRows
		if end > len(m.packages) {
			end = len(m.packages)
		}

		for i := start; i < end; i++ {
			mark := "[ ]"
			if m.pkgSelected[i] {
				mark = "[x]"
			}
			line := fmt.Sprintf("%s %s", mark, m.packages[i])
			if i == m.pkgCursor {
				line = selectedItemStyle.Render(" " + line)
			} else if m.pkgSelected[i] {
				line = successStyle.Render("  " + line)
			} else {
				line = itemStyle.Render(line)
			}
			sb.WriteString(line + "\n")
		}

		if len(m.packages) > maxRows {
			sb.WriteString(statusBarStyle.Render(
				fmt.Sprintf("\n  %d-%d of %d", start+1, end, len(m.packages))) + "\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(keyStyle.Render("space") + " toggle  " +
		keyStyle.Render("a") + " all  " +
		keyStyle.Render("d") + " delete selected  " +
		keyStyle.Render("esc") + " back")

	if m.statusMsg != "" {
		sb.WriteString("\n")
		if m.statusErr {
			sb.WriteString(errorStyle.Render(m.statusMsg))
		} else {
			sb.WriteString(successStyle.Render(m.statusMsg))
		}
	}

	if selected > 0 {
		sb.WriteString("\n" + confirmStyle.Render(fmt.Sprintf("%d selected", selected)))
	}

	width := m.width - 4
	if width < 4 {
		width = 4
	}
	return panelActiveStyle.Width(width).Padding(1, 2).Render(sb.String())
}

func (m Model) viewPackageDeleteConfirm() string {
	names := m.selectedNames()

	// Below this a terminal cannot show the lists and the decision together.
	// The dialog collapses to the decision alone, because the alternative is
	// chrome clipping the key hints off the bottom — leaving the user in a
	// dialog with no visible way out of it.
	if m.height < deleteDialogFullHeight {
		return m.viewPackageDeleteCompact(names)
	}

	// The box width drives how much of a package name fits on a line, and the
	// terminal height how many lines the name lists may take. The chrome cost
	// is fixed and known, so the budget is arithmetic rather than a guess and
	// clipHeight never has to fire.
	boxW := min(76, max(40, m.width-4))
	innerW := boxW - 6
	rows := max(1, m.height-deleteDialogChrome)

	var sb strings.Builder
	sb.WriteString(confirmStyle.Render(fmt.Sprintf("Delete %d package(s) from ", len(names))) +
		dangerStyle.Render(m.pkgEnvName()) +
		confirmStyle.Render("?") + "\n\n")
	// The selection itself only needs enough rows to recognise it.
	sb.WriteString(linesBlock(nameLines(names, innerW, min(3, rows))) + "\n")

	if m.rel.Empty() {
		sb.WriteString("\n" + statusBarStyle.Render("No other installed package is connected to this selection.") + "\n")
	} else {
		// Share what is left between the two groups when both are present. The
		// broken dependents outrank the orphans, so they take the odd row.
		breakRows, orphanRows := rows, 0
		if len(m.rel.Breaks) > 0 && len(m.rel.Orphans) > 0 {
			breakRows, orphanRows = rows-rows/2, rows/2
		}

		sb.WriteString("\n")
		if n := len(m.rel.Breaks); n > 0 {
			sb.WriteString(dangerStyle.Render(fmt.Sprintf("%d package(s) depend on what you selected", n)) +
				statusBarStyle.Render(" and would be left broken:") + "\n")
			if breakRows > 0 {
				sb.WriteString(linesBlock(nameLines(m.rel.Breaks, innerW, breakRows)) + "\n")
			}
		}
		if n := len(m.rel.Orphans); n > 0 {
			sb.WriteString(confirmStyle.Render(fmt.Sprintf("%d package(s)", n)) +
				statusBarStyle.Render(" are only needed by your selection and would be left unused:") + "\n")
			if orphanRows > 0 {
				sb.WriteString(linesBlock(nameLines(m.rel.Orphans, innerW, orphanRows)) + "\n")
			}
		}
		// conda's solver keeps the environment consistent and so removes more
		// than it is asked to; the graph above is a lower bound, not a promise.
		if m.pkgEnvType == "conda" {
			sb.WriteString("\n" + confirmStyle.Render("note: ") +
				statusBarStyle.Render("conda may also remove further packages to keep the environment consistent.") + "\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(m.deleteConfirmHints(names))

	box := panelActiveStyle.Width(boxW).Padding(1, 2).Render(sb.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, clipHeight(box, m.height))
}

// viewPackageDeleteCompact is the dialog for a terminal too short for the
// package lists: what is at stake, and the keys.
func (m Model) viewPackageDeleteCompact(names []string) string {
	var sb strings.Builder
	sb.WriteString(confirmStyle.Render(fmt.Sprintf("Delete %d package(s) from ", len(names))) +
		dangerStyle.Render(m.pkgEnvName()) + confirmStyle.Render("?") + "\n")
	if n := len(m.rel.Breaks); n > 0 {
		sb.WriteString(dangerStyle.Render(fmt.Sprintf("%d dependent(s) would break", n)) + "\n")
	}
	if n := len(m.rel.Orphans); n > 0 {
		sb.WriteString(confirmStyle.Render(fmt.Sprintf("%d dependency(ies) would be orphaned", n)) + "\n")
	}
	sb.WriteString("\n" + m.deleteConfirmHints(names))

	box := panelActiveStyle.Width(min(76, max(40, m.width-4))).Padding(1, 2).Render(sb.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, clipHeight(box, m.height))
}

// deleteConfirmHints renders the key legend, which both dialog sizes show.
func (m Model) deleteConfirmHints(names []string) string {
	if m.rel.Empty() {
		return keyStyle.Render("y") + " delete  " + keyStyle.Render("esc") + " cancel"
	}
	total := len(names) + len(m.rel.All())
	return keyStyle.Render("y") + fmt.Sprintf(" delete all %d  ", total) +
		keyStyle.Render("n") + fmt.Sprintf(" only the %d selected  ", len(names)) +
		keyStyle.Render("esc") + " cancel"
}

// deleteDialogChrome is the number of rows viewPackageDeleteConfirm spends on
// everything that is not a package list: border, padding, the heading and its
// blank line, the selection preview, the group headings, the conda note and the
// key hints. The two group lists get whatever the terminal has left over.
const deleteDialogChrome = 15

// deleteDialogFullHeight is the shortest terminal that can show the chrome
// above plus one row for each group list.
const deleteDialogFullHeight = 17

// linesBlock renders name lines in the muted style used for secondary text.
func linesBlock(lines []string) string {
	if len(lines) == 0 {
		return statusBarStyle.Render("-")
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = statusBarStyle.Render(l)
	}
	return strings.Join(out, "\n")
}

// nameLines flows names into comma-separated lines of at most width runes. When
// more than maxRows lines are needed it keeps the first maxRows and folds the
// count of what is not shown into the last one, so a long list is still
// recognisable rather than reduced to a truncated stub.
func nameLines(names []string, width, maxRows int) []string {
	if len(names) == 0 || maxRows < 1 || width <= 0 {
		return nil
	}

	var lines []string
	cur, inLine := "", 0
	for i, n := range names {
		if inLine > 0 && len([]rune(cur))+2+len([]rune(n)) > width {
			lines = append(lines, truncate(cur, width))
			cur, inLine = "", 0
		}
		if inLine == 0 {
			if len(lines) >= maxRows {
				// The count is the whole point of the last line, so reserve its
				// room before trimming the names to fit beside it.
				suffix := fmt.Sprintf(" +%d more", len(names)-i)
				keep := width - len([]rune(suffix))
				if keep < 1 {
					lines[len(lines)-1] = truncate(strings.TrimSpace(suffix), width)
				} else {
					lines[len(lines)-1] = truncate(lines[len(lines)-1], keep) + suffix
				}
				return lines
			}
			cur, inLine = n, 1
			continue
		}
		cur += ", " + n
		inLine++
	}
	if inLine > 0 {
		lines = append(lines, truncate(cur, width))
	}
	return lines
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m Model) renderStatusBar() string {
	var hints string
	switch m.state {
	case stateList:
		hints = keyStyle.Render("↵") + " activate  " +
			keyStyle.Render("p") + " packages  " +
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

// renderPackageStatusBar renders the key hints and status line under the
// package view.
func (m Model) renderPackageStatusBar() string {
	hints := keyStyle.Render("space") + " toggle  " +
		keyStyle.Render("a") + " all  " +
		keyStyle.Render("d") + " delete  " +
		keyStyle.Render("esc") + " back"

	var status string
	switch {
	case m.statusMsg != "" && m.statusErr:
		status = errorStyle.Render("  " + m.statusMsg)
	case m.statusMsg != "":
		status = successStyle.Render("  " + m.statusMsg)
	}
	return statusBarStyle.Width(m.width).Render(" " + hints + status)
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
