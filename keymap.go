package main

import (
	"fmt"
	"strings"
)

// keyLayout is an xkb keyboard layout the user can pick on the form.
type keyLayout struct {
	layout  string
	variant string
}

// parseLayout parses a layout given as "layout" or "layout:variant".
func parseLayout(s string) (keyLayout, error) {
	layout, variant, _ := strings.Cut(s, ":")
	if layout == "" {
		return keyLayout{}, fmt.Errorf("empty layout in %q", s)
	}
	if strings.ContainsAny(s, ";\a\033") {
		return keyLayout{}, fmt.Errorf("invalid character in layout %q", s)
	}
	return keyLayout{layout: layout, variant: variant}, nil
}

func (k keyLayout) name() string {
	if k.variant == "" {
		return k.layout
	}
	return fmt.Sprintf("%s (%s)", k.layout, k.variant)
}

// escape returns frecon's escape code to switch to the layout.
func (k keyLayout) escape() string {
	return fmt.Sprintf("\033]keymap:layout=%s;variant=%s\a", k.layout, k.variant)
}

// setEnv adds the XKB_DEFAULT_ variables for the layout to env, which
// wlroots compositors read.
func (k keyLayout) setEnv(env map[string]string) {
	env["XKB_DEFAULT_LAYOUT"] = k.layout
	if k.variant != "" {
		env["XKB_DEFAULT_VARIANT"] = k.variant
	}
}
