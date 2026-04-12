# DisplayLoop Repository Audit Report

**Date:** 2026-04-12
**Repository:** jeffresc/displayloop
**Version Audited:** 1.0.0 (commit history through current HEAD)
**Auditor:** Automated best-practices audit

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Project Overview](#project-overview)
3. [Security Audit](#security-audit)
4. [Code Quality Audit](#code-quality-audit)
5. [CI/CD and DevOps Audit](#cicd-and-devops-audit)
6. [Consolidated Findings](#consolidated-findings)
7. [Recommendations](#recommendations)

---

## Executive Summary

DisplayLoop is a well-architected Go-based digital signage management system. The codebase demonstrates strong fundamentals: clean modular design, idiomatic Go conventions, comprehensive linting configuration, parameterized SQL queries, and a solid release automation pipeline.

The primary areas for improvement are:

- **No automated tests** -- the single most impactful gap in the project
- **No authentication or authorization** -- all endpoints are publicly accessible
- **No HTTPS/TLS** -- traffic is unencrypted by default
- **No CSRF protection** -- state-changing POST endpoints lack token validation
- **Missing security headers** -- no defense-in-depth HTTP headers

### Overall Scores

| Category | Score | Rating |
|---|---|---|
| Code Organization & Architecture | 9/10 | Excellent |
| Code Quality & Naming | 9/10 | Excellent |
| Error Handling | 8/10 | Good |
| Type Safety | 9/10 | Excellent |
| Linting & Formatting | 9/10 | Excellent |
| Release & Versioning | 9/10 | Excellent |
| CI/CD Pipeline | 6/10 | Adequate |
| Security (Application) | 3/10 | Needs Work |
| Security (Injection Prevention) | 10/10 | Excellent |
| Test Coverage | 0/10 | Missing |
| Documentation | 7/10 | Good |
| Dependency Management | 5/10 | Adequate |

---

## Project Overview

DisplayLoop is a monolithic Go binary that manages multi-screen digital signage deployments. It provides a web-based interface for uploading media, configuring per-screen operating hours, and remotely managing displays via VNC.

**Tech Stack:**
- **Language:** Go 1.25.0
- **Web Framework:** chi/v5 (HTTP routing)
- **Database:** SQLite via modernc.org/sqlite (pure Go, no CGO)
- **Frontend:** Go html/template + HTMX + Tailwind CSS
- **External Tools:** VLC (video playback), feh (image display), x11vnc (remote access), xrandr (display detection)

**Codebase Size:** ~3,965 lines across 18 Go source files, 6 HTML templates, and 4 configuration files.

**Architecture:**
```
cmd/displayloop/main.go         Entry point, wiring, HTTP server
internal/config/                 TOML configuration with sensible defaults
internal/db/                     SQLite schema, migrations, query layer
internal/display/                xrandr parsing and display mode setting
internal/handler/                HTTP handlers (7 files)
internal/monitor/                Physical display connect/disconnect polling
internal/player/                 VLC/feh subprocess management
internal/scheduler/              Operating hours evaluation loop
internal/scrubber/               Media file and audit log retention cleanup
internal/vnc/                    x11vnc subprocess management
assets/                          Embedded templates and static files
```

---

## Security Audit

### SEC-01: No Authentication or Authorization [CRITICAL]

**Severity:** Critical
**Location:** Entire application

The application has no authentication or authorization controls. All HTTP endpoints -- including content upload, screen deletion, configuration changes, and VNC access -- are accessible to any client with network access.

**Evidence:**
- No authentication middleware in `cmd/displayloop/main.go:100-127`
- No session management
- No user model or roles
- All routes are publicly accessible

**Recommendation:** Implement authentication middleware. At minimum, HTTP Basic Auth or token-based authentication should be added before any network-exposed deployment. Consider integrating with an external identity provider for more complex environments.

---

### SEC-02: No HTTPS/TLS Support [HIGH]

**Severity:** High
**Location:** `cmd/displayloop/main.go:129-145`

The server listens on plaintext HTTP only. VNC WebSocket connections use `ws://` instead of `wss://`. x11vnc is launched with `-nossl` (`internal/vnc/manager.go:142`).

**Recommendation:** Deploy behind a TLS-terminating reverse proxy (nginx, Caddy), or add native TLS support using `tls.ListenAndServeTLS()`. Document the deployment pattern in the README.

---

### SEC-03: No CSRF Protection [MEDIUM]

**Severity:** Medium
**Location:** All POST handlers in `internal/handler/screens.go`, `internal/handler/upload.go`

State-changing POST endpoints do not validate CSRF tokens. An attacker could craft a malicious page that submits forms to a locally-running DisplayLoop instance.

**Recommendation:** Add CSRF token middleware. Libraries like `gorilla/csrf` integrate well with chi.

---

### SEC-04: Missing Security Headers [MEDIUM]

**Severity:** Medium
**Location:** HTTP response configuration

The application does not set security headers on responses:
- `X-Frame-Options` -- missing (clickjacking risk)
- `X-Content-Type-Options` -- missing (MIME sniffing risk)
- `Content-Security-Policy` -- missing
- `Strict-Transport-Security` -- missing

**Recommendation:** Add a middleware that sets standard security headers on all responses.

---

### SEC-05: WebSocket Origin Check Allows All Origins [LOW]

**Severity:** Low
**Location:** `internal/handler/vnc.go:16-18`

```go
var vncUpgrader = websocket.Upgrader{
    CheckOrigin: func(_ *http.Request) bool { return true },
}
```

The comment notes this is safe because x11vnc binds to `127.0.0.1`, but the WebSocket proxy itself is exposed on the application's public address. A malicious website could establish a WebSocket connection to the VNC proxy.

**Recommendation:** Validate the `Origin` header against the expected host, or restrict this behind authentication.

---

### SEC-06: Injection Prevention [PASS]

**Status:** Excellent -- no vulnerabilities found.

- **SQL Injection:** All database queries use parameterized `?` placeholders. The dynamic IN clause in `GetCurrentMediaBulk` (`internal/db/queries.go:191-195`) correctly constructs placeholders from a generated slice, not user input.
- **Command Injection:** All subprocess invocations use `exec.CommandContext()` with argument slices, never shell string interpolation.
- **XSS:** Go's `html/template` package provides context-aware auto-escaping. No use of `template.HTML` or raw HTML injection was found.
- **Path Traversal:** `HandleMediaServe` (`internal/handler/upload.go:109-113`) properly rejects filenames containing `..` or `/`, and uses `filepath.Base()`.

---

### SEC-07: File Permissions [LOW]

**Severity:** Low
**Location:** `internal/handler/upload.go:50`, `internal/handler/upload.go:124`

Upload directories are created with `0755` permissions. Uploaded files inherit the process umask, potentially making media world-readable.

**Recommendation:** Use `0700` for directories and `0600` for files if media content is sensitive.

---

### SEC-08: .gitignore and Secret Management [PASS]

**Status:** Good.

- `.gitignore` excludes `uploads/` and `displayloop.db`
- No `.env` files, API keys, passwords, or tokens in source code
- Configuration loaded from `config.toml` with safe defaults; the file is not committed

---

## Code Quality Audit

### CQ-01: Code Organization and Architecture [EXCELLENT]

The codebase follows idiomatic Go project layout with a clean `cmd/` entry point and `internal/` packages. Each package has a single clear responsibility:

- **Dependency injection** is explicit via struct fields (`handler.App` holds all dependencies)
- **No circular imports** between packages
- **Factory functions** (`New()`) used consistently across all packages
- **Embedded assets** via `//go:embed` keep deployment as a single binary

---

### CQ-02: Naming Conventions [EXCELLENT]

- Exported types properly capitalized: `Screen`, `Media`, `Manager`, `Status`
- Unexported helpers properly lowercased: `screenEntry`, `vncProc`, `scanner`
- Consistent single-letter receivers: `(a *App)`, `(m *Manager)`, `(s *Scheduler)`
- Descriptive function names: `HandleScreenDetail`, `EnsureRunning`, `PlayOffHours`
- No naming inconsistencies found across the codebase

---

### CQ-03: Error Handling [GOOD]

**Strengths:**
- Consistent use of `fmt.Errorf` with `%w` for error wrapping
- Proper use of `errors.Is()` for sentinel error checks (e.g., `sql.ErrNoRows` at `internal/db/queries.go:218`)
- HTTP handlers consistently return appropriate status codes

**Concerns:**
- 13+ instances of `_ = db.InsertAuditLog()` suppress audit logging errors silently. While this is intentional (audit logging is best-effort), it could mask database issues. Consider logging these at debug level.
- `_ = srv.Shutdown()` at `cmd/displayloop/main.go:154` -- acceptable for shutdown path but worth a comment.

---

### CQ-04: Type Safety [EXCELLENT]

- Strong typing throughout with proper nullable types (`*int`, `*time.Time`)
- SQL nullable conversions handled correctly with `sql.NullInt64`, `sql.NullBool`, `sql.NullTime`
- Status enum with `String()` method (`internal/player/manager.go:21-44`)
- `any` type used only where necessary (JSON marshaling, template function maps)

---

### CQ-05: Code Duplication [MODERATE]

Some duplication exists but is within acceptable bounds for Go:

1. **Audit entry scanning** -- `ListAuditLog` (`internal/db/queries.go:265-320`) and `GetAuditEntry` (`internal/db/queries.go:322-357`) repeat similar JOIN + scan logic. Could extract a shared scan helper.
2. **Apply-all handlers** -- `HandleScreenApplyHoursAll` and `HandleScreenApplyOffHoursAll` in `internal/handler/screens.go` follow nearly identical patterns (list screens, loop, update, audit log).
3. **Image file type detection** -- `detectMediaType()` in `internal/handler/upload.go:140-149` and `isImageFile()` in `internal/player/manager.go:382-389` both maintain separate file extension lists.

---

### CQ-06: Dead Code [PASS]

No dead code, unused imports, or unreachable code found. All exported functions are referenced. The `GetAllStatuses()` method (`internal/player/manager.go:238-251`) is the only potentially unused public method, though it may be intended for future API expansion.

---

### CQ-07: Function Complexity [GOOD]

Most functions are appropriately sized. The longest functions are:

| Function | File | Lines | Notes |
|---|---|---|---|
| `main()` | `cmd/displayloop/main.go` | 118 | Orchestration code; acceptable |
| `HandleUpload` | `internal/handler/upload.go` | 85 | Multi-step file handling; acceptable |
| `HandleVNCProxy` | `internal/handler/vnc.go` | 82 | WebSocket proxy; inherent complexity |
| `parse` | `internal/display/xrandr.go` | 59 | Regex-based parsing; well-structured |

No function exceeds a reasonable complexity threshold.

---

### CQ-08: Concurrency [GOOD]

- Proper mutex usage: `sync.RWMutex` in player Manager and SSEHub
- Context-based cancellation throughout all background goroutines
- SQLite single-connection pool (`SetMaxOpenConns(1)`) prevents concurrent write issues
- `WaitGroup` used correctly in VNC proxy bidirectional forwarding (`internal/handler/vnc.go:75-115`)
- Process reaping handled properly with `done` channels (`internal/player/manager.go:202-216`)

**Minor note:** `vlcBackground = context.Background()` (`internal/player/manager.go:18`) is a module-level variable. While functionally correct (VLC processes outlive request contexts), passing it as a parameter would improve testability.

---

### CQ-09: Logging [GOOD]

- Uses standard `log` package with consistent module prefixes (`"monitor:"`, `"scheduler:"`, `"scrubber:"`, `"vnc:"`)
- Critical startup errors use `log.Fatalf()` appropriately
- Health check failures logged with screen IDs for debugging
- chi's `middleware.Logger` provides request logging

**Improvement:** Consider migrating to Go's `log/slog` (structured logging) for production observability with JSON output.

---

### CQ-10: Documentation [GOOD]

**Strengths:**
- Package-level comments on `handler`, `monitor`, `assets` packages
- Build variable injection documented (`cmd/displayloop/main.go:31-32`)
- Template parsing strategy explained (`cmd/displayloop/main.go:157-159`)
- HTTP timeout decisions documented inline (`cmd/displayloop/main.go:133-137`)
- xrandr regex patterns documented (`internal/display/xrandr.go:76-85`)

**Gaps:**
- Missing package comments on `player`, `scheduler`, `scrubber`, `vnc`, `config`, `db`
- No GoDoc examples
- Thread-safety guarantees not documented on Manager types

---

## CI/CD and DevOps Audit

### CD-01: CI Pipeline [GOOD]

**PR Workflow** (`.github/workflows/pr.yml`):
- Linting with golangci-lint (comprehensive configuration)
- Build verification for linux/amd64
- Uses `actions/checkout@v6` and `actions/setup-go@v6` (current versions)
- Go version read from `go.mod` (good practice)

**Gap:** No test execution step (`go test ./...` is not run in CI because no tests exist).

---

### CD-02: Release Pipeline [EXCELLENT]

**Release Workflow** (`.github/workflows/release.yml`):
- Automated semantic versioning via release-please
- Multi-architecture builds (linux/amd64, linux/arm64)
- Version and commit SHA injected via ldflags
- Binaries attached to GitHub releases
- Changelog auto-generated from conventional commits

---

### CD-03: Linting Configuration [EXCELLENT]

`.golangci.yml` enables 13 linters including:
- `errcheck`, `errorlint`, `nilerr` -- error handling correctness
- `staticcheck`, `gocritic` -- static analysis
- `bodyclose`, `noctx` -- HTTP best practices
- `revive` with 19 rules -- comprehensive style enforcement
- `gofmt`, `goimports` -- formatting

**Issue:** `version: latest` used for golangci-lint in CI. Pin to a specific version to prevent unexpected CI failures when new linter versions release.

---

### CD-04: No Automated Tests [CRITICAL]

**Severity:** Critical

Zero test files exist in the repository. No `*_test.go` files, no test data, no benchmarks. The golangci-lint config explicitly sets `tests: false`.

This is the single largest gap in the project. Key areas that would benefit most from tests:

1. `scheduler.IsWithinHours()` -- time-based logic is error-prone and ideal for table-driven tests
2. `display.parse()` -- regex parsing of xrandr output, easy to test with fixtures
3. `handler.detectMediaType()` and `player.isImageFile()` -- simple pure functions
4. `db` package -- query correctness with an in-memory SQLite database
5. `handler` package -- HTTP handler tests with `httptest.NewServer`

**Recommendation:** Start with unit tests for pure functions and the scheduler, then expand to integration tests for the HTTP layer.

---

### CD-05: golangci-lint Version Not Pinned [LOW]

**Severity:** Low
**Location:** `.github/workflows/pr.yml:23`

```yaml
- name: golangci-lint
  uses: golangci/golangci-lint-action@v9
  with:
    version: latest
```

Using `latest` can cause unexpected CI failures when a new linter release introduces stricter checks.

**Recommendation:** Pin to a specific version (e.g., `version: v2.1.6`).

---

### CD-06: No Docker Support [INFORMATIONAL]

No Dockerfile or docker-compose.yml exists. The application is distributed as compiled binaries, which is appropriate for a single-binary Go application targeting Linux-based signage hardware. Docker may not be necessary for the target deployment scenario, but would aid reproducible builds and testing.

---

### CD-07: No Dependency Update Automation [MEDIUM]

**Severity:** Medium

Git history shows Renovate was previously configured but the configuration file has been removed. Dependencies are now updated manually.

**Current dependencies** (all appear up-to-date as of audit date):
- `github.com/go-chi/chi/v5 v5.2.5`
- `github.com/gorilla/websocket v1.5.3`
- `modernc.org/sqlite v1.48.2`
- `github.com/BurntSushi/toml v1.6.0`
- `github.com/google/uuid v1.6.0`

**Recommendation:** Restore Renovate or add Dependabot configuration to automate dependency updates and security patches.

---

### CD-08: No Environment Variable Override [LOW]

**Severity:** Low
**Location:** `internal/config/config.go`

Configuration is TOML-file-only with no environment variable override support. This complicates deployment in containerized or cloud environments where environment variables are the standard configuration mechanism.

**Recommendation:** Add environment variable overrides for key settings (port, uploads directory, VNC base port).

---

### CD-09: Release Artifact Integrity [LOW]

**Severity:** Low
**Location:** `.github/workflows/release.yml`

Release binaries are not accompanied by checksums or GPG signatures. Users cannot verify download integrity.

**Recommendation:** Add a step to generate SHA256 checksums and upload them alongside binaries.

---

## Consolidated Findings

### Critical

| ID | Finding | Category |
|---|---|---|
| SEC-01 | No authentication or authorization | Security |
| CD-04 | Zero automated tests | Testing |

### High

| ID | Finding | Category |
|---|---|---|
| SEC-02 | No HTTPS/TLS support | Security |

### Medium

| ID | Finding | Category |
|---|---|---|
| SEC-03 | No CSRF protection | Security |
| SEC-04 | Missing security headers | Security |
| CD-07 | No dependency update automation | DevOps |

### Low

| ID | Finding | Category |
|---|---|---|
| SEC-05 | WebSocket allows all origins | Security |
| SEC-07 | Upload directory permissions too open | Security |
| CD-05 | golangci-lint version not pinned | CI/CD |
| CD-08 | No environment variable config override | Configuration |
| CD-09 | No release artifact checksums | DevOps |
| CQ-05 | Duplicated file extension lists | Code Quality |

### Informational (Strengths)

| ID | Finding | Category |
|---|---|---|
| SEC-06 | Excellent injection prevention (SQL, command, XSS, path traversal) | Security |
| CQ-01 | Clean modular architecture | Code Quality |
| CQ-02 | Consistent idiomatic naming | Code Quality |
| CQ-04 | Strong type safety | Code Quality |
| CQ-06 | No dead code | Code Quality |
| CQ-08 | Correct concurrency patterns | Code Quality |
| CD-02 | Excellent release automation | CI/CD |
| CD-03 | Comprehensive linting configuration | CI/CD |

---

## Recommendations

### Priority 1: Add Automated Tests

This is the highest-impact improvement. Suggested starting points:

1. **`scheduler.IsWithinHours()`** -- Table-driven tests covering edge cases (midnight crossover, disabled days, empty schedules)
2. **`display.parse()`** -- Fixture-based tests with sample xrandr output
3. **`detectMediaType()` / `isImageFile()`** -- Simple assertion tests for all supported and unsupported extensions
4. **`db` package** -- Integration tests using an in-memory SQLite database
5. **HTTP handlers** -- Tests using `net/http/httptest` for critical paths (upload, rollback, screen CRUD)

Add `go test ./...` to the PR workflow once tests exist.

### Priority 2: Add Authentication

Implement authentication middleware on the chi router. Options in order of increasing complexity:

1. **HTTP Basic Auth** -- Simplest; credentials in config.toml
2. **Token-based auth** -- Bearer token in config, validated via middleware
3. **OAuth2/OIDC** -- Integration with external identity provider

### Priority 3: Security Hardening

1. Add CSRF token middleware (e.g., `gorilla/csrf`)
2. Add security headers middleware (`X-Frame-Options`, `X-Content-Type-Options`, `Content-Security-Policy`)
3. Validate WebSocket `Origin` header against expected host
4. Document recommended deployment behind a TLS-terminating reverse proxy

### Priority 4: CI/CD Improvements

1. Pin golangci-lint to a specific version
2. Restore Renovate or add Dependabot for automated dependency updates
3. Add `go test ./...` step to PR workflow (once tests exist)
4. Add SHA256 checksum generation to release workflow

### Priority 5: Minor Code Quality

1. Consolidate duplicated file extension lists into a shared package
2. Add package-level GoDoc comments to all `internal/` packages
3. Consider structured logging (`log/slog`) for production deployments
4. Add environment variable overrides to the config loader
