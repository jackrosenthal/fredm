package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSplitExec(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"sway", []string{"sway"}},
		{"sway --unsupported-gpu %U", []string{"sway", "--unsupported-gpu"}},
		{`env "A=b c" run\ me`, []string{"env", "A=b c", "run me"}},
		{"  a   b ", []string{"a", "b"}},
	} {
		if got := splitExec(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("splitExec(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadSessions(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"sway.desktop": "[Desktop Entry]\nName=Sway\nExec=sway\nDesktopNames=sway;wlroots\n" +
			"[Desktop Action foo]\nName=Other\nExec=other\n",
		"hidden.desktop": "[Desktop Entry]\nName=Hidden\nExec=x\nNoDisplay=true\n",
		"noexec.desktop": "[Desktop Entry]\nName=Broken\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := loadSessions(dir)
	if len(got) != 2 {
		t.Fatalf("got %d sessions, want sway and shell: %+v", len(got), got)
	}
	sway := got[0]
	if sway.id != "sway" || sway.name != "Sway" || !slices.Equal(sway.exec, []string{"sway"}) ||
		!slices.Equal(sway.desktopNames, []string{"sway", "wlroots"}) {
		t.Errorf("sway session = %+v", sway)
	}
	if !got[1].shell {
		t.Errorf("last session = %+v, want shell", got[1])
	}
}
