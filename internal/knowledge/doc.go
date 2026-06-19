package knowledge

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var headingRE = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)

var supportedKinds = map[string]bool{
	"adr":       true,
	"spec":      true,
	"plan":      true,
	"idea":      true,
	"research":  true,
	"runbook":   true,
	"incident":  true,
	"principle": true,
	"glossary":  true,
}

var historicalStatuses = map[string]bool{
	"superseded": true,
	"rejected":   true,
	"obsolete":   true,
	"abandoned":  true,
	"completed":  true,
	"done":       true,
	"deprecated": true,
}

type Document struct {
	ID          string              `json:"id"`
	Kind        string              `json:"kind"`
	Status      string              `json:"status"`
	Title       string              `json:"title"`
	Scope       Scope               `json:"scope"`
	Symbols     []string            `json:"symbols"`
	Relations   map[string][]string `json:"relations"`
	Created     string              `json:"created,omitempty"`
	ReviewAfter string              `json:"review_after,omitempty"`
	Path        string              `json:"path"`
	Body        string              `json:"-"`
	Sections    []Section           `json:"sections"`
	Derived     []string            `json:"derived,omitempty"`
	Warnings    []ValidationIssue   `json:"warnings,omitempty"`
}

type Scope struct {
	Domains []string `json:"domains" yaml:"domains"`
	Paths   []string `json:"paths" yaml:"paths"`
}

