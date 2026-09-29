package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func testModel(t *testing.T) model {
	t.Helper()
	sessions := []sessionEntry{{id: "sway", name: "Sway", exec: []string{"sway"}}, shellSession}
	layouts := []keyLayout{{layout: "us", variant: "3l"}, {layout: "us"}}
	var m tea.Model = newModel("login", "testhost", sessions, layouts, nil)
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

	x, y := find(t, m, "›")
	m = click(m, x, y)
	if got := m.sessions[m.session].name; got != "Shell" {
		t.Errorf("session after clicking › = %q, want Shell", got)
	}

	x, y = find(t, m, "‹")
	m = click(m, x, y)
	if got := m.sessions[m.session].name; got != "Sway" {
		t.Errorf("session after clicking ‹ = %q, want Sway", got)
	}
}

func TestLayoutMenuPull(t *testing.T) {
	m := testModel(t)

	bx, by := find(t, m, "us (3l)")
	if by != 1 || bx < 80 {
		t.Errorf("layout button at (%d, %d), want top right", bx, by)
	}
	m = click(m, bx, by)
	if !m.menuOpen || m.focus != fieldLayout {
		t.Fatalf("pressing the layout button: open = %v, focus = %d", m.menuOpen, m.focus)
	}

	// " us  " matches the "us" item, not "us (3l)".
	x, y := find(t, m, " us  ")
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

	x, y := find(t, m, "us (3l)")
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

	for range 5 {
		press(tea.KeyTab)
	}
	if f := m.(model).focus; f != fieldLayout {
		t.Fatalf("focus after 5 tabs = %d, want layout", f)
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

// fakeSystemctl records the systemctl commands run until the test ends, and
// fails them with err.
func fakeSystemctl(t *testing.T, err error) *[]string {
	t.Helper()
	var verbs []string
	orig := systemctl
	systemctl = func(verb string) error {
		verbs = append(verbs, verb)
		return err
	}
	t.Cleanup(func() { systemctl = orig })
	return &verbs
}

func TestPowerMenuPull(t *testing.T) {
	verbs := fakeSystemctl(t, nil)
	m := testModel(t)

	bx, by := find(t, m, powerText)
	lx, _ := find(t, m, "us (3l)")
	if by != 1 || bx >= lx {
		t.Errorf("power button at (%d, %d), want top right, left of the layout button", bx, by)
	}
	m = click(m, bx, by)
	if !m.menuOpen || m.focus != fieldPower || m.menuHover != -1 {
		t.Fatalf("pressing the power button: open = %v, focus = %d, hover = %d", m.menuOpen, m.focus, m.menuHover)
	}

	x, y := find(t, m, "Power off")
	next, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = next.(model)
	if m.menuOpen || !m.busy || m.status != "Powering off..." {
		t.Errorf("after picking power off: open = %v, busy = %v, status = %q", m.menuOpen, m.busy, m.status)
	}
	if cmd == nil {
		t.Fatal("picking power off returned no command")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if len(*verbs) != 1 || (*verbs)[0] != "poweroff" {
		t.Errorf("systemctl ran %q, want poweroff", *verbs)
	}
	if !m.busy {
		t.Error("form not busy after powering off")
	}
}

func TestMenuDragAcross(t *testing.T) {
	m := testModel(t)

	px, py := find(t, m, powerText)
	lx, ly := find(t, m, "us (3l)")
	m = click(m, px, py)
	next, _ := m.Update(tea.MouseMotionMsg{X: lx, Y: ly, Button: tea.MouseLeft})
	m = next.(model)
	if !m.menuOpen || !m.menuPulled || m.focus != fieldLayout {
		t.Fatalf("dragging onto the layout button: open = %v, pulled = %v, focus = %d", m.menuOpen, m.menuPulled, m.focus)
	}

	x, y := find(t, m, " us  ")
	next, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = next.(model)
	if got := m.layouts[m.layout].name(); m.menuOpen || got != "us" || !sendsRaw(cmd, "\033]keymap:layout=us;variant=\a") {
		t.Errorf("releasing on us after dragging across: open = %v, layout = %q", m.menuOpen, got)
	}

	// And back, from the layout pull-down to the power one.
	m = click(m, lx, ly)
	next, _ = m.Update(tea.MouseMotionMsg{X: px, Y: py, Button: tea.MouseLeft})
	m = next.(model)
	if !m.menuOpen || !m.menuPulled || m.focus != fieldPower {
		t.Errorf("dragging onto the power button: open = %v, pulled = %v, focus = %d", m.menuOpen, m.menuPulled, m.focus)
	}
}

func TestPowerMenuKeysFail(t *testing.T) {
	verbs := fakeSystemctl(t, errors.New("access denied"))
	var m tea.Model = testModel(t)
	var cmd tea.Cmd
	press := func(code rune) {
		m, cmd = m.Update(tea.KeyPressMsg{Code: code})
	}

	press(tea.KeyUp)
	press(tea.KeyUp)
	if f := m.(model).focus; f != fieldPower {
		t.Fatalf("focus after 2 ups = %d, want power", f)
	}
	press(tea.KeyEnter)
	press(tea.KeyEnter)
	if cmd != nil || !m.(model).menuOpen {
		t.Fatal("enter with nothing highlighted closed the menu or did something")
	}
	press(tea.KeyDown)
	press(tea.KeyEnter)
	if cmd == nil {
		t.Fatal("picking restart returned no command")
	}
	m, _ = m.Update(cmd())
	if len(*verbs) != 1 || (*verbs)[0] != "reboot" {
		t.Errorf("systemctl ran %q, want reboot", *verbs)
	}
	if got := m.(model); got.busy || !got.statusErr || got.status != "access denied" {
		t.Errorf("after a failed restart: busy = %v, status = %q, error = %v", got.busy, got.status, got.statusErr)
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

func TestBoxes(t *testing.T) {
	m := testModel(t)
	if got := m.boxes(); got != "" {
		t.Errorf("boxes without a cell size = %q, want none", got)
	}

	m.cellW, m.cellH = 8, 16
	b := m.widget(fieldButton)
	above := box{b.x * 8, b.y*16 - 8, b.w * 8, 8, colorPurple}.escape()
	below := box{b.x * 8, (b.y + 1) * 16, b.w * 8, 8, colorPurple}.escape()
	if got := m.boxes(); !strings.Contains(got, above) || !strings.Contains(got, below) {
		t.Errorf("boxes = %q, want the button padded by %q and %q", got, above, below)
	}

	m.setFocus(fieldButton)
	pink := box{b.x * 8, b.y*16 - 8, b.w * 8, 8, colorPink}.escape()
	if got := m.boxes(); !strings.Contains(got, pink) {
		t.Errorf("boxes with the button focused = %q, want %q", got, pink)
	}

	m.result = &loginResult{}
	if got := m.boxes(); got != "" {
		t.Errorf("boxes after logging in = %q, want none", got)
	}
}

func TestBoxOutputWrite(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	out := newBoxOutput(w)
	out.setBoxes("B")

	n, err := out.Write([]byte("text"))
	if n != 4 || err != nil {
		t.Errorf("Write = %d, %v, want 4, nil", n, err)
	}
	_ = out.Close()
	got, _ := io.ReadAll(r)
	if string(got) != "textB" {
		t.Errorf("wrote %q, want the boxes after the text", got)
	}
}

func TestTypeUserAtStart(t *testing.T) {
	var m tea.Model = testModel(t)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := m.(model).user.Value(); got != "j" {
		t.Errorf("user = %q, want %q", got, "j")
	}
}
