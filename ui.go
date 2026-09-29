package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/msteinert/pam/v2"
)

// Form layout, in cells inside the box's border and padding.
const (
	formWidth  = 40
	labelWidth = 10

	rowUser    = 2
	rowPass    = 4
	rowSession = 6
	rowButton  = 8

	boxBorder  = 1
	boxPadVert = 1
	boxPadHorz = 3
)

type field int

const (
	fieldUser field = iota
	fieldPass
	fieldSession
	fieldButton
	numFields
)

// Colors are ANSI indices, so they follow frecon's --palette.
var (
	colorBorder = lipgloss.Color("4")
	colorAccent = lipgloss.Color("1")
	colorTitle  = lipgloss.Color("3")
	colorError  = lipgloss.Color("9")
	colorDim    = lipgloss.Color("6")

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(boxPadVert, boxPadHorz)
	titleStyle         = lipgloss.NewStyle().Bold(true).Foreground(colorTitle)
	labelStyle         = lipgloss.NewStyle().Width(labelWidth)
	focusedLabelStyle  = labelStyle.Bold(true).Foreground(colorAccent)
	buttonStyle        = lipgloss.NewStyle().Padding(0, 2).Border(lipgloss.RoundedBorder(), false, true).BorderForeground(colorBorder)
	focusedButtonStyle = buttonStyle.Bold(true).Reverse(true).Foreground(colorAccent).BorderForeground(colorAccent)
	errorStyle         = lipgloss.NewStyle().Foreground(colorError)
	dimStyle           = lipgloss.NewStyle().Foreground(colorDim)
)

const buttonText = "Log in"

// loginResult is what the form hands to main after a successful login.
type loginResult struct {
	tx      *pam.Transaction
	session sessionEntry
}

type authMsg struct {
	tx  *pam.Transaction
	err error
}

type model struct {
	pamService string
	hostname   string
	sessions   []sessionEntry

	user    textinput.Model
	pass    textinput.Model
	session int
	focus   field

	width, height int
	busy          bool
	status        string
	result        *loginResult
}

func newModel(pamService, hostname string, sessions []sessionEntry) model {
	newInput := func() textinput.Model {
		ti := textinput.New()
		ti.Prompt = ""
		ti.SetWidth(formWidth - labelWidth - 1)
		return ti
	}

	m := model{
		pamService: pamService,
		hostname:   hostname,
		sessions:   sessions,
		user:       newInput(),
		pass:       newInput(),
	}
	m.pass.EchoMode = textinput.EchoPassword
	m.pass.EchoCharacter = '●'
	m.user.Focus()
	return m
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case authMsg:
		m.busy = false
		if msg.err != nil {
			m.status = errorStyle.Render(msg.err.Error())
			m.pass.Reset()
			return m, m.setFocus(fieldPass)
		}
		m.result = &loginResult{tx: msg.tx, session: m.sessions[m.session]}
		return m, tea.Quit
	}

	if m.busy {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return m.handleClick(msg.X, msg.Y)
		}
		return m, nil
	case tea.MouseWheelMsg:
		if _, row, ok := m.formPos(msg.X, msg.Y); ok && row == rowSession {
			switch msg.Button {
			case tea.MouseWheelUp:
				m.cycleSession(-1)
			case tea.MouseWheelDown:
				m.cycleSession(1)
			}
		}
		return m, nil
	}

	return m.updateInput(msg)
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "down":
		return m, m.setFocus((m.focus + 1) % numFields)
	case "shift+tab", "up":
		return m, m.setFocus((m.focus + numFields - 1) % numFields)
	case "enter":
		if m.focus == fieldUser {
			return m, m.setFocus(fieldPass)
		}
		return m.submit()
	case "left":
		if m.focus == fieldSession {
			m.cycleSession(-1)
			return m, nil
		}
	case "right", "space":
		if m.focus == fieldSession {
			m.cycleSession(1)
			return m, nil
		}
		if m.focus == fieldButton && msg.String() == "space" {
			return m.submit()
		}
	}
	return m.updateInput(msg)
}

