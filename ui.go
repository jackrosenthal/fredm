package main

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/msteinert/pam/v2"
)

// Form layout, in cells. The form is a card of solid rectangles. Each field
// and the button is a row of text padded by half a row above and below,
// which is drawn with boxes.
const (
	formWidth  = 46
	labelWidth = 10
	cardPadX   = 4
	cardWidth  = formWidth + 2*cardPadX
	cardHeight = 20

	// Rows in the card.
	rowTitle   = 2
	rowUser    = 5
	rowPass    = 8
	rowSession = 11
	rowButton  = 14
	rowStatus  = 17
)

type field int

const (
	fieldUser field = iota
	fieldPass
	fieldSession
	fieldButton
	// fieldLayout is the layout dropdown at the top right of the screen.
	fieldLayout
	numFields
)

var fieldRows = [...]int{
	fieldUser:    rowUser,
	fieldPass:    rowPass,
	fieldSession: rowSession,
	fieldButton:  rowButton,
}

var (
	onCard       = lipgloss.NewStyle().Background(colorBlack.text)
	titleStyle   = onCard.Bold(true).Foreground(colorYellow.text)
	labelStyle   = onCard.Width(labelWidth).Foreground(colorGrey.text)
	focusedLabel = labelStyle.Bold(true).Foreground(colorPink.text)
	statusStyle  = onCard.Width(formWidth).Align(lipgloss.Center).Foreground(colorGrey.text)
	errorStyle   = statusStyle.Foreground(colorRose.text)
	arrowStyle   = lipgloss.NewStyle().Foreground(colorGrey.text)
	focusedArrow = arrowStyle.Bold(true).Foreground(colorPink.text)

	buttonStyle        = lipgloss.NewStyle().Bold(true).Foreground(colorCream.text).Background(colorPurple.text)
	focusedButtonStyle = buttonStyle.Foreground(colorBlack.text).Background(colorPink.text)
	menuButtonStyle    = onCard.Foreground(colorGrey.text)
	focusedMenuButton  = menuButtonStyle.Bold(true).Foreground(colorPink.text)
	menuItemStyle      = onCard
	hoveredMenuItem    = menuItemStyle.Bold(true).Foreground(colorBlack.text).Background(colorPink.text)
)

const buttonText = "Log in"

// loginResult is what the form hands to main after a successful login.
type loginResult struct {
	tx      *pam.Transaction
	session sessionEntry
	layout  keyLayout
}

type authMsg struct {
	tx  *pam.Transaction
	err error
}

// cellRect is a rectangle of cells on the screen.
type cellRect struct {
	x, y, w, h int
}

