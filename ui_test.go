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
	var m tea.Model = newModel("login", "testhost", sessions)
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
