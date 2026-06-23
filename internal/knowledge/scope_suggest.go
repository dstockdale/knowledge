package knowledge

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ScopeSuggestionOptions struct {
	CodeRoot string
	Limit    int
}

type ScopeSuggestionReport struct {
	Documents          int                 `json:"documents"`
	Scoped             int                 `json:"scoped"`
	Unscoped           int                 `json:"unscoped"`
	InvalidScopedPaths []InvalidScopedPath `json:"invalid_scoped_paths"`
	Suggestions        []ScopeSuggestion   `json:"suggestions"`
}

type InvalidScopedPath struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Scope string `json:"scope"`
}

type ScopeSuggestion struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Status         string   `json:"status"`
	Title          string   `json:"title"`
	Path           string   `json:"path"`
	ScopeDomains   []string `json:"scope_domains"`
	SuggestedPaths []string `json:"suggested_paths"`
	Reasons        []string `json:"reasons"`
}

func SuggestScopes(root string, opts ScopeSuggestionOptions) (ScopeSuggestionReport, error) {
	docs, _, err := LoadBestEffort(root)
	if err != nil {
		return ScopeSuggestionReport{}, err
	}
	report := ScopeSuggestionReport{
		Documents:          len(docs),
		InvalidScopedPaths: []InvalidScopedPath{},
		Suggestions:        []ScopeSuggestion{},
	}
	for _, doc := range docs {
		if len(doc.Scope.Paths) > 0 {
			report.Scoped++
			if opts.CodeRoot != "" {
				for _, scopedPath := range doc.Scope.Paths {
					if !scopePathExists(opts.CodeRoot, scopedPath) {
						report.InvalidScopedPaths = append(report.InvalidScopedPaths, InvalidScopedPath{
							ID:    doc.ID,
							Path:  doc.Path,
							Scope: scopedPath,
						})
					}
				}
			}
			continue
		}
		report.Unscoped++
		suggestion := suggestScopeForDocument(doc)
		if len(suggestion.SuggestedPaths) == 0 {
			continue
		}
		report.Suggestions = append(report.Suggestions, suggestion)
	}
	sort.SliceStable(report.InvalidScopedPaths, func(i, j int) bool {
		if report.InvalidScopedPaths[i].Path == report.InvalidScopedPaths[j].Path {
			return report.InvalidScopedPaths[i].Scope < report.InvalidScopedPaths[j].Scope
		}
		return report.InvalidScopedPaths[i].Path < report.InvalidScopedPaths[j].Path
	})
	sort.SliceStable(report.Suggestions, func(i, j int) bool {
		iPriority := scopeSuggestionPriority(report.Suggestions[i])
		jPriority := scopeSuggestionPriority(report.Suggestions[j])
		if iPriority == jPriority {
			return report.Suggestions[i].ID < report.Suggestions[j].ID
		}
		return iPriority > jPriority
	})
	if opts.Limit > 0 && len(report.Suggestions) > opts.Limit {
		report.Suggestions = report.Suggestions[:opts.Limit]
	}
	return report, nil
}

func suggestScopeForDocument(doc Document) ScopeSuggestion {
	paths := []string{}
	reasons := []string{}
	identity := scopeSuggestionIdentity(doc)
	add := func(reason string, suggested ...string) {
		paths = append(paths, suggested...)
		reasons = append(reasons, reason)
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "frontend", "react", "tailwind", "css", "heex", "component", "layout", "ui") {
		add("frontend signal", "assets/js/**", "assets/css/**", "lib/boopbup_web/**", "priv/static/**")
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "web", "routing", "route", "router", "app-shell") {
		add("web routing signal", "lib/boopbup_web/**", "assets/js/user_app/**", "assets/js/pages/**", "lib/boopbup_web/router.ex")
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "admin", "operator", "workspace", "workbench") {
		add("admin signal", "assets/js/pages/admin/**", "assets/js/components/**", "lib/boopbup_web/channels/**")
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "analytics", "event", "events", "telemetry", "funnel", "retention") {
		add("analytics signal", "lib/boopbup/analytics/**", "priv/repo/migrations/**", "test/boopbup/analytics/**")
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "clickhouse") {
		add("clickhouse signal", "lib/boopbup/analytics/click_house/**", "priv/clickhouse/**")
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "media", "upload", "asset", "rendition", "storage") {
		add("media signal", "lib/boopbup/media/**", "lib/boopbup/listings/**", "priv/fixtures/listings/**", "priv/static/images/**", "test/boopbup/media/**")
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "directory", "listing", "listings", "event", "events", "graphql", "relay") {
		add("directory/listings signal", "lib/boopbup/listings/**", "priv/graphql/**", "test/boopbup/listings/**", "test/boopbup_web/**")
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "identity", "auth", "signup", "sign-in", "account", "accounts") {
		add("identity/auth signal", "lib/boopbup/accounts/**", "lib/boopbup/identity/**", "lib/boopbup_web/controllers/user_auth_html/**", "lib/boopbup_web/router.ex")
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "discovery", "feed", "locality") {
		add("discovery signal", "lib/boopbup/discovery/**", "test/boopbup/discovery/**")
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "ingestion", "google-sheets", "sheets") {
		add("ingestion signal", "lib/boopbup/ingestion/**", "test/boopbup/ingestion/**")
	}
	if containsScopeSignal(identity, doc.Scope.Domains, "chat", "conversation", "realtime", "channel") {
		add("chat/realtime signal", "lib/boopbup/chat/**", "lib/boopbup_web/channels/**")
	}
	return ScopeSuggestion{
		ID:             doc.ID,
		Kind:           doc.Kind,
		Status:         doc.Status,
		Title:          doc.Title,
		Path:           doc.Path,
		ScopeDomains:   doc.Scope.Domains,
		SuggestedPaths: normalizeList(paths),
		Reasons:        normalizeList(reasons),
	}
}

func scopeSuggestionIdentity(doc Document) string {
	return strings.ToLower(strings.ReplaceAll(doc.ID+" "+doc.Kind+" "+doc.Title+" "+doc.Path, "_", "-"))
}

func containsScopeSignal(identity string, domains []string, terms ...string) bool {
	for _, domain := range domains {
		domain = strings.ToLower(strings.ReplaceAll(domain, "_", "-"))
		for _, term := range terms {
			if domain == term {
				return true
			}
		}
	}
	for _, term := range terms {
		if strings.Contains(identity, term) {
			return true
		}
	}
	return false
}

func scopeSuggestionPriority(suggestion ScopeSuggestion) int {
	score := len(suggestion.SuggestedPaths)
	switch suggestion.Kind {
	case "principle", "spec":
		score += 40
	case "adr", "runbook":
		score += 30
	case "plan":
		score += 20
	case "spike", "idea":
		score += 10
	}
	switch strings.ToLower(suggestion.Status) {
	case "current", "accepted", "active":
		score += 20
	case "proposed", "draft", "exploring":
		score += 10
	}
	return score
}

func scopePathExists(root, scopedPath string) bool {
	scopedPath = filepath.ToSlash(strings.TrimSpace(scopedPath))
	if scopedPath == "" {
		return false
	}
	if strings.Contains(scopedPath, "**") {
		prefix := strings.Split(scopedPath, "**")[0]
		prefix = strings.TrimSuffix(prefix, "/")
		if prefix == "" {
			return true
		}
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(prefix)))
		return err == nil
	}
	return pathExists(root, scopedPath)
}
