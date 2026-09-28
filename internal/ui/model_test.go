package ui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tkzzzzzz6/pvman/internal/conda"
)

// TestMoveCursorWrapAround covers the reported bug where moving up from the
// first environment crashed with "index out of range [-1]".
func TestMoveCursorWrapAround(t *testing.T) {
	m := &Model{items: []listItem{
		{kind: kindHeader, label: "conda"},
		{kind: kindEnv, envType: "conda", idx: 0},
		{kind: kindEnv, envType: "conda", idx: 1},
	}}

	// From the first selectable item, "up" must land on the last one.
	m.cursor = 1
	m.moveCursor(-1)
	if m.cursor != 2 {
		t.Fatalf("up from first: cursor = %d, want 2", m.cursor)
	}

	// From the last, "down" must wrap to the first selectable item.
	m.moveCursor(1)
	if m.cursor != 1 {
		t.Fatalf("down from last: cursor = %d, want 1", m.cursor)
	}
}

func TestMoveCursorEmptyAndAllHeaders(t *testing.T) {
	empty := &Model{}
	empty.moveCursor(1) // must not panic
	empty.moveCursor(-1)

	headers := &Model{items: []listItem{{kind: kindHeader}, {kind: kindHeader}}}
	headers.moveCursor(1) // must not panic on an all-header list
	headers.moveCursor(-1)
}

// TestPkgStateSafety drives the package view across cursor positions and
// selection maps that no longer match the loaded list, which is what happens
// when the list shrinks after a deletion.
func TestPkgStateSafety(t *testing.T) {
	for _, n := range []int{0, 1, 5, 30} {
		for _, cur := range []int{-3, -1, 0, 1, n - 1, n, n + 5, 30} {
			for _, sel := range []map[int]bool{
				nil,
				{},
				{0: true},
				{5: true, 25: true},
				{100: true},
			} {
				m := Model{
					state:       statePackageList,
					width:       80,
					height:      24,
					packages:    mkPkgs(n),
					pkgCursor:   cur,
					pkgSelected: sel,
					pkgLoaded:   true,
				}
				_ = m.viewPackages()
				_ = m.viewPackageDeleteConfirm()

				for _, k := range []string{"down", "up", " ", "a", "d", "y", "esc"} {
					w := m
					mm, _ := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
					got := mm.(Model)
					if got.pkgCursor < 0 || (n > 0 && got.pkgCursor >= n) {
						t.Fatalf("n=%d cur=%d key=%q -> cursor %d out of range", n, cur, k, got.pkgCursor)
					}
					for i := range got.pkgSelected {
						if i >= n {
							t.Fatalf("n=%d cur=%d key=%q -> stale selection %d", n, cur, k, i)
						}
					}
				}
			}
		}
	}
}

func TestDeletePackagesCmdIgnoresStaleIndices(t *testing.T) {
	// A selection map holding indices past the end of the list must not panic.
	cmd := deletePackagesCmd("conda", 0, nil, nil,
		map[int]bool{0: true, 5: true, 100: true}, mkPkgs(3))
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	if _, ok := cmd().(packageDeletedMsg); !ok {
		t.Fatal("unexpected message type")
	}
}

// TestDeleteConfirmSurvivesShrinkingList reproduces a panic: open the delete
// confirmation on the last environment, let a refresh land with a shorter list
// while the dialog is still open, then press y.
func TestDeleteConfirmSurvivesShrinkingList(t *testing.T) {
	m := New()
	m.width, m.height = 100, 30
	m.condaEnvs = []conda.Env{{Name: "A"}, {Name: "B"}, {Name: "C"}}
	m.rebuildItems()
	for i, it := range m.items {
		if it.kind == kindEnv && it.label == "C" {
			m.cursor = i
		}
	}

	mm, _ := m.Update(keyRune('d'))
	m = mm.(Model)
	if m.state != stateDeleteConfirm {
		t.Fatalf("state = %v, want delete confirm", m.state)
	}

	// A refresh resolves with fewer environments than the dialog was built on.
	mm, _ = m.Update(envsLoadedMsg{condaEnvs: []conda.Env{{Name: "A"}}})
	m = mm.(Model)
	if m.state == stateDeleteConfirm {
		t.Fatal("stale confirmation dialog was kept after the list shrank")
	}

	// And even if a stale target reached the command, it must not panic.
	if cmd := deleteEnvCmd(listItem{kind: kindEnv, envType: "conda", idx: 5},
		[]conda.Env{{Name: "A"}}, nil); cmd != nil {
		msg, ok := cmd().(envDeletedMsg)
		if !ok {
			t.Fatalf("unexpected msg %T", cmd())
		}
		if msg.err == nil {
			t.Fatal("expected an error for an out-of-range environment")
		}
	}
}

// TestDetailsForVanishedEnv covers a detail load resolving after a refresh
// removed the environment it was loading for.
func TestDetailsForVanishedEnv(t *testing.T) {
	m := New()
	m.condaEnvs = []conda.Env{{Name: "A"}}
	mm, _ := m.Update(detailsLoadedMsg{envType: "conda", idx: 7})
	_ = mm.(Model) // must not panic
}