func (m model) handleClick(x, y int) (tea.Model, tea.Cmd) {
	col, row, ok := m.formPos(x, y)
	if !ok {
		return m, nil
	}
	switch row {
	case rowUser:
		return m, m.setFocus(fieldUser)
	case rowPass:
		return m, m.setFocus(fieldPass)
	case rowSession:
		cmd := m.setFocus(fieldSession)
		// The "‹ " before the name goes back, anything else forward.
		if col >= labelWidth && col < labelWidth+2 {
			m.cycleSession(-1)
		} else {
			m.cycleSession(1)
		}
		return m, cmd
	case rowButton:
		start, end := m.buttonSpan()
		if col >= start && col < end {
			cmd := m.setFocus(fieldButton)
			next, submitCmd := m.submit()
			return next, tea.Batch(cmd, submitCmd)
		}
	}
	return m, nil
}

func (m model) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focus {
	case fieldUser:
		m.user, cmd = m.user.Update(msg)
	case fieldPass:
		m.pass, cmd = m.pass.Update(msg)
	}
	return m, cmd
}

func (m *model) setFocus(f field) tea.Cmd {
	m.focus = f
	m.user.Blur()
	m.pass.Blur()
	switch f {
	case fieldUser:
		return m.user.Focus()
	case fieldPass:
		return m.pass.Focus()
	}
	return nil
}

func (m *model) cycleSession(delta int) {
	n := len(m.sessions)
	m.session = (m.session + delta + n) % n
}

func (m model) submit() (tea.Model, tea.Cmd) {
	user := strings.TrimSpace(m.user.Value())
	if user == "" {
		m.status = errorStyle.Render("Enter a user name")
		return m, m.setFocus(fieldUser)
	}
	m.busy = true
	m.status = dimStyle.Render("Authenticating…")
	service, password := m.pamService, m.pass.Value()
	return m, func() tea.Msg {
		tx, err := authenticate(service, user, password)
		return authMsg{tx: tx, err: err}
	}
}

// boxOrigin returns the screen position of the box's top left corner.
func (m model) boxOrigin(boxW, boxH int) (int, int) {
	return max(0, (m.width-boxW)/2), max(0, (m.height-boxH)/2)
}

// formPos converts a screen position to a column and row in the form.
func (m model) formPos(x, y int) (col, row int, ok bool) {
	box := m.renderBox()
	bx, by := m.boxOrigin(lipgloss.Width(box), lipgloss.Height(box))
	col = x - bx - boxBorder - boxPadHorz
	row = y - by - boxBorder - boxPadVert
	ok = col >= 0 && col < formWidth && row >= 0
	return col, row, ok
}

// buttonSpan returns the form columns covered by the button.
func (m model) buttonSpan() (int, int) {
	w := lipgloss.Width(buttonStyle.Render(buttonText))
	start := (formWidth - w) / 2
	return start, start + w
}

func (m model) renderBox() string {
	center := lipgloss.NewStyle().Width(formWidth).Align(lipgloss.Center)
	left := lipgloss.NewStyle().Width(formWidth)

	label := func(f field, text string) string {
		if m.focus == f && !m.busy {
			return focusedLabelStyle.Render(text)
		}
		return labelStyle.Render(text)
	}

	sessName := m.sessions[m.session].name
	if m.focus == fieldSession {
		sessName = lipgloss.NewStyle().Bold(true).Render(sessName)
	}
	arrows := dimStyle
	if m.focus == fieldSession {
		arrows = lipgloss.NewStyle().Foreground(colorAccent)
	}

	button := buttonStyle
	if m.focus == fieldButton {
		button = focusedButtonStyle
	}
	start, _ := m.buttonSpan()

	rows := []string{
		center.Render(titleStyle.Render(m.hostname)),
		"",
		left.Render(label(fieldUser, "User") + m.user.View()),
		"",
		left.Render(label(fieldPass, "Password") + m.pass.View()),
		"",
		left.Render(fmt.Sprintf("%s%s%s%s",
			label(fieldSession, "Session"), arrows.Render("‹ "), sessName, arrows.Render(" ›"))),
		"",
		left.Render(strings.Repeat(" ", start) + button.Render(buttonText)),
		"",
		center.Render(m.status),
	}
	return boxStyle.Render(strings.Join(rows, "\n"))
}

func (m model) View() tea.View {
	box := m.renderBox()
	bx, by := m.boxOrigin(lipgloss.Width(box), lipgloss.Height(box))
	indent := strings.Repeat(" ", bx)

	var b strings.Builder
	b.WriteString(strings.Repeat("\n", by))
	for i, line := range strings.Split(box, "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(indent)
		b.WriteString(line)
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