type Section struct {
	Heading         string `json:"heading"`
	Level           int    `json:"level"`
	Anchor          string `json:"anchor"`
	Content         string `json:"content,omitempty"`
	StartLine       int    `json:"start_line"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

type ValidationIssue struct {
	Path     string `json:"path,omitempty"`
	ID       string `json:"id,omitempty"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type ValidationOptions struct {
	CodeRoot string
	Strict   bool
}

type frontmatter struct {
	ID          string              `yaml:"id"`
	Kind        string              `yaml:"kind"`
	Type        string              `yaml:"type"`
	Status      string              `yaml:"status"`
	Title       string              `yaml:"title"`
	Area        string              `yaml:"area"`
	Scope       Scope               `yaml:"scope"`
	Symbols     []string            `yaml:"symbols"`
	Relations   map[string][]string `yaml:"relations"`
	Source      any                 `yaml:"source"`
	Created     string              `yaml:"created"`
	ReviewAfter string              `yaml:"review_after"`
}

func Discover(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == ".knowledge" || name == "bin" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(name), ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func Load(root string) ([]Document, error) {
	paths, err := Discover(root)
	if err != nil {
		return nil, err
	}
	docs := make([]Document, 0, len(paths))
	for _, path := range paths {
		doc, err := ParseFile(root, path)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func ParseFile(root, path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return Document{}, err
	}
	rel = filepath.ToSlash(rel)
	meta, body, hasFrontmatter, err := splitFrontmatter(data)
	if err != nil {
		return Document{Path: rel}, fmt.Errorf("%s: %w", rel, err)
	}
	var fm frontmatter
	if len(meta) > 0 {
		if err := yaml.Unmarshal(meta, &fm); err != nil {
			return Document{Path: rel}, fmt.Errorf("%s: invalid frontmatter: %w", rel, err)
		}
	}
	bodyText := strings.TrimSpace(string(body))
	sections := ExtractSections(bodyText)
	doc := Document{
		ID:          strings.TrimSpace(fm.ID),
		Kind:        normalizeKind(firstNonEmpty(fm.Kind, fm.Type), rel),
		Status:      strings.TrimSpace(fm.Status),
		Title:       strings.TrimSpace(fm.Title),
		Scope:       normalizeScope(fm.Scope),
		Symbols:     normalizeList(fm.Symbols),
		Relations:   normalizeRelations(fm.Relations),
		Created:     strings.TrimSpace(fm.Created),
		ReviewAfter: strings.TrimSpace(fm.ReviewAfter),
		Path:        rel,
		Body:        bodyText,
		Sections:    sections,
	}
	if fm.Area != "" {
		doc.Scope.Domains = normalizeList(append(doc.Scope.Domains, fm.Area))
	}
	if len(doc.Relations) == 0 {
		doc.Relations = map[string][]string{}
	}
	if sourceTargets := sourceRelations(fm.Source); len(sourceTargets) > 0 {
		doc.Relations["source"] = normalizeList(append(doc.Relations["source"], sourceTargets...))
	}
	if !hasFrontmatter {
		doc.Warnings = append(doc.Warnings, warning(doc, "missing_yaml_frontmatter", "missing YAML frontmatter; metadata was derived from path and headings"))
	}
	if doc.ID == "" {
		doc.ID = deriveID(rel)
		doc.Derived = append(doc.Derived, "id")
		doc.Warnings = append(doc.Warnings, warning(doc, "derived_id", "missing id; derived stable id from path"))
	}
	if doc.Kind == "" {
		doc.Kind = inferKind(rel)
		doc.Derived = append(doc.Derived, "kind")
		doc.Warnings = append(doc.Warnings, warning(doc, "derived_kind", "missing kind/type; inferred kind from path"))
	}
	if doc.Status == "" {
		doc.Status = inferStatus(rel)
		doc.Derived = append(doc.Derived, "status")
		doc.Warnings = append(doc.Warnings, warning(doc, "derived_status", "missing status; defaulted from path or current"))
	}
	if doc.Title == "" {
		doc.Title = deriveTitle(rel, sections)
		doc.Derived = append(doc.Derived, "title")
		doc.Warnings = append(doc.Warnings, warning(doc, "derived_title", "missing title; derived title from first heading or filename"))
	}
	doc.Derived = normalizeList(doc.Derived)
	return doc, nil
}

func splitFrontmatter(data []byte) ([]byte, []byte, bool, error) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, data, false, nil
	}
	rest := data[len("---\n"):]
	idx := bytes.Index(rest, []byte("\n---\n"))
	if idx == -1 {
		return nil, nil, true, fmt.Errorf("unterminated YAML frontmatter")
	}
	meta := rest[:idx]
	body := rest[idx+len("\n---\n"):]
	return meta, body, true, nil
}

func normalizeKind(kind, path string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	kind = strings.ReplaceAll(kind, "-", " ")
	kind = strings.ReplaceAll(kind, "_", " ")
	switch kind {
	case "":
		return ""
	case "adr", "architecture decision record", "architectural decision record", "decision record":
		return "adr"
	case "spec", "specification", "architecture":
		return "spec"
	case "plan", "implementation plan":
		return "plan"
	case "idea":
		return "idea"
	case "research", "research note":
		return "research"
	case "runbook", "operation", "operations":
		return "runbook"
	case "incident":
		return "incident"
	case "principle", "constitution":
		return "principle"
	case "glossary", "glossary entry":
		return "glossary"
	default:
		if strings.HasSuffix(path, ".adr.md") {
			return "adr"
		}
		return strings.ReplaceAll(kind, " ", "-")
	}
}

func inferKind(path string) string {
	path = strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.Contains(path, "/adrs/") || strings.HasPrefix(path, "adrs/") || strings.HasSuffix(path, ".adr.md"):
		return "adr"
	case strings.Contains(path, "/plans/") || strings.HasPrefix(path, "plans/") || strings.HasSuffix(path, ".plan.md"):
		return "plan"
	case strings.Contains(path, "/ideas/") || strings.HasPrefix(path, "ideas/") || strings.HasSuffix(path, ".idea.md"):
		return "idea"
	case strings.Contains(path, "/research/") || strings.HasPrefix(path, "research/") || strings.HasSuffix(path, ".research.md"):
		return "research"
	case strings.Contains(path, "/operations/") || strings.HasPrefix(path, "operations/") || strings.HasSuffix(path, ".runbook.md"):
		return "runbook"
	case strings.Contains(path, "/incidents/") || strings.HasPrefix(path, "incidents/") || strings.HasSuffix(path, ".incident.md"):
		return "incident"
	case strings.Contains(path, "/glossary/") || strings.HasPrefix(path, "glossary/"):
		return "glossary"
	case strings.Contains(path, "constitution") || strings.Contains(path, "/principles/") || strings.HasPrefix(path, "principles/"):
		return "principle"
	case strings.Contains(path, "/architecture/") || strings.HasPrefix(path, "architecture/"):
		return "spec"
	default:
		return "research"
	}
}

func inferStatus(path string) string {
	path = strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.Contains(path, "/completed/"):
		return "completed"
	case strings.Contains(path, "/abandoned/"):
		return "abandoned"
	case strings.Contains(path, "/active/"):
		return "active"
	default:
		return "current"
	}
}

func deriveID(path string) string {
	path = strings.TrimSuffix(filepath.ToSlash(path), ".md")
	for _, suffix := range []string{".adr", ".plan", ".idea", ".research", ".runbook", ".incident"} {
		path = strings.TrimSuffix(path, suffix)
	}
	return strings.ReplaceAll(path, "/", ".")
}