func (r cellRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

type model struct {
	pamService string
	hostname   string
	sessions   []sessionEntry
	layouts    []keyLayout

	user    textinput.Model
	pass    textinput.Model
	session int
	layout  int
	focus   field

	// menuOpen is whether the layout dropdown is open, and menuHover the
	// highlighted layout in it, or -1. menuPulled is whether it was opened
	// by pressing the mouse on its button, so releasing the mouse picks the
	// layout under the pointer.
	menuOpen   bool
	menuHover  int
	menuPulled bool

	// out draws the boxes, and cellW and cellH are the size of a cell in
	// pixels, or zeros if it is not known and there are no boxes.
	out          *boxOutput
	cellW, cellH int

	width, height int
	busy          bool
	status        string
	statusErr     bool
	result        *loginResult
}

func newModel(pamService, hostname string, sessions []sessionEntry, layouts []keyLayout, out *boxOutput) model {
	newInput := func() textinput.Model {
		ti := textinput.New()
		ti.Prompt = ""
		// Leave a cell of padding on each side, and one for the cursor.
		ti.SetWidth(formWidth - labelWidth - 3)
		styles := ti.Styles()
		styles.Blurred.Text = styles.Focused.Text
		ti.SetStyles(styles)
		return ti
	}

	m := model{
		pamService: pamService,
		hostname:   hostname,
		sessions:   sessions,
		layouts:    layouts,
		user:       newInput(),
		pass:       newInput(),
		out:        out,
	}
	m.pass.EchoMode = textinput.EchoPassword
	m.pass.EchoCharacter = '●'
	m.user.Focus()
	return m
}

func (m model) Init() tea.Cmd {
	// frecon keeps the keymap from the last run, so reset it to the default.
	return tea.Batch(textinput.Blink, m.setKeymap())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.out != nil {
			m.cellW, m.cellH = m.out.cellSize()
		}
		return m, nil

	case authMsg:
		m.busy = false
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			m.pass.Reset()
			return m, m.setFocus(fieldPass)
		}
		m.result = &loginResult{tx: msg.tx, session: m.sessions[m.session], layout: m.layouts[m.layout]}
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
	case tea.MouseMotionMsg:
		if m.menuPulled {
			m.menuHover = -1
			if i, ok := m.menuItemAt(msg.X, msg.Y); ok {
				m.menuHover = i
			}
		}
		return m, nil
	case tea.MouseReleaseMsg:
		if m.menuPulled {
			if i, ok := m.menuItemAt(msg.X, msg.Y); ok {
				return m, m.selectLayout(i)
			}
			m.menuOpen, m.menuPulled = false, false
		}
		return m, nil
	case tea.MouseWheelMsg:
		if f, ok := m.fieldAt(msg.X, msg.Y); ok && f == fieldSession {
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
	if m.menuOpen {
		return m.handleMenuKey(msg)
	}
	if m.focus == fieldLayout {
		switch msg.String() {
		case "enter", "space":
			m.openMenu()
			return m, nil
		}
	}
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

func (m model) handleMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	n := len(m.layouts)
	switch msg.String() {
	case "up", "shift+tab":
		m.menuHover = (max(m.menuHover, 0) + n - 1) % n
	case "down", "tab":
		m.menuHover = (m.menuHover + 1) % n
	case "enter", "space":
		if m.menuHover >= 0 {
			return m, m.selectLayout(m.menuHover)
		}
	case "esc":
		m.menuOpen, m.menuPulled = false, false
	}
	return m, nil
}

func (m model) handleClick(x, y int) (tea.Model, tea.Cmd) {
	if m.menuOpen {
		// While the dropdown is open from the keyboard, a click picks a
		// layout or closes it.
		if i, ok := m.menuItemAt(x, y); ok {
			return m, m.selectLayout(i)
		}
		m.menuOpen = false
		return m, nil
	}
	if padded(m.menuButton()).contains(x, y) {
		// Pressing the button pulls the dropdown down until the release.
		cmd := m.setFocus(fieldLayout)
		m.openMenu()
		m.menuPulled = true
		return m, cmd
	}

	f, ok := m.fieldAt(x, y)
	if !ok {
		return m, nil
	}
	cmd := m.setFocus(f)
	switch f {
	case fieldSession:
		// The left half, with the ‹, goes back, and the right half forward.
		w := m.widget(fieldSession)
		if x < w.x+w.w/2 {
			m.cycleSession(-1)
		} else {
			m.cycleSession(1)
		}
	case fieldButton:
		next, submitCmd := m.submit()
		return next, tea.Batch(cmd, submitCmd)
	}
	return m, cmd
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

func (m *model) openMenu() {
	m.menuOpen = true
	m.menuHover = m.layout
}

// selectLayout closes the dropdown and switches to layout i.
func (m *model) selectLayout(i int) tea.Cmd {
	m.menuOpen, m.menuPulled = false, false
	m.layout = i
	return m.setKeymap()
}

// setKeymap switches frecon to the selected layout.
func (m model) setKeymap() tea.Cmd {
	return tea.Raw(m.layouts[m.layout].escape())
}

func (m *model) setStatus(s string, isErr bool) {
	m.status, m.statusErr = s, isErr
}

func (m model) submit() (tea.Model, tea.Cmd) {
	user := strings.TrimSpace(m.user.Value())
	if user == "" {
		m.setStatus("Enter a user name", true)
		return m, m.setFocus(fieldUser)
	}
	m.busy = true
	m.setStatus("Authenticating…", false)
	service, password := m.pamService, m.pass.Value()
	return m, func() tea.Msg {
		tx, err := authenticate(service, user, password)
		return authMsg{tx: tx, err: err}
	}
}

// card returns the card's position on the screen.
func (m model) card() cellRect {
	return cellRect{max(0, (m.width-cardWidth)/2), max(0, (m.height-cardHeight)/2), cardWidth, cardHeight}
}

// widget returns the text row of a field or the button, which has a cell
// of padding on each side.
func (m model) widget(f field) cellRect {
	c := m.card()
	return cellRect{c.x + cardPadX + labelWidth, c.y + fieldRows[f], formWidth - labelWidth, 1}
}

// padded returns a widget's text row with the half rows that pad it, as the
// rows they are in.
func padded(r cellRect) cellRect {
	return cellRect{r.x, r.y - 1, r.w, r.h + 2}
}

// fieldAt returns the field or button under a screen position. A field's row
// includes its label.
func (m model) fieldAt(x, y int) (field, bool) {
	for f := fieldUser; f <= fieldButton; f++ {
		r := padded(m.widget(f))
		if f != fieldButton {
			r.x -= labelWidth
			r.w += labelWidth
		}
		if r.contains(x, y) {
			return f, true
		}
	}
	return 0, false
}

// menuButton returns the text row of the layout dropdown's button, at the
// top right of the screen. It is as wide as the longest layout name, so it
// does not move when the layout changes.
func (m model) menuButton() cellRect {
	w := 0
	for _, l := range m.layouts {
		w = max(w, ansi.StringWidth(l.name()))
	}
	w += 2
	return cellRect{max(0, m.width-w-2), 1, w, 1}
}

// menu returns the open dropdown, right under the button, with a row of
// padding above and below the layouts. The half row of the button's padding
// covers the top half of the row above.
func (m model) menu() cellRect {
	b := m.menuButton()
	return cellRect{b.x, b.y + 1, b.w, len(m.layouts) + 2}
}

// menuItemAt returns the layout under a screen position in the open
// dropdown.
func (m model) menuItemAt(x, y int) (int, bool) {
	r := m.menu()
	i := y - r.y - 1
	return i, r.contains(x, y) && i >= 0 && i < len(m.layouts)
}

func (m model) renderMenuButton() string {
	style := menuButtonStyle
	if m.focus == fieldLayout && !m.busy {
		style = focusedMenuButton
	}
	return style.Width(m.menuButton().w).Render(" " + m.layouts[m.layout].name())
}

func (m model) renderMenu() string {
	r := m.menu()
	rows := []string{menuItemStyle.Render(strings.Repeat(" ", r.w))}
	for i, l := range m.layouts {
		style := menuItemStyle
		if i == m.menuHover {
			style = hoveredMenuItem
		}
		rows = append(rows, style.Width(r.w).Render(" "+l.name()))
	}
	rows = append(rows, rows[0])
	return strings.Join(rows, "\n")
}

// renderInput renders a text field's row, the text between a cell of
// padding on each side.
func renderInput(r cellRect, ti textinput.Model) string {
	return " " + lipgloss.NewStyle().Width(r.w-2).Render(ti.View()) + " "
}

func (m model) renderSession(r cellRect) string {
	focused := m.focus == fieldSession && !m.busy
	arrows := arrowStyle
	if focused {
		arrows = focusedArrow
	}
	name := lipgloss.NewStyle().Bold(focused).
		Width(r.w - 4).Align(lipgloss.Center).
		Render(ansi.Truncate(m.sessions[m.session].name, r.w-4, "…"))
	return " " + arrows.Render("‹") + name + arrows.Render("›") + " "
}

func (m model) buttonFocused() bool {
	return m.focus == fieldButton && !m.busy
}

func (m model) renderButton(r cellRect) string {
	style := buttonStyle
	if m.buttonFocused() {
		style = focusedButtonStyle
	}
	return style.Width(r.w).Align(lipgloss.Center).Render(buttonText)
}

// boxes returns the escapes that draw the form's boxes, or nothing if the
// cell size is not known or the form is done.
func (m model) boxes() string {
	if m.cellW == 0 || m.cellH == 0 || m.result != nil {
		return ""
	}
	var boxes []box
	for _, f := range []field{fieldUser, fieldPass, fieldSession} {
		boxes = append(boxes, padBoxes(m.widget(f), colorBackground, m.cellW, m.cellH)...)
	}
	button := colorPurple
	if m.buttonFocused() {
		button = colorPink
	}
	boxes = append(boxes, padBoxes(m.widget(fieldButton), button, m.cellW, m.cellH)...)
	boxes = append(boxes, padBoxes(m.menuButton(), colorBlack, m.cellW, m.cellH)...)
	return boxEscapes(boxes)
}

func (m model) View() tea.View {
	c := m.card()
	formX := c.x + cardPadX

	label := func(f field, text string) *lipgloss.Layer {
		style := labelStyle
		if m.focus == f && !m.busy {
			style = focusedLabel
		}
		return lipgloss.NewLayer(style.Render(text)).X(formX).Y(c.y + fieldRows[f])
	}
	at := func(r cellRect, s string) *lipgloss.Layer {
		return lipgloss.NewLayer(s).X(r.x).Y(r.y)
	}

	status := statusStyle
	if m.statusErr {
		status = errorStyle
	}
	user, pass := m.widget(fieldUser), m.widget(fieldPass)
	session, button := m.widget(fieldSession), m.widget(fieldButton)

	layers := []*lipgloss.Layer{
		at(c, onCard.Width(c.w).Height(c.h).Render("")),
		lipgloss.NewLayer(titleStyle.Width(formWidth).Align(lipgloss.Center).
			Render(ansi.Truncate(m.hostname, formWidth, "…"))).X(formX).Y(c.y + rowTitle),
		label(fieldUser, "User"),
		at(user, renderInput(user, m.user)),
		label(fieldPass, "Password"),
		at(pass, renderInput(pass, m.pass)),
		label(fieldSession, "Session"),
		at(session, m.renderSession(session)),
		at(button, m.renderButton(button)),
		lipgloss.NewLayer(status.Render(ansi.Truncate(m.status, formWidth, "…"))).X(formX).Y(c.y + rowStatus),
		at(m.menuButton(), m.renderMenuButton()),
	}
	if m.menuOpen {
		layers = append(layers, at(m.menu(), m.renderMenu()).Z(1))
	}
	canvas := lipgloss.NewCanvas(max(m.width, c.w), max(m.height, c.h))
	canvas.Compose(lipgloss.NewCompositor(layers...))

	// The boxes go with the text they pad, and are drawn after it is.
	if m.out != nil {
		m.out.setBoxes(m.boxes())
	}

	v := tea.NewView(canvas.Render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
