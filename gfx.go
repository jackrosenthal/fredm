package main

import (
	"fmt"
	"image/color"
	"os"
	"slices"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"golang.org/x/sys/unix"
)

// paletteColor is a color of frecon's WildCherry palette. Text uses its ANSI
// index, so frecon draws it from the palette, but boxes need the RGB value.
type paletteColor struct {
	text color.Color
	rgb  uint32
}

var (
	// colorBackground is the default background, which text gets by not
	// setting one.
	colorBackground = paletteColor{nil, 0x1f1626}
	colorBlack      = paletteColor{lipgloss.Color("0"), 0x000506}
	colorPink       = paletteColor{lipgloss.Color("1"), 0xd94085}
	colorYellow     = paletteColor{lipgloss.Color("3"), 0xffd06e}
	colorPurple     = paletteColor{lipgloss.Color("4"), 0x873bdb}
	colorGrey       = paletteColor{lipgloss.Color("6"), 0xc1b8b6}
	colorCream      = paletteColor{lipgloss.Color("7"), 0xfff8dd}
	colorRose       = paletteColor{lipgloss.Color("9"), 0xda6bab}
)

// box is a rectangle drawn with frecon's box escape, in pixels.
type box struct {
	x, y, w, h int
	color      paletteColor
}

func (b box) escape() string {
	return fmt.Sprintf("\033]box:color=0x%06x;size=%d,%d;location=%d,%d\a",
		b.color.rgb, b.w, b.h, b.x, b.y)
}

// padBoxes returns the boxes that pad a one-row widget by half a row above
// and below, so it is two rows high with its text centered. cw and ch are the
// size of a cell in pixels.
func padBoxes(r cellRect, c paletteColor, cw, ch int) []box {
	x, w, half := r.x*cw, r.w*cw, ch/2
	return []box{
		{x, r.y*ch - half, w, half, c},
		{x, (r.y + 1) * ch, w, half, c},
	}
}

func boxEscapes(boxes []box) string {
	var b strings.Builder
	for _, bx := range boxes {
		b.WriteString(bx.escape())
	}
	return b.String()
}

// boxOutput is the terminal the form is drawn on. frecon paints text over
// boxes in the cells it changes, so boxOutput draws the form's boxes again
// after everything written to the terminal.
type boxOutput struct {
	f     *os.File
	mu    sync.Mutex
	boxes string
}

func newBoxOutput(f *os.File) *boxOutput {
	return &boxOutput{f: f}
}

// setBoxes sets the escapes that draw the boxes, drawn after the next write.
func (o *boxOutput) setBoxes(s string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.boxes = s
}

// Write writes p and the boxes together, so frecon draws the boxes on top of
// the text in p.
func (o *boxOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n, err := o.f.Write(append(slices.Clip(p), o.boxes...))
	return min(n, len(p)), err
}

func (o *boxOutput) WriteString(s string) (int, error) {
	return o.Write([]byte(s))
}

func (o *boxOutput) Read(p []byte) (int, error) { return o.f.Read(p) }
func (o *boxOutput) Close() error               { return o.f.Close() }
func (o *boxOutput) Fd() uintptr                { return o.f.Fd() }

// cellSize returns the size of a cell in pixels, from the window size frecon
// sets on the terminal, or zeros if it is not known.
func (o *boxOutput) cellSize() (int, int) {
	ws, err := unix.IoctlGetWinsize(int(o.f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 0, 0
	}
	return int(ws.Xpixel / ws.Col), int(ws.Ypixel / ws.Row)
}