func deriveTitle(path string, sections []Section) string {
	if len(sections) > 0 && sections[0].Heading != "" && sections[0].Heading != "Document" {
		return strings.TrimSpace(sections[0].Heading)
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return titleWords(base)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sourceRelations(source any) []string {
	switch value := source.(type) {
	case string:
		return wikiTargets(value)
	case []any:
		var out []string
		for _, item := range value {
			out = append(out, sourceRelations(item)...)
		}
		return normalizeList(out)
	default:
		return nil
	}
}

func wikiTargets(text string) []string {
	re := regexp.MustCompile(`\[\[([^\]|#]+)(?:#[^\]|]+)?(?:\|[^\]]+)?\]\]`)
	var targets []string
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		target := strings.TrimSpace(match[1])
		if target == "" {
			continue
		}
		target = strings.TrimSuffix(target, ".md")
		targets = append(targets, strings.ReplaceAll(target, "/", "."))
	}
	return normalizeList(targets)
}

func titleWords(value string) string {
	words := strings.Fields(value)
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func ExtractSections(body string) []Section {
	scanner := bufio.NewScanner(strings.NewReader(body))
	type heading struct {
		title string
		level int
		line  int
		start int
		end   int
	}
	var lines []string
	var headings []heading
	lineNo := 0
	offset := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		lines = append(lines, line)
		if match := headingRE.FindStringSubmatch(line); match != nil {
			headings = append(headings, heading{
				title: cleanHeading(match[2]),
				level: len(match[1]),
				line:  lineNo,
				start: offset,
			})
		}
		offset += len(line) + 1
	}
	for i := range headings {
		if i+1 < len(headings) {
			headings[i].end = headings[i+1].start
		} else {
			headings[i].end = len(body)
		}
	}
	if len(headings) == 0 {
		content := strings.TrimSpace(body)
		if content == "" {
			return nil
		}
		return []Section{{
			Heading:         "Document",
			Level:           1,
			Anchor:          "document",
			Content:         content,
			StartLine:       1,
			EstimatedTokens: EstimateTokens(content),
		}}
	}
	sections := make([]Section, 0, len(headings))
	seenAnchors := map[string]int{}
	for _, h := range headings {
		content := strings.TrimSpace(body[h.start:h.end])
		anchor := uniqueAnchor(slug(h.title), seenAnchors)
		sections = append(sections, Section{
			Heading:         h.title,
			Level:           h.level,
			Anchor:          anchor,
			Content:         content,
			StartLine:       h.line,
			EstimatedTokens: EstimateTokens(content),
		})
	}
	return sections
}

func Validate(docs []Document, codeRoot string) []ValidationIssue {
	return FilterIssues(ValidateWithOptions(docs, ValidationOptions{CodeRoot: codeRoot}), "error")
}

func ValidateWithOptions(docs []Document, opts ValidationOptions) []ValidationIssue {
	issues := []ValidationIssue{}
	byID := map[string]Document{}
	seenIDs := map[string]string{}
	for _, doc := range docs {
		if opts.Strict {
			issues = append(issues, strictIssuesForDerived(doc)...)
		} else {
			issues = append(issues, doc.Warnings...)
		}
		if doc.ID == "" {
			issues = append(issues, issue(doc, "missing_id", "missing required field id"))
		}
		if doc.Kind == "" {
			issues = append(issues, issue(doc, "missing_kind", "missing required field kind"))
		} else if !supportedKinds[doc.Kind] {
			issues = append(issues, issue(doc, "unsupported_kind", fmt.Sprintf("unsupported kind %q", doc.Kind)))
		}
		if doc.Status == "" {
			issues = append(issues, issue(doc, "missing_status", "missing required field status"))
		}
		if doc.Title == "" {
			issues = append(issues, issue(doc, "missing_title", "missing required field title"))
		}
		if doc.ID != "" {
			if firstPath, ok := seenIDs[doc.ID]; ok {
				issues = append(issues, issue(doc, "duplicate_id", fmt.Sprintf("duplicate id also used by %s", firstPath)))
			} else {
				seenIDs[doc.ID] = doc.Path
				byID[doc.ID] = doc
			}
		}
	}
	for _, doc := range docs {
		for relation, targets := range doc.Relations {
			if strings.TrimSpace(relation) == "" {
				issues = append(issues, issue(doc, "empty_relation", "relation name must not be empty"))
				continue
			}
			for _, target := range targets {
				targetID, heading := splitTarget(target)
				targetDoc, ok := byID[targetID]
				if !ok {
					issues = append(issues, issue(doc, "dangling_relation", fmt.Sprintf("relation %s targets missing document %q", relation, targetID)))
					continue
				}
				if heading != "" && !HasSection(targetDoc, heading) {
					issues = append(issues, issue(doc, "broken_heading_reference", fmt.Sprintf("relation %s targets missing heading %q in %s", relation, heading, targetID)))
				}
			}
		}
		if opts.CodeRoot != "" {
			for _, scopedPath := range doc.Scope.Paths {
				if !pathExists(opts.CodeRoot, scopedPath) {
					issues = append(issues, issue(doc, "missing_scoped_path", fmt.Sprintf("scope path %q does not exist under %s", scopedPath, opts.CodeRoot)))
				}
			}
		}
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path == issues[j].Path {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Path < issues[j].Path
	})
	return issues
}

func FilterIssues(issues []ValidationIssue, severity string) []ValidationIssue {
	filtered := []ValidationIssue{}
	for _, issue := range issues {
		if issue.Severity == severity {
			filtered = append(filtered, issue)
		}
	}
	return filtered
}

func IsHistorical(status string) bool {
	return historicalStatuses[strings.ToLower(strings.TrimSpace(status))]
}

func IsSupportedKind(kind string) bool {
	return supportedKinds[strings.ToLower(strings.TrimSpace(kind))]
}

func HasSection(doc Document, headingOrAnchor string) bool {
	want := strings.ToLower(strings.TrimSpace(headingOrAnchor))
	want = strings.TrimPrefix(want, "#")
	for _, section := range doc.Sections {
		if strings.ToLower(section.Heading) == want || strings.ToLower(section.Anchor) == want || section.Anchor == slug(want) {
			return true
		}
	}
	return false
}

func SectionByHeading(doc Document, headingOrAnchor string) (Section, bool) {
	if strings.TrimSpace(headingOrAnchor) == "" {
		content := strings.TrimSpace(doc.Body)
		return Section{
			Heading:         "Document",
			Level:           0,
			Anchor:          "document",
			Content:         content,
			StartLine:       1,
			EstimatedTokens: EstimateTokens(content),
		}, true
	}
	want := strings.ToLower(strings.TrimSpace(headingOrAnchor))
	want = strings.TrimPrefix(want, "#")
	for _, section := range doc.Sections {
		if strings.ToLower(section.Heading) == want || strings.ToLower(section.Anchor) == want || section.Anchor == slug(want) {
			return section, true
		}
	}
	return Section{}, false
}

func EstimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	return int(math.Ceil(float64(len(text)) / 4.0))
}

