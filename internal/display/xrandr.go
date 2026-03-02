package display

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Display represents a physical connected display detected via xrandr.
type Display struct {
	Name          string
	X, Y          int
	Width, Height int
}

// String returns a human-readable summary.
func (d Display) String() string {
	return fmt.Sprintf("%s %dx%d+%d+%d", d.Name, d.Width, d.Height, d.X, d.Y)
}

// Detect runs xrandr and returns all connected displays with their geometry.
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

// connected line pattern: "HDMI-1 connected 1920x1080+0+0 ..."
// also handles: "HDMI-1 connected primary 1920x1080+0+0 ..."
var connectedRE = regexp.MustCompile(
	`^(\S+)\s+connected(?:\s+primary)?\s+(\d+)x(\d+)\+(\d+)\+(\d+)`,
)

func parse(output string) []Display {
	var displays []Display
	for _, line := range strings.Split(output, "\n") {
		m := connectedRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		w, _ := strconv.Atoi(m[2])
		h, _ := strconv.Atoi(m[3])
		x, _ := strconv.Atoi(m[4])
		y, _ := strconv.Atoi(m[5])
		displays = append(displays, Display{
			Name:   m[1],
			Width:  w,
			Height: h,
			X:      x,
			Y:      y,
		})
	}
	return displays
}
