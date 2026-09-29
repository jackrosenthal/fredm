package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sessionEntry is something the user can log in to: a graphical session
// from a desktop entry, or a shell in this terminal.
type sessionEntry struct {
	// id is the desktop entry name without .desktop, used for
	// XDG_SESSION_DESKTOP.
	id           string
	name         string
	exec         []string
	desktopNames []string
	shell        bool
}

var shellSession = sessionEntry{id: "shell", name: "Shell", shell: true}

// loadSessions reads the desktop entries in dir, and adds the shell last.
func loadSessions(dir string) []sessionEntry {
	paths, err := filepath.Glob(filepath.Join(dir, "*.desktop"))
	if err != nil {
		slog.Warn("listing sessions", "dir", dir, "err", err)
	}
	sort.Strings(paths)

	var sessions []sessionEntry
	for _, p := range paths {
		s, err := parseDesktopEntry(p)
		if err != nil {
			slog.Warn("skipping session", "path", p, "err", err)
			continue
		}
		if s != nil {
			sessions = append(sessions, *s)
		}
	}
	return append(sessions, shellSession)
}

// parseDesktopEntry parses the keys fredm needs from a session desktop
// entry. It returns nil for entries marked Hidden or NoDisplay.
func parseDesktopEntry(path string) (*sessionEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	s := &sessionEntry{id: strings.TrimSuffix(filepath.Base(path), ".desktop")}
	inEntry := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "Name":
			s.name = value
		case "Exec":
			s.exec = splitExec(value)
		case "DesktopNames":
			s.desktopNames = strings.FieldsFunc(value, func(r rune) bool { return r == ';' })
		case "Hidden", "NoDisplay":
			if value == "true" {
				return nil, nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	if len(s.exec) == 0 {
		return nil, fmt.Errorf("no Exec key")
	}
	if s.name == "" {
		s.name = s.id
	}
	return s, nil
}

// splitExec splits an Exec value into arguments, honoring double quotes and
// backslash escapes, and dropping %-field codes, which don't apply to
// sessions.
func splitExec(value string) []string {
	var (
		args    []string
		cur     strings.Builder
		inArg   bool
		inQuote bool
	)
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c == '\\' && i+1 < len(value):
			i++
			cur.WriteByte(value[i])
			inArg = true
		case c == '"':
			inQuote = !inQuote
			inArg = true
		case (c == ' ' || c == '\t') && !inQuote:
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if inArg {
		args = append(args, cur.String())
	}

	out := args[:0]
	for _, a := range args {
		if len(a) == 2 && a[0] == '%' {
			continue
		}
		out = append(out, a)
	}
	return out
}