func FindByID(docs []Document, id string) (Document, bool) {
	for _, doc := range docs {
		if doc.ID == id {
			return doc, true
		}
	}
	return Document{}, false
}

func issue(doc Document, code, message string) ValidationIssue {
	return ValidationIssue{Path: doc.Path, ID: doc.ID, Severity: "error", Code: code, Message: message}
}

func warning(doc Document, code, message string) ValidationIssue {
	return ValidationIssue{Path: doc.Path, ID: doc.ID, Severity: "warning", Code: code, Message: message}
}

func strictIssuesForDerived(doc Document) []ValidationIssue {
	issues := []ValidationIssue{}
	for _, field := range doc.Derived {
		issues = append(issues, issue(doc, "derived_"+field, fmt.Sprintf("strict mode requires explicit %s", field)))
	}
	for _, warn := range doc.Warnings {
		if warn.Code == "missing_yaml_frontmatter" {
			issues = append(issues, issue(doc, warn.Code, "strict mode requires YAML frontmatter"))
		}
	}
	return issues
}

func normalizeScope(scope Scope) Scope {
	return Scope{
		Domains: normalizeList(scope.Domains),
		Paths:   normalizeList(scope.Paths),
	}
}

func normalizeList(values []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeRelations(relations map[string][]string) map[string][]string {
	if len(relations) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(relations))
	for name, targets := range relations {
		name = strings.TrimSpace(name)
		if name == "" {
			out[name] = targets
			continue
		}
		out[name] = normalizeList(targets)
	}
	return out
}

func cleanHeading(heading string) string {
	heading = strings.TrimSpace(heading)
	heading = strings.Trim(heading, "#")
	return strings.TrimSpace(heading)
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func uniqueAnchor(base string, seen map[string]int) string {
	if base == "" {
		base = "section"
	}
	seen[base]++
	if seen[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, seen[base]-1)
}

func splitTarget(target string) (string, string) {
	target = strings.TrimSpace(target)
	id, heading, ok := strings.Cut(target, "#")
	if !ok {
		return target, ""
	}
	return strings.TrimSpace(id), strings.TrimSpace(heading)
}

func pathExists(root, scopedPath string) bool {
	full := filepath.Join(root, filepath.FromSlash(scopedPath))
	if strings.ContainsAny(scopedPath, "*?[") {
		matches, err := filepath.Glob(full)
		return err == nil && len(matches) > 0
	}
	_, err := os.Stat(full)
	return err == nil
}
