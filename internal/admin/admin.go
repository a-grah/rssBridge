package admin

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"rssbridge/internal/feed"
	"rssbridge/internal/store"
)

// Scheduler is an interface to avoid a circular import.
type Scheduler interface {
	TriggerFetch(siteID int64)
}

// Handler holds dependencies for the admin HTTP handlers.
type Handler struct {
	st          *store.Store
	sc          Scheduler
	templateDir string
	funcMap     template.FuncMap
	version     string
}

// New creates an admin Handler.
func New(st *store.Store, sc Scheduler, templateDir string, version string) (*Handler, error) {
	funcMap := template.FuncMap{
		"formatTime": func(t *time.Time) string {
			if t == nil {
				return "never"
			}
			return t.Format("2006-01-02 15:04:05")
		},
		"formatTimePlain": func(t time.Time) string {
			return t.Format("2006-01-02 15:04:05")
		},
		"humanDuration": func(t *time.Time) string {
			if t == nil {
				return "never"
			}
			d := time.Since(*t)
			if d < 0 {
				return "in " + (-d).Round(time.Minute).String()
			}
			return d.Round(time.Minute).String() + " ago"
		},
		"sub": func(a, b int) int { return a - b },
	}
	// Verify templates exist
	pattern := filepath.Join(templateDir, "*.html")
	if _, err := template.New("").Funcs(funcMap).ParseGlob(pattern); err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Handler{st: st, sc: sc, templateDir: templateDir, funcMap: funcMap, version: version}, nil
}

// RegisterRoutes registers all admin and RSS routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/rss", h.handleRSS)
	mux.HandleFunc("/admin", h.handleDashboard)
	mux.HandleFunc("/admin/sites", h.handleSites)
	mux.HandleFunc("/admin/sites/", h.handleSiteActions)
	mux.HandleFunc("/admin/settings", h.handleSettings)
	mux.HandleFunc("/admin/log", h.handleLog)
	// Redirect root to admin
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})
}

// --- RSS ---

func (h *Handler) handleRSS(w http.ResponseWriter, r *http.Request) {
	data, err := feed.Build(h.st)
	if err != nil {
		http.Error(w, "feed error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write(data)
}

// --- Dashboard ---

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" && r.URL.Path != "/admin/" {
		http.NotFound(w, r)
		return
	}

	stats, err := h.st.GetDashboardStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sites, err := h.st.ListSites()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	recentLog, err := h.st.ListFetchLog(10)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.render(w, "dashboard.html", map[string]any{
		"Stats":     stats,
		"Sites":     sites,
		"RecentLog": recentLog,
		"Now":       time.Now(),
	})
}

// --- Sites ---

