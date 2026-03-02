package display

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ModeRate represents one refresh rate within a display mode.
type ModeRate struct {
	Rate      float64
	Current   bool // marked with * in xrandr output
	Preferred bool // marked with + in xrandr output
}

// Mode represents a resolution mode available on a display.
type Mode struct {
	Name          string // e.g. "1920x1080" (used as --mode arg to xrandr)
	Width, Height int
	Rates         []ModeRate
}

// Display represents a physical connected display detected via xrandr.
type Display struct {
	Name          string
	X, Y          int
	Width, Height int
	CurrentRate   float64 // active refresh rate (0 if unknown)
	Modes         []Mode  // all available modes/rates
}

// String returns a human-readable summary.
func (d Display) String() string {
	return fmt.Sprintf("%s %dx%d+%d+%d", d.Name, d.Width, d.Height, d.X, d.Y)
}

// Detect runs xrandr and returns all connected displays with their geometry and modes.
func Detect(ctx context.Context) ([]Display, error) {
	out, err := exec.CommandContext(ctx, "xrandr", "--query").Output()
	if err != nil {
		return nil, fmt.Errorf("xrandr: %w", err)
	}
	return parse(string(out)), nil
}

// ConnectedNames returns the set of currently-connected port names (e.g. "HDMI-1", "DP-2").
// This is a fast poll used by the connection monitor.
func ConnectedNames(ctx context.Context) (map[string]struct{}, error) {
	out, err := exec.CommandContext(ctx, "xrandr", "--query").Output()
	if err != nil {
		return nil, fmt.Errorf("xrandr: %w", err)
	}
	names := make(map[string]struct{})
	for _, d := range parse(string(out)) {
		names[d.Name] = struct{}{}
	}
	return names, nil
}

// SetMode applies a resolution and refresh rate to a named output via xrandr.
// modeName is the xrandr mode string (e.g. "1920x1080"). A zero rate omits --rate.
func SetMode(ctx context.Context, output, modeName string, rate float64) error {
	args := []string{"--output", output, "--mode", modeName}
	if rate > 0 {
		args = append(args, "--rate", strconv.FormatFloat(rate, 'f', 2, 64))
	}
	if out, err := exec.CommandContext(ctx, "xrandr", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("xrandr %v: %w\n%s", args, err, out)
	}
	return nil
}

// connected line: "HDMI-1 connected 1920x1080+0+0 ..." or "... connected primary 1920x1080+0+0 ..."
var connectedRE = regexp.MustCompile(
	`^(\S+)\s+connected(?:\s+primary)?\s+(\d+)x(\d+)\+(\d+)\+(\d+)`,
)

// mode line (indented): "   1920x1080     60.00*+  50.00"
var modeLineRE = regexp.MustCompile(`^\s+(\d+x\d+i?)\s+(.+)$`)

// individual rate token, optionally tagged with * (current) and/or + (preferred)
var rateRE = regexp.MustCompile(`(\d+\.\d+)([*+]*)`)

func parse(output string) []Display {
	var displays []Display
	var current *Display

	for line := range strings.SplitSeq(output, "\n") {
		// Non-indented line — new port declaration.
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			if current != nil {
				displays = append(displays, *current)
				current = nil
			}
			if m := connectedRE.FindStringSubmatch(line); m != nil {
				w, _ := strconv.Atoi(m[2])
				h, _ := strconv.Atoi(m[3])
				x, _ := strconv.Atoi(m[4])
				y, _ := strconv.Atoi(m[5])
				current = &Display{
					Name:   m[1],
					Width:  w,
					Height: h,
					X:      x,
					Y:      y,
				}
			}
			continue
		}

		// Indented line — mode entry for the current display.
		if current == nil {
			continue
		}
		mm := modeLineRE.FindStringSubmatch(line)
		if mm == nil {
			continue
		}
		modeName := mm[1]
		mw, mh := parseModeResolution(modeName)
		mode := Mode{Name: modeName, Width: mw, Height: mh}
		for _, rm := range rateRE.FindAllStringSubmatch(mm[2], -1) {
			rate, _ := strconv.ParseFloat(rm[1], 64)
			flags := rm[2]
			mr := ModeRate{
				Rate:      rate,
				Current:   strings.Contains(flags, "*"),
				Preferred: strings.Contains(flags, "+"),
			}
			if mr.Current {
				current.CurrentRate = rate
			}
			mode.Rates = append(mode.Rates, mr)
		}
		current.Modes = append(current.Modes, mode)
	}

	if current != nil {
		displays = append(displays, *current)
	}
	return displays
}

// parseModeResolution extracts width and height from a mode name like "1920x1080" or "1920x1080i".
func parseModeResolution(name string) (w, h int) {
	name = strings.TrimSuffix(name, "i")
	parts := strings.SplitN(name, "x", 2)
	if len(parts) == 2 {
		w, _ = strconv.Atoi(parts[0])
		h, _ = strconv.Atoi(parts[1])
	}
	return
}
