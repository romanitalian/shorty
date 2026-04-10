package main

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
)

// Embed all HTML templates at compile time so the web UI ships inside the
// Lambda binary and works identically in local dev and in production.
//
//go:embed web/templates/*.html
var webFS embed.FS

// webTemplates holds one *template.Template per page. Each is a clone of the
// shared "layout" with the page-specific "content" block parsed on top. That
// gives us a single source of truth for the header/footer chrome without
// runtime template composition.
var webTemplates map[string]*template.Template

// pageData is the data contract every page template renders against.
type pageData struct {
	Title       string
	Description string
	Year        int
	// login page:
	SSOEnabled  bool
	SSOLoginURL string
	// features page:
	Features []featureItem
}

type featureItem struct {
	Icon  string
	Title string
	Body  string
}

func init() {
	webTemplates = map[string]*template.Template{}

	layout := template.Must(template.ParseFS(webFS, "web/templates/layout.html"))

	pages := []string{
		"home",
		"login",
		"dashboard",
		"about",
		"features",
		"pricing",
		"privacy",
		"terms",
		"contact",
	}
	for _, name := range pages {
		t := template.Must(layout.Clone())
		t = template.Must(t.ParseFS(webFS, "web/templates/"+name+".html"))
		webTemplates[name] = t
	}
}

// registerWebHandlers wires the marketing site onto the chi router. It must
// be called BEFORE gen.HandlerFromMux adds the wildcard GET /{code} route so
// chi's trie prefers these static segments over the redirect handler.
func registerWebHandlers(r chi.Router) {
	// Landing page.
	r.Get("/", handleHome)

	// Secondary pages.
	r.Get("/about", renderPage("about", "About", "About the Shorty project."))
	r.Get("/features", handleFeatures)
	r.Get("/pricing", renderPage("pricing", "Pricing", "Simple, predictable pricing."))
	r.Get("/privacy", renderPage("privacy", "Privacy Policy", "How Shorty handles your data."))
	r.Get("/terms", renderPage("terms", "Terms of Service", "Terms governing use of Shorty."))
	r.Get("/contact", renderPage("contact", "Contact", "Ways to reach the Shorty team."))

	// Auth surfaces.
	r.Get("/login", handleLogin)
	r.Post("/login", handleLoginPost)
	r.Get("/dashboard", renderPage("dashboard", "Dashboard", "Your personal Shorty cabinet."))
}

// handleHome renders the landing page.
func handleHome(w http.ResponseWriter, _ *http.Request) {
	renderTemplate(w, "home", pageData{
		Title:       "Short links, serious speed",
		Description: "Open-source URL shortener with click analytics, TTLs, and password protection.",
		Year:        time.Now().Year(),
	})
}

// handleFeatures injects the feature list into the template data.
func handleFeatures(w http.ResponseWriter, _ *http.Request) {
	renderTemplate(w, "features", pageData{
		Title:       "Features",
		Description: "Everything Shorty ships with out of the box.",
		Year:        time.Now().Year(),
		Features: []featureItem{
			{"⚡️", "Blazing fast redirects", "Redis-first lookup, CloudFront at the edge. p99 latency below 100 ms end-to-end."},
			{"📊", "Full click analytics", "Geography, referrers, devices, time-series — all processed async via SQS FIFO so the redirect critical path stays untouched."},
			{"⏱", "Time and click limits", "Expire by absolute timestamp, by click count, or both. Enforced atomically using DynamoDB conditional writes."},
			{"🔒", "Password-protected links", "Gate any short link behind a password. CSRF-hardened form, no secrets ever in URLs."},
			{"🛡️", "SSRF-safe validator", "Blocks javascript:, data:, file: schemes plus the full private-IP CIDR space — resolved via DNS where needed."},
			{"🌐", "IDN homograph detection", "Mixed-script domains (Latin + Cyrillic, etc.) are flagged and rejected at write time."},
			{"🧩", "Open API contract", "OpenAPI 3.0 spec drives the Go stubs, BDD tests, and Redoc viewer."},
			{"☁️", "Serverless, no ops", "Runs on Lambda ARM64, DynamoDB on-demand, ElastiCache Redis. Terraform manages every resource."},
		},
	})
}

// handleLogin renders the login page, surfacing SSO when COGNITO_LOGIN_URL is set.
func handleLogin(w http.ResponseWriter, _ *http.Request) {
	ssoURL := os.Getenv("COGNITO_LOGIN_URL")
	renderTemplate(w, "login", pageData{
		Title:       "Log in",
		Description: "Sign in to manage your Shorty links.",
		Year:        time.Now().Year(),
		SSOEnabled:  ssoURL != "",
		SSOLoginURL: ssoURL,
	})
}

// handleLoginPost is a placeholder — the real session/cookie flow will land
// in a separate PR. For now we just bounce the user back to /dashboard so
// the form has somewhere to go in local dev.
func handleLoginPost(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// renderPage builds an http.HandlerFunc for a static page that only needs
// {Title, Description, Year}.
func renderPage(name, title, description string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		renderTemplate(w, name, pageData{
			Title:       title,
			Description: description,
			Year:        time.Now().Year(),
		})
	}
}

// renderTemplate executes the named template against the shared layout.
func renderTemplate(w http.ResponseWriter, name string, data pageData) {
	t, ok := webTemplates[name]
	if !ok {
		http.Error(w, fmt.Sprintf("template %q not found", name), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The landing page can be cached briefly by the browser; logged-in pages
	// stay no-store so stale auth state never leaks.
	if name == "home" || name == "about" || name == "features" || name == "pricing" ||
		name == "privacy" || name == "terms" || name == "contact" {
		w.Header().Set("Cache-Control", "public, max-age=60")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		// Response headers already committed — log via stderr and give up.
		fmt.Fprintf(os.Stderr, "web: render %q: %v\n", name, err)
	}
}
