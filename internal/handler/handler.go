// Package handler wires HTTP routes to application logic.
package handler

import (
	"database/sql"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/JeffResc/DisplayLoop/internal/player"
	"github.com/JeffResc/DisplayLoop/internal/scheduler"
)

// App holds shared dependencies for all handlers.
type App struct {
	DB            *sql.DB
	Players       *player.Manager
	Scheduler     *scheduler.Scheduler
	UploadsDir    string
	TemplateFS    fs.FS
	TemplateFuncs template.FuncMap
	Hub           *SSEHub
}

// render parses layout.html + the named page template together and executes
// the "layout" block. Parsing per-request keeps {{define}} blocks isolated
// between pages, avoiding the "last parsed wins" conflict in Go templates.
func (a *App) render(w http.ResponseWriter, page string, data any) {
	tmpl, err := template.New("").Funcs(a.TemplateFuncs).ParseFS(a.TemplateFS, "layout.html", page)
	if err != nil {
		http.Error(w, "template parse: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, "template exec: "+err.Error(), http.StatusInternalServerError)
	}
}

// renderPartial parses a single template file and executes the named block.
// Used for HTMX partials that don't wrap the full layout.
func (a *App) renderPartial(w http.ResponseWriter, file, block string, data any) {
	tmpl, err := template.New("").Funcs(a.TemplateFuncs).ParseFS(a.TemplateFS, file)
	if err != nil {
		http.Error(w, "template parse: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, block, data); err != nil {
		http.Error(w, "template exec: "+err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) respondError(w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

func (a *App) redirect(w http.ResponseWriter, r *http.Request, url string) {
	http.Redirect(w, r, url, http.StatusSeeOther)
}