// runKey applies one key to the model and returns the result.
func runKey(m Model, k tea.KeyMsg) Model {
	mm, _ := m.Update(k)
	return mm.(Model)
}

func pkgModel(n int) Model {
	return Model{
		state:       statePackageList,
		width:       80,
		height:      24,
		packages:    mkPkgs(n),
		pkgSelected: make(map[int]bool),
		pkgLoaded:   true,
	}
}

// TestUntickDropsSelection guards the invariant that len(pkgSelected) is the
// number of ticked packages: unticking must remove the entry rather than store
// false, which would inflate the count, the "N selected" footer, the delete
// confirmation and the "Deleted N package(s)" message.
func TestUntickDropsSelection(t *testing.T) {
	m := pkgModel(3)

	m = runKey(m, keyRune(' ')) // tick 0
	if got := len(m.pkgSelected); got != 1 {
		t.Fatalf("after tick: %d selected, want 1", got)
	}

	m = runKey(m, keyRune(' ')) // untick 0
	if got := len(m.pkgSelected); got != 0 {
		t.Fatalf("after untick: %d selected, want 0", got)
	}
	if m.pkgSelected[0] {
		t.Fatal("index 0 still ticked after untick")
	}
}

// TestSelectAllTogglesCleanly covers the select-all key seeing a stale count.
func TestSelectAllTogglesCleanly(t *testing.T) {
	m := pkgModel(3)

	m = runKey(m, keyRune('a')) // select all
	if got := len(m.pkgSelected); got != 3 {
		t.Fatalf("after select-all: %d selected, want 3", got)
	}

	// Tick one off, then select-all again: it must select, not clear.
	m = runKey(m, keyRune(' '))
	if got := len(m.pkgSelected); got != 2 {
		t.Fatalf("after untick: %d selected, want 2", got)
	}
	m = runKey(m, keyRune('a'))
	if got := len(m.pkgSelected); got != 3 {
		t.Fatalf("select-all on a partly-ticked list: %d selected, want 3", got)
	}

	// A fully-ticked list clears.
	m = runKey(m, keyRune('a'))
	if got := len(m.pkgSelected); got != 0 {
		t.Fatalf("select-all on a fully-ticked list: %d selected, want 0", got)
	}
}

// TestPackageKeysIgnoredBeforeLoad stops a keypress that lands while the list is
// still loading from recording a selection against a package nobody has seen.
func TestPackageKeysIgnoredBeforeLoad(t *testing.T) {
	m := pkgModel(0)
	m.pkgLoaded = false

	for _, k := range []string{" ", "a", "d"} {
		got := runKey(m, keyRune(rune(k[0])))
		if len(got.pkgSelected) != 0 {
			t.Fatalf("key %q before load recorded %d selections", k, len(got.pkgSelected))
		}
		if got.state != statePackageList {
			t.Fatalf("key %q before load moved to state %v", k, got.state)
		}
	}
}

// TestCtrlCQuitsFromPackageViews makes sure ctrl+c still exits the program
// rather than being treated as "back".
func TestCtrlCQuitsFromPackageViews(t *testing.T) {
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	for _, st := range []appState{statePackageList, statePackageDeleteConfirm, statePackageDeleting} {
		m := pkgModel(3)
		m.state = st
		mm, cmd := m.Update(ctrlC)
		if cmd == nil {
			t.Fatalf("state %v: ctrl+c produced no command", st)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("state %v: ctrl+c did not quit", st)
		}
		if got := mm.(Model); got.state != st {
			t.Fatalf("state %v: ctrl+c changed state to %v", st, got.state)
		}
	}
}

// TestViewPackagesFitsTerminal checks the rendered panel never exceeds the
// terminal height, including when the status and "N selected" lines appear.
func TestViewPackagesFitsTerminal(t *testing.T) {
	for _, h := range []int{8, 12, 24, 40} {
		for _, n := range []int{0, 1, 5, 100} {
			m := pkgModel(n)
			m.height, m.width = h, 80
			m.statusMsg = "Deleted 3 package(s)."
			m.pkgSelected = map[int]bool{0: true, 1: true}
			if got := lipgloss.Height(m.viewPackages()); got > h {
				t.Fatalf("height=%d packages=%d: panel is %d lines", h, n, got)
			}
		}
	}
}

// TestEnvsReloadKeepsPackageView documents that an environment refresh landing
// while the package list is open does not navigate away from it.
func TestEnvsReloadKeepsPackageView(t *testing.T) {
	m := New()
	m.width, m.height = 100, 30
	m.condaEnvs = []conda.Env{{Name: "A"}, {Name: "B"}}
	m.rebuildItems()
	m.cursor = 1
	m.state = statePackageList
	m.pkgEnvType, m.pkgIdx = "conda", 1

	got := runKey(m, keyRune('r'))
	mm, _ := got.Update(envsLoadedMsg{condaEnvs: []conda.Env{{Name: "A"}}})
	if got := mm.(Model); got.state != statePackageList {
		t.Fatalf("state = %v, want the package list to stay open", got.state)
	}
}

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func mkPkgs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("pkg%d", i)
	}
	return out
}