func (h *Handler) handleSites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sites, err := h.st.ListSites()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defaultInterval := "12"
		if v, err := h.st.GetSetting("default_interval_hours"); err == nil {
			defaultInterval = v
		}
		h.render(w, "sites.html", map[string]any{
			"Sites":           sites,
			"DefaultInterval": defaultInterval,
		})
	case http.MethodPost:
		h.handleCreateSite(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	siteURL := strings.TrimSpace(r.FormValue("url"))
	intervalStr := r.FormValue("fetch_interval_hours")
	keywords := strings.TrimSpace(r.FormValue("keywords_exclude"))

	if name == "" || siteURL == "" {
		http.Error(w, "name and url are required", http.StatusBadRequest)
		return
	}

	interval := 12.0
	if intervalStr != "" {
		if v, err := strconv.ParseFloat(intervalStr, 64); err == nil && v > 0 {
			interval = v
		}
	}

	st := &store.Site{
		Name:               name,
		URL:                siteURL,
		Enabled:            true,
		FetchIntervalHours: interval,
		KeywordsExclude:    keywords,
	}
	id, err := h.st.CreateSite(st)
	if err != nil {
		http.Error(w, "create site: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Trigger immediate fetch
	h.sc.TriggerFetch(id)

	http.Redirect(w, r, "/admin/sites", http.StatusSeeOther)
}

// handleSiteActions routes /admin/sites/{id}/edit|delete|fetch|{id}
func (h *Handler) handleSiteActions(w http.ResponseWriter, r *http.Request) {
	// Pattern: /admin/sites/{id}[/action]
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/sites/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Redirect(w, r, "/admin/sites", http.StatusFound)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid site id", http.StatusBadRequest)
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch action {
	case "edit":
		h.handleEditSiteForm(w, r, id)
	case "delete":
		h.handleDeleteSite(w, r, id)
	case "fetch":
		h.handleFetchNow(w, r, id)
	default:
		h.handleUpdateSite(w, r, id)
	}
}

func (h *Handler) handleEditSiteForm(w http.ResponseWriter, r *http.Request, id int64) {
	site, err := h.st.GetSite(id)
	if err != nil {
		http.Error(w, "site not found", http.StatusNotFound)
		return
	}
	h.render(w, "site_form.html", map[string]any{"Site": site})
}

func (h *Handler) handleUpdateSite(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	site, err := h.st.GetSite(id)
	if err != nil {
		http.Error(w, "site not found", http.StatusNotFound)
		return
	}

	site.Name = strings.TrimSpace(r.FormValue("name"))
	site.URL = strings.TrimSpace(r.FormValue("url"))
	site.KeywordsExclude = strings.TrimSpace(r.FormValue("keywords_exclude"))
	site.Enabled = r.FormValue("enabled") == "1"

	if v, err := strconv.ParseFloat(r.FormValue("fetch_interval_hours"), 64); err == nil && v > 0 {
		site.FetchIntervalHours = v
	}

	if err := h.st.UpdateSite(site); err != nil {
		http.Error(w, "update site: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/sites", http.StatusSeeOther)
}

func (h *Handler) handleDeleteSite(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.st.DeleteSite(id); err != nil {
		http.Error(w, "delete site: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/sites", http.StatusSeeOther)
}

func (h *Handler) handleFetchNow(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	site, err := h.st.GetSite(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "site not found"})
		return
	}
	_ = site

	h.sc.TriggerFetch(id)

	// Wait a beat so the caller sees something useful
	time.Sleep(200 * time.Millisecond)
	writeJSON(w, http.StatusOK, map[string]string{"status": "fetch triggered"})
}

// --- Settings ---

func (h *Handler) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := h.st.GetAllSettings()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.render(w, "settings.html", map[string]any{"Settings": settings, "Version": h.version})
	case http.MethodPost:
		h.handleSaveSettings(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	keys := []string{"default_interval_hours", "prune_after_days", "rss_max_items", "rss_title"}
	for _, k := range keys {
		if v := r.FormValue(k); v != "" {
			if err := h.st.SetSetting(k, v); err != nil {
				log.Printf("admin: save setting %s: %v", k, err)
			}
		}
	}
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

// --- Log ---

func (h *Handler) handleLog(w http.ResponseWriter, r *http.Request) {
	logs, err := h.st.ListFetchLog(200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, "log.html", map[string]any{"Logs": logs})
}

// --- Helpers ---

// render parses base.html + the page template fresh each call (avoids block
// conflicts when multiple pages define the same block name), then executes
// the "base" template so the layout wraps the page content.
func (h *Handler) render(w http.ResponseWriter, pageName string, data map[string]any) {
	// Inject Page for nav highlighting (derived from filename without .html)
	page := strings.TrimSuffix(pageName, ".html")
	if data == nil {
		data = map[string]any{}
	}
	data["Page"] = page

	tmpl, err := template.New("").Funcs(h.funcMap).ParseFiles(
		filepath.Join(h.templateDir, "base.html"),
		filepath.Join(h.templateDir, pageName),
	)
	if err != nil {
		log.Printf("admin: parse template %s: %v", pageName, err)
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("admin: execute template %s: %v", pageName, err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
