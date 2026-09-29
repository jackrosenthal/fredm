package main

import (
	"errors"
	"log/slog"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const powerText = "Power"

// powerAction is an item in the power pull-down.
type powerAction struct {
	name   string
	verb   string // the systemctl command
	status string
}

var powerActions = []powerAction{
	{name: "Restart", verb: "reboot", status: "Restarting..."},
	{name: "Power off", verb: "poweroff", status: "Powering off..."},
}

type powerMsg struct {
	err error
}

// systemctl runs a systemctl command. Tests replace it.
var systemctl = func(verb string) error {
	out, err := exec.Command("systemctl", verb).CombinedOutput()
	if err != nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return errors.New(s)
		}
	}
	return err
}

// power starts a power action. systemd stops fredm when it goes through, so
// the form stays busy unless it fails.
func (m *model) power(a powerAction) tea.Cmd {
	m.busy = true
	m.setStatus(a.status, false)
	return func() tea.Msg {
		slog.Info("power action", "verb", a.verb)
		err := systemctl(a.verb)
		if err != nil {
			slog.Error("power action failed", "verb", a.verb, "err", err)
		}
		return powerMsg{err: err}
	}
}
