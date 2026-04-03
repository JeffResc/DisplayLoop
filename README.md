# DisplayLoop

A digital signage management system for controlling multiple screens with scheduled content playback. Built in Go, DisplayLoop provides a web-based interface for uploading media, configuring operating hours, and remotely managing displays.

## Features

- **Multi-Screen Management** - Auto-detect connected displays via xrandr with independent configuration per screen
- **Content Scheduling** - Per-day operating hours (Monday-Sunday) with configurable start/end times
- **Media Playback** - Video playback via VLC and image display via feh, with automatic crash recovery
- **Off-Hours Modes** - Black screen, custom image, desktop, or no action when outside operating hours
- **Remote Access** - VNC viewer built into the web UI for each screen via x11vnc
- **Data Retention** - Automatic cleanup of old media files and audit logs on a configurable schedule
- **Audit Logging** - Full history of content changes, scheduling updates, and screen events
- **Live Status Updates** - Real-time dashboard via Server-Sent Events (SSE)

## Prerequisites

- Go 1.25.0 or later
- [VLC](https://www.videolan.org/) - video playback
- [feh](https://feh.finalrewind.org/) - image display
- [x11vnc](https://github.com/LibVNC/x11vnc) - remote screen access
- [xrandr](https://www.x.org/wiki/Projects/XRandR/) - display detection
- An X11 display environment

## Building

```bash
go build -o displayloop ./cmd/displayloop
```

To inject version information:

```bash
go build -ldflags "-X main.version=1.0.0 -X main.commit=$(git rev-parse --short HEAD)" -o displayloop ./cmd/displayloop
```

## Configuration

Create a `config.toml` file (or use the provided default):

```toml
[server]
port = 8080
uploads_dir = "./uploads"

[retention]
# Delete audit log entries older than this many days
audit_days = 365
# Delete non-current media files older than this many days
scrub_days = 30
```

## Usage

```bash
# Start the server
./displayloop

# Development mode (skips VLC process spawning)
./displayloop -no-vlc
```

The web interface is available at `http://localhost:8080` by default.

### Supported Media Formats

- **Video**: mp4, mkv, avi, mov, wmv, flv, webm, m4v
- **Image**: jpg, png, gif, bmp, webp

### VNC Access

Each screen gets a dedicated VNC port calculated as the configured base port (default `5900`) plus the screen ID. VNC sessions can also be accessed directly through the web UI.

## API

| Endpoint | Description |
|----------|-------------|
| `GET /api/status` | JSON status snapshot of all screens |
| `GET /api/status/stream` | SSE stream of live status updates |

## Project Structure

```
cmd/displayloop/      Entry point
internal/
  config/             TOML configuration parsing
  db/                 SQLite database layer
  display/            xrandr-based display detection
  handler/            HTTP handlers and templates
  monitor/            Physical display monitoring
  player/             VLC/feh subprocess management
  scheduler/          Operating hours scheduling
  scrubber/           Media cleanup and retention
  vnc/                x11vnc session management
assets/
  templates/          HTML templates (Tailwind CSS + HTMX)
  static/             Static assets
```
