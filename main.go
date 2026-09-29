// fredm is a minimal login manager for frecon. frecon runs it in place of
// agetty with --login-cmd. It asks for a user name, password, and session,
// then starts the session: a graphical session on a VT of its own, or a
// shell in the frecon terminal. It exits when the session ends, and frecon
// starts it again.
package main

import (
	"fmt"
	"log/slog"
	"log/syslog"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/alecthomas/kong"
	fay "github.com/jackrosenthal/fay"
)

type cli struct {
	PAMService  string   `name:"pam-service" default:"login" help:"PAM service to authenticate with."`
	SessionsDir string   `name:"sessions-dir" default:"/usr/share/wayland-sessions" help:"Directory of session desktop entries."`
	Layouts     []string `name:"layouts" default:"us:3l,us" help:"Keyboard layouts to choose from, as layout or layout:variant. The first is the default."`
}

func main() {
	var args cli
	kong.Parse(
		&args,
		kong.Description("Minimal login manager for frecon."),
		fay.Register(),
	)

	// stderr is the terminal the form is drawn on, so log to the journal.
	if w, err := syslog.New(syslog.LOG_AUTHPRIV|syslog.LOG_INFO, "fredm"); err == nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(w, nil)))
	}

	if err := run(args); err != nil {
		slog.Error("fredm failed", "err", err)
		fmt.Fprintf(os.Stderr, "fredm: %v\n", err)
		os.Exit(1)
	}
}

func run(args cli) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root")
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "fredm"
	}

	var layouts []keyLayout
	for _, s := range args.Layouts {
		l, err := parseLayout(s)
		if err != nil {
			return err
		}
		layouts = append(layouts, l)
	}
	if len(layouts) == 0 {
		return fmt.Errorf("no keyboard layouts")
	}

	m := newModel(args.PAMService, hostname, loadSessions(args.SessionsDir), layouts)
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return fmt.Errorf("running form: %w", err)
	}

	res := final.(model).result
	if res == nil {
		return nil
	}
	return runSession(res.tx, res.session, res.layout)
}
