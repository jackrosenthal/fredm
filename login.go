package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/msteinert/pam/v2"
	"golang.org/x/sys/unix"
)

const defaultPath = "/usr/local/sbin:/usr/local/bin:/usr/bin"

// runSession opens a PAM session for an authenticated transaction, runs the
// chosen session as the user, waits for it to exit, and closes the session.
func runSession(tx *pam.Transaction, sess sessionEntry, layout keyLayout) error {
	defer func() { _ = tx.End() }()

	name, err := tx.GetItem(pam.User)
	if err != nil {
		return fmt.Errorf("getting PAM user: %w", err)
	}
	acct, err := lookupAccount(name)
	if err != nil {
		return err
	}

	// A graphical session gets a VT of its own, and frecon keeps this one.
	var (
		tty      *os.File
		vt       int
		returnVT int
		sessEnv  = map[string]string{"XDG_SESSION_CLASS": "user"}
	)
	layout.setEnv(sessEnv)
	if sess.shell {
		ttyPath, err := os.Readlink("/proc/self/fd/0")
		if err != nil {
			return fmt.Errorf("finding terminal: %w", err)
		}
		tty = os.Stdin
		sessEnv["XDG_SESSION_TYPE"] = "tty"
		if err := tx.SetItem(pam.Tty, ttyPath); err != nil {
			return fmt.Errorf("setting PAM_TTY: %w", err)
		}
	} else {
		if returnVT, err = activeVT(); err != nil {
			return err
		}
		if vt, err = freeVT(); err != nil {
			return err
		}
		ttyPath := fmt.Sprintf("/dev/tty%d", vt)
		tty, err = os.OpenFile(ttyPath, os.O_RDWR|unix.O_NOCTTY, 0)
		if err != nil {
			return fmt.Errorf("opening %s: %w", ttyPath, err)
		}
		defer func() { _ = tty.Close() }()
		defer func() {
			if err := switchVT(returnVT); err != nil {
				slog.Warn("switching back", "vt", returnVT, "err", err)
			}
		}()

		sessEnv["XDG_SESSION_TYPE"] = "wayland"
		sessEnv["XDG_SEAT"] = "seat0"
		sessEnv["XDG_VTNR"] = fmt.Sprint(vt)
		sessEnv["XDG_SESSION_DESKTOP"] = sess.id
		if len(sess.desktopNames) > 0 {
			sessEnv["XDG_CURRENT_DESKTOP"] = strings.Join(sess.desktopNames, ":")
		}
		if err := tx.SetItem(pam.Tty, ttyPath); err != nil {
			return fmt.Errorf("setting PAM_TTY: %w", err)
		}
	}

	// Like login(1), give the user the terminal.
	if err := setTTYOwner(tty, acct.uid); err != nil {
		slog.Warn("changing terminal owner", "err", err)
	}
	defer func() {
		if err := setTTYOwner(tty, 0); err != nil {
			slog.Warn("restoring terminal owner", "err", err)
		}
	}()

	// pam_systemd reads the XDG_ variables from the PAM environment.
	for k, v := range sessEnv {
		if err := tx.PutEnv(fmt.Sprintf("%s=%s", k, v)); err != nil {
			return fmt.Errorf("setting %s: %w", k, err)
		}
	}
	if err := tx.SetCred(pam.EstablishCred); err != nil {
		return fmt.Errorf("establishing credentials: %w", err)
	}
	defer func() { _ = tx.SetCred(pam.DeleteCred) }()
	if err := tx.OpenSession(0); err != nil {
		return fmt.Errorf("opening session: %w", err)
	}
	defer func() {
		if err := tx.CloseSession(0); err != nil {
			slog.Warn("closing session", "err", err)
		}
	}()

	pamEnv, err := tx.GetEnvList()
	if err != nil {
		return fmt.Errorf("getting PAM environment: %w", err)
	}
	env := sessionEnviron(acct, sessEnv, pamEnv, sess.shell)

	cmd := sessionCommand(acct, sess)
	cmd.Env = env
	cmd.Dir = acct.home
	if _, err := os.Stat(acct.home); err != nil {
		cmd.Dir = "/"
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: acct.uid, Gid: acct.gid, Groups: acct.groups},
	}
	if sess.shell {
		// Hand the shell the foreground of this terminal.
		cmd.SysProcAttr.Foreground = true
		cmd.SysProcAttr.Ctty = int(tty.Fd())
	} else {
		cmd.SysProcAttr.Setsid = true
		cmd.SysProcAttr.Setctty = true
		cmd.SysProcAttr.Ctty = 0

		if err := switchVT(vt); err != nil {
			return err
		}
		fmt.Printf("\033[H\033[2J%s is running on tty%d.\r\n", sess.name, vt)
		fmt.Printf("Press Ctrl+Alt+F%d to return to it.\r\n", vt)

		// Keep the graphical session if frecon restarts this terminal.
		signal.Ignore(syscall.SIGHUP)
	}

	slog.Info("starting session", "user", acct.name, "session", sess.id, "vt", vt)
	err = cmd.Run()
	slog.Info("session ended", "user", acct.name, "session", sess.id, "err", err)

	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return fmt.Errorf("running session: %w", err)
	}
	return nil
}

// sessionCommand builds the command for a session. Graphical sessions run
// through the user's login shell, as with LightDM's session wrapper, so
// they get the environment from /etc/profile.
func sessionCommand(acct *account, sess sessionEntry) *exec.Cmd {
	shellName := fmt.Sprintf("-%s", filepath.Base(acct.shell))
	if sess.shell {
		return &exec.Cmd{Path: acct.shell, Args: []string{shellName}}
	}
	args := append([]string{shellName, "-c", `exec "$@"`, "fredm-session"}, sess.exec...)
	return &exec.Cmd{Path: acct.shell, Args: args}
}

// sessionEnviron builds the environment of the session from scratch,
// with the PAM environment (XDG_RUNTIME_DIR, XDG_SESSION_ID, pam_env)
// taking priority.
func sessionEnviron(acct *account, sessEnv, pamEnv map[string]string, shell bool) []string {
	vars := map[string]string{
		"HOME":    acct.home,
		"SHELL":   acct.shell,
		"USER":    acct.name,
		"LOGNAME": acct.name,
		"PATH":    defaultPath,
	}
	if shell {
		vars["TERM"] = os.Getenv("TERM")
	}
	for k, v := range sessEnv {
		vars[k] = v
	}
	for k, v := range pamEnv {
		vars[k] = v
	}

	env := make([]string, 0, len(vars))
	for k, v := range vars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(env)
	return env
}

// setTTYOwner gives the terminal to uid with mode 0620 and group tty, as
// login(1) does.
func setTTYOwner(tty *os.File, uid uint32) error {
	gid := -1
	if g, err := lookupGroupID("tty"); err == nil {
		gid = g
	}
	if err := tty.Chown(int(uid), gid); err != nil {
		return err
	}
	return tty.Chmod(0o620)
}
