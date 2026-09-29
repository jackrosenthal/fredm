package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func testModel(t *testing.T) model {
	t.Helper()
	sessions := []sessionEntry{{id: "sway", name: "Sway", exec: []string{"sway"}}, shellSession}
	layouts := []keyLayout{{layout: "us", variant: "3l"}, {layout: "us"}}
	var m tea.Model = newModel("login", "testhost", sessions, layouts)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m.(model)
}

// find returns the screen position of the first occurrence of s.
func find(t *testing.T, m model, s string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if i := strings.Index(line, s); i >= 0 {
			return ansi.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("%q not on screen", s)
	return 0, 0
}

func click(m model, x, y int) model {
	next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return next.(model)
}

func TestClickFocusesFields(t *testing.T) {
	m := testModel(t)

	x, y := find(t, m, "Password")
	m = click(m, x+20, y)
	if m.focus != fieldPass {
		t.Errorf("focus after clicking password row = %d, want %d", m.focus, fieldPass)
	}

	x, y = find(t, m, "User")
	m = click(m, x, y)
	if m.focus != fieldUser {
		t.Errorf("focus after clicking user row = %d, want %d", m.focus, fieldUser)
	}
}

func TestClickCyclesSession(t *testing.T) {
	m := testModel(t)

	x, y := find(t, m, "Sway ›")
	m = click(m, x, y)
	if got := m.sessions[m.session].name; got != "Shell" {
		t.Errorf("session after clicking name = %q, want Shell", got)
	}

	x, y = find(t, m, "‹")
	m = click(m, x, y)
	if got := m.sessions[m.session].name; got != "Sway" {
		t.Errorf("session after clicking ‹ = %q, want Sway", got)
	}
}

func TestLayoutMenuPull(t *testing.T) {
	m := testModel(t)

	bx, by := find(t, m, "us (3l) ▾")
	if by != 0 || bx < 80 {
		t.Errorf("layout button at (%d, %d), want top right", bx, by)
	}
	m = click(m, bx, by)
	if !m.menuOpen || m.focus != fieldLayout {
		t.Fatalf("pressing the layout button: open = %v, focus = %d", m.menuOpen, m.focus)
	}

	// "│ us  " matches the "us" item, not "us (3l)".
	x, y := find(t, m, "│ us  ")
	x += 2
	next, _ := m.Update(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = next.(model)
	if m.menuHover != 1 {
		t.Errorf("hover after dragging to us = %d, want 1", m.menuHover)
	}

	next, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = next.(model)
	if m.menuOpen {
		t.Error("menu still open after releasing on a layout")
	}
	if got := m.layouts[m.layout].name(); got != "us" {
		t.Errorf("layout = %q, want us", got)
	}
	if !sendsRaw(cmd, "\033]keymap:layout=us;variant=\a") {
		t.Error("releasing on a layout did not send the keymap escape")
	}
}

func TestLayoutMenuReleaseOutside(t *testing.T) {
	m := testModel(t)

	x, y := find(t, m, "us (3l) ▾")
	m = click(m, x, y)
	next, _ := m.Update(tea.MouseMotionMsg{X: 0, Y: 10, Button: tea.MouseLeft})
	m = next.(model)
	if m.menuHover != -1 {
		t.Errorf("hover off the menu = %d, want -1", m.menuHover)
	}
	next, cmd := m.Update(tea.MouseReleaseMsg{X: 0, Y: 10, Button: tea.MouseLeft})
	m = next.(model)
	if m.menuOpen || m.layout != 0 || cmd != nil {
		t.Errorf("release off the menu: open = %v, layout = %d", m.menuOpen, m.layout)
	}

	// A click on the button without a drag opens and closes it.
	m = click(m, x, y)
	next, _ = m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = next.(model)
	if m.menuOpen || m.layout != 0 {
		t.Errorf("click on the button: open = %v, layout = %d", m.menuOpen, m.layout)
	}
}

func TestLayoutMenuKeys(t *testing.T) {
	var m tea.Model = testModel(t)
	press := func(code rune) tea.Cmd {
		var cmd tea.Cmd
		m, cmd = m.Update(tea.KeyPressMsg{Code: code})
		return cmd
	}

	press(tea.KeyTab)
	press(tea.KeyTab)
	press(tea.KeyTab)
	press(tea.KeyTab)
	if f := m.(model).focus; f != fieldLayout {
		t.Fatalf("focus after 4 tabs = %d, want layout", f)
	}
	press(tea.KeyEnter)
	if !m.(model).menuOpen {
		t.Fatal("enter did not open the menu")
	}
	press(tea.KeyEscape)
	if m.(model).menuOpen || m.(model).layout != 0 {
		t.Error("escape did not close the menu unchanged")
	}

	press(tea.KeyEnter)
	press(tea.KeyDown)
	if !sendsRaw(press(tea.KeyEnter), "\033]keymap:layout=us;variant=\a") {
		t.Error("picking a layout did not send the keymap escape")
	}
	if got := m.(model).layouts[m.(model).layout].name(); got != "us" {
		t.Errorf("layout = %q, want us", got)
	}
}

func TestInitSetsKeymap(t *testing.T) {
	m := testModel(t)
	if !sendsRaw(m.Init(), "\033]keymap:layout=us;variant=3l\a") {
		t.Error("Init did not send the default keymap escape")
	}
}

// sendsRaw reports whether cmd, or a command it batches, writes s.
func sendsRaw(cmd tea.Cmd, s string) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case tea.RawMsg:
		return msg.Msg == s
	case tea.BatchMsg:
		for _, c := range msg {
			if sendsRaw(c, s) {
				return true
			}
		}
	}
	return false
}

func TestClickButtonNeedsUser(t *testing.T) {
	m := testModel(t)

	x, y := find(t, m, buttonText)
	m = click(m, x, y)
	if m.busy {
		t.Error("submitted without a user name")
	}
	if m.focus != fieldUser {
		t.Errorf("focus = %d, want user field", m.focus)
	}

	x, y = find(t, m, buttonText)
	m = click(m, x-20, y)
	if m.focus != fieldUser {
		t.Error("click beside the button changed focus")
	}
}

func TestClickOutsideBox(t *testing.T) {
	m := testModel(t)
	m = click(m, 0, 0)
	if m.focus != fieldUser {
		t.Errorf("focus = %d, want user field", m.focus)
	}
}

func TestTypeUserAtStart(t *testing.T) {
	var m tea.Model = testModel(t)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := m.(model).user.Value(); got != "j" {
		t.Errorf("user = %q, want %q", got, "j")
	}
}
