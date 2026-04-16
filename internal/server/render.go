package server

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/pfortini/debeasy/web"
)

// Renderer parses each top-level template into its own tree (cloned from a base that
// holds layout.html + partials/*). This avoids the {{define "content"}} collision that
// occurs when all templates share a single tree.
type Renderer struct {
	pages map[string]*template.Template
	funcs template.FuncMap
}

func NewRenderer() (*Renderer, error) {
	funcs := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"default": func(def, v any) any {
			if v == nil || v == "" {
				return def
			}
			return v
		},
		"truncate": func(n int, s string) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "…"
		},
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
	}

	// 1. Build a base template containing layout + every partial.
	base := template.New("base").Funcs(funcs)
	var pageFiles []string
	err := fs.WalkDir(web.Templates, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		b, err := web.Templates.ReadFile(path)
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(path, "templates/")
		// partials and layout.html go into the base; pages are parsed per-template later
		if name == "layout.html" || strings.HasPrefix(name, "partials/") {
			if _, err := base.New(name).Parse(string(b)); err != nil {
				return fmt.Errorf("parse %s: %w", name, err)
			}
		} else {
			pageFiles = append(pageFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 2. For each page, clone the base and parse the page on top.
	pages := map[string]*template.Template{}
	for _, path := range pageFiles {
		name := strings.TrimPrefix(path, "templates/")
		clone, err := base.Clone()
		if err != nil {
			return nil, err
		}
		b, err := web.Templates.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if _, err := clone.New(name).Parse(string(b)); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		pages[name] = clone
	}
	return &Renderer{pages: pages, funcs: funcs}, nil
}

func (r *Renderer) Render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	tpl, ok := r.pages[name]
	if !ok {
		// not a top-level page — try as a partial against any tree (use the first available)
		for _, t := range r.pages {
			tpl = t
			break
		}
		if tpl == nil {
			http.Error(w, "no templates loaded", 500)
			return
		}
	}
	if err := tpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "render error: "+err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func IsHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }
