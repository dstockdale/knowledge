package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const defaultTokenBudget = 6000

type SearchOptions struct {
	Query             string
	Kinds             []string
	Statuses          []string
	Scopes            []string
	Paths             []string
	IncludeHistorical bool
	Limit             int
}

type SearchResult struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Status          string   `json:"status"`
	Title           string   `json:"title"`
	Path            string   `json:"path"`
	Score           float64  `json:"score"`
	EstimatedTokens int      `json:"estimated_tokens"`
	Reasons         []string `json:"reasons"`
}

type SearchResponse struct {
	Results    []SearchResult    `json:"results"`
	Validation ValidationSummary `json:"validation"`
}

type ContextRequest struct {
	Task              string   `json:"task"`
	Paths             []string `json:"paths"`
	Symbols           []string `json:"symbols"`
	TokenBudget       int      `json:"token_budget"`
	IncludeHistorical bool     `json:"include_historical"`
}

type ContextManifest struct {
	TokenBudget         int               `json:"token_budget"`
	EstimatedTokensUsed int               `json:"estimated_tokens_used"`
	Documents           []ContextDocument `json:"documents"`
	HistoricalDocuments []ContextDocument `json:"historical_documents"`
	Validation          ValidationSummary `json:"validation"`
}

type ContextDocument struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Status          string   `json:"status"`
	Title           string   `json:"title"`
	Path            string   `json:"path"`
	Section         string   `json:"section"`
	Reason          string   `json:"reason"`
	EstimatedTokens int      `json:"estimated_tokens"`
	Score           float64  `json:"score"`
	Reasons         []string `json:"reasons"`
}

type ReadResult struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	Title           string `json:"title"`
	Path            string `json:"path"`
	Section         string `json:"section"`
	Content         string `json:"content"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

type GraphResult struct {
	ID        string      `json:"id"`
	Depth     int         `json:"depth"`
	Neighbors []GraphEdge `json:"neighbors"`
	Documents []GraphDoc  `json:"documents"`
}

type GraphEdge struct {
	Source   string `json:"source"`
	Relation string `json:"relation"`
	Target   string `json:"target"`
}

type GraphDoc struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Path   string `json:"path"`
}

type AffectedDocument struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Status          string   `json:"status"`
	Title           string   `json:"title"`
	Path            string   `json:"path"`
	MatchedPaths    []string `json:"matched_paths"`
	EstimatedTokens int      `json:"estimated_tokens"`
}

type StatusResult struct {
	Root              string            `json:"root"`
	DBPath            string            `json:"db_path"`
	Documents         int               `json:"documents"`
	IssueCount        int               `json:"issue_count"`
	WarningCount      int               `json:"warning_count"`
	Issues            []ValidationIssue `json:"issues"`
	Warnings          []ValidationIssue `json:"warnings"`
	IssuesTruncated   bool              `json:"issues_truncated"`
	WarningsTruncated bool              `json:"warnings_truncated"`
	DBExists          bool              `json:"db_exists"`
	IndexStale        bool              `json:"index_stale"`
	IndexUsable       bool              `json:"index_usable"`
	IndexedAt         string            `json:"indexed_at,omitempty"`
}

type contextCandidate struct {
	doc     Document
	score   float64
	reasons []string
}

func DefaultDBPath(root string) string {
	return filepath.Join(root, ".knowledge", "index.sqlite")
}

func ResolveDBPath(root, explicit string) string {
	if explicit != "" {
		return explicit
	}
	return DefaultDBPath(root)
}

func EnsureIndex(ctx context.Context, root, dbPath string) error {
	needs, err := indexNeedsRebuild(root, dbPath)
	if err != nil {
		return err
	}
	if !needs {
		return nil
	}
	_, err = RebuildIndex(ctx, root, dbPath)
	return err
}

func RebuildIndex(ctx context.Context, root, dbPath string) ([]Document, error) {
	docs, issues, err := loadValidatedDocuments(root)
	if err != nil {
		return nil, err
	}
	indexableDocs := indexableDocuments(docs, issues)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := openIndexDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := createSchema(ctx, db); err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := clearIndex(ctx, tx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	for _, doc := range indexableDocs {
		if err := insertDocument(ctx, tx, doc); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	fileCount, maxMTime, err := corpusState(root)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := putMetadata(ctx, tx, "file_count", strconv.Itoa(fileCount)); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := putMetadata(ctx, tx, "max_mtime_unix_nano", strconv.FormatInt(maxMTime, 10)); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := putMetadata(ctx, tx, "indexed_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return indexableDocs, nil
}

func Search(ctx context.Context, root, dbPath string, opts SearchOptions) ([]SearchResult, error) {
	response, err := SearchWithValidation(ctx, root, dbPath, opts)
	if err != nil {
		return nil, err
	}
	return response.Results, nil
}

func SearchWithValidation(ctx context.Context, root, dbPath string, opts SearchOptions) (SearchResponse, error) {
	if err := EnsureIndex(ctx, root, dbPath); err != nil {
		return SearchResponse{}, err
	}
	docs, issues, err := loadValidatedDocuments(root)
	if err != nil {
		return SearchResponse{}, err
	}
	indexableDocs := indexableDocuments(docs, issues)
	fts, err := ftsScores(ctx, dbPath, opts.Query)
	if err != nil {
		return SearchResponse{}, err
	}
	terms := queryTerms(opts.Query)
	var results []SearchResult
	for _, doc := range indexableDocs {
		if !opts.IncludeHistorical && IsHistorical(doc.Status) && !exactIdentifierOrTitleMatch(doc, opts.Query) {
			continue
		}
		if !matchesAny(doc.Kind, opts.Kinds) || !matchesAny(doc.Status, opts.Statuses) {
			continue
		}
		if len(opts.Scopes) > 0 && !matchesScopeFilter(doc, opts.Scopes) {
			continue
		}
		if len(opts.Paths) > 0 && !matchesAnyRequestedPath(doc, opts.Paths) {
			continue
		}
		score, reasons := lexicalScore(doc, terms, opts.Query, fts[doc.ID], opts.IncludeHistorical)
		if strings.TrimSpace(opts.Query) != "" && len(reasons) == 0 {
			continue
		}
		results = append(results, SearchResult{
			ID:              doc.ID,
			Kind:            doc.Kind,
			Status:          doc.Status,
			Title:           doc.Title,
			Path:            doc.Path,
			Score:           score,
			EstimatedTokens: EstimateTokens(doc.Body),
			Reasons:         reasons,
		})
	}
	sortSearchResults(results)
	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return SearchResponse{
		Results:    results,
		Validation: SummarizeValidation(len(docs), issues, 20, 20),
	}, nil
}

func ContextForTask(ctx context.Context, root, dbPath string, req ContextRequest) (ContextManifest, error) {
	if err := EnsureIndex(ctx, root, dbPath); err != nil {
		return ContextManifest{}, err
	}
	docs, issues, err := loadValidatedDocuments(root)
	if err != nil {
		return ContextManifest{}, err
	}
	indexableDocs := indexableDocuments(docs, issues)
	budget := req.TokenBudget
	if budget <= 0 {
		budget = defaultTokenBudget
	}
	fts, err := ftsScores(ctx, dbPath, req.Task)
	if err != nil {
		return ContextManifest{}, err
	}
	terms := queryTerms(req.Task)
	hints := contextHintsFor(req.Task, req.Paths, req.Symbols)
	candidates := make([]contextCandidate, 0, len(indexableDocs))
	byID := map[string]Document{}
	for _, doc := range indexableDocs {
		byID[doc.ID] = doc
		score, reasons := contextScore(doc, terms, req.Task, req.Paths, req.Symbols, hints, fts[doc.ID], req.IncludeHistorical)
		if len(reasons) == 0 {
			continue
		}
		candidates = append(candidates, contextCandidate{doc: doc, score: score, reasons: reasons})
	}
	candidates = expandOneHop(candidates, byID)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].doc.ID < candidates[j].doc.ID
		}
		return candidates[i].score > candidates[j].score
	})
	manifest := ContextManifest{TokenBudget: budget, Validation: SummarizeValidation(len(docs), issues, 20, 20)}
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.doc.ID] {
			continue
		}
		seen[c.doc.ID] = true
		section := bestSection(c.doc, terms)
		item := ContextDocument{
			ID:              c.doc.ID,
			Kind:            c.doc.Kind,
			Status:          c.doc.Status,
			Title:           c.doc.Title,
			Path:            c.doc.Path,
			Section:         section.Heading,
			Reason:          strings.Join(c.reasons, "; "),
			EstimatedTokens: section.EstimatedTokens,
			Score:           c.score,
			Reasons:         c.reasons,
		}
		if IsHistorical(c.doc.Status) && !req.IncludeHistorical {
			if shouldListHistorical(c.doc, c.reasons) {
				manifest.HistoricalDocuments = append(manifest.HistoricalDocuments, item)
			}
			continue
		}
		if manifest.EstimatedTokensUsed+item.EstimatedTokens > budget && len(manifest.Documents) > 0 {
			continue
		}
		manifest.Documents = append(manifest.Documents, item)
		manifest.EstimatedTokensUsed += item.EstimatedTokens
	}
	return manifest, nil
}

func shouldListHistorical(doc Document, reasons []string) bool {
	status := strings.ToLower(doc.Status)
	if status == "superseded" || status == "completed" || status == "done" {
		return true
	}
	for _, reason := range reasons {
		if strings.HasPrefix(reason, "one-hop relation") {
			return true
		}
	}
	return false
}

func ReadDocument(root, id, heading string, maxTokens int) (ReadResult, error) {
	docs, _, err := LoadBestEffort(root)
	if err != nil {
		return ReadResult{}, err
	}
	doc, ok := FindByID(docs, id)
	if !ok {
		return ReadResult{}, fmt.Errorf("document %q not found", id)
	}
	section, ok := SectionByHeading(doc, heading)
	if !ok {
		return ReadResult{}, fmt.Errorf("section %q not found in %s", heading, id)
	}
	content := section.Content
	tokens := section.EstimatedTokens
	if maxTokens > 0 && tokens > maxTokens {
		content = truncateByTokens(content, maxTokens)
		tokens = EstimateTokens(content)
	}
	return ReadResult{
		ID:              doc.ID,
		Kind:            doc.Kind,
		Status:          doc.Status,
		Title:           doc.Title,
		Path:            doc.Path,
		Section:         section.Heading,
		Content:         content,
		EstimatedTokens: tokens,
	}, nil
}

func Neighbors(root, id string, relations []string, depth int) (GraphResult, error) {
	docs, _, err := LoadBestEffort(root)
	if err != nil {
		return GraphResult{}, err
	}
	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	byID := map[string]Document{}
	for _, doc := range docs {
		byID[doc.ID] = doc
	}
	if _, ok := byID[id]; !ok {
		return GraphResult{}, fmt.Errorf("document %q not found", id)
	}
	relationFilter := map[string]bool{}
	for _, relation := range relations {
		relationFilter[relation] = true
	}
	seenDocs := map[string]bool{id: true}
	frontier := []string{id}
	var edges []GraphEdge
	for d := 0; d < depth; d++ {
		var next []string
		for _, current := range frontier {
			doc := byID[current]
			for relation, targets := range doc.Relations {
				if len(relationFilter) > 0 && !relationFilter[relation] {
					continue
				}
				for _, target := range targets {
					targetID, _ := splitTarget(target)
					if _, ok := byID[targetID]; !ok {
						continue
					}
					edges = append(edges, GraphEdge{Source: current, Relation: relation, Target: targetID})
					if !seenDocs[targetID] {
						seenDocs[targetID] = true
						next = append(next, targetID)
					}
				}
			}
		}
		frontier = next
	}
	var graphDocs []GraphDoc
	for docID := range seenDocs {
		doc := byID[docID]
		graphDocs = append(graphDocs, GraphDoc{ID: doc.ID, Kind: doc.Kind, Status: doc.Status, Title: doc.Title, Path: doc.Path})
	}
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].Source == edges[j].Source {
			if edges[i].Relation == edges[j].Relation {
				return edges[i].Target < edges[j].Target
			}
			return edges[i].Relation < edges[j].Relation
		}
		return edges[i].Source < edges[j].Source
	})
	sort.SliceStable(graphDocs, func(i, j int) bool { return graphDocs[i].ID < graphDocs[j].ID })
	return GraphResult{ID: id, Depth: depth, Neighbors: edges, Documents: graphDocs}, nil
}

func AffectedDocuments(root string, paths []string) ([]AffectedDocument, error) {
	docs, _, err := LoadBestEffort(root)
	if err != nil {
		return nil, err
	}
	affected := []AffectedDocument{}
	for _, doc := range docs {
		var matched []string
		for _, changed := range paths {
			if matchesPath(doc, changed) {
				matched = append(matched, changed)
			}
		}
		if len(matched) == 0 {
			continue
		}
		sort.Strings(matched)
		affected = append(affected, AffectedDocument{
			ID:              doc.ID,
			Kind:            doc.Kind,
			Status:          doc.Status,
			Title:           doc.Title,
			Path:            doc.Path,
			MatchedPaths:    matched,
			EstimatedTokens: EstimateTokens(doc.Body),
		})
	}
	sort.SliceStable(affected, func(i, j int) bool { return affected[i].ID < affected[j].ID })
	return affected, nil
}

func Status(ctx context.Context, root, dbPath string, issueLimit, warningLimit int) (StatusResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return StatusResult{}, err
	}
	absDBPath, err := filepath.Abs(dbPath)
	if err != nil {
		return StatusResult{}, err
	}
	docs, issues, err := loadValidatedDocuments(root)
	if err != nil {
		return StatusResult{}, err
	}
	summary := SummarizeValidation(len(docs), issues, issueLimit, warningLimit)
	_, statErr := os.Stat(dbPath)
	dbExists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return StatusResult{}, statErr
	}
	indexStale := true
	if dbExists {
		stale, err := indexNeedsRebuild(root, dbPath)
		if err != nil {
			indexStale = true
		} else {
			indexStale = stale
		}
	}
	metadata, err := readIndexMetadata(ctx, dbPath)
	if err != nil {
		return StatusResult{}, err
	}
	return StatusResult{
		Root:              absRoot,
		DBPath:            absDBPath,
		Documents:         summary.Documents,
		IssueCount:        summary.IssueCount,
		WarningCount:      summary.WarningCount,
		Issues:            summary.Issues,
		Warnings:          summary.Warnings,
		IssuesTruncated:   summary.IssuesTruncated,
		WarningsTruncated: summary.WarningsTruncated,
		DBExists:          dbExists,
		IndexStale:        indexStale,
		IndexUsable:       len(indexableDocuments(docs, issues)) > 0,
		IndexedAt:         metadata["indexed_at"],
	}, nil
}

func loadValidatedDocuments(root string) ([]Document, []ValidationIssue, error) {
	docs, loadIssues, err := LoadBestEffort(root)
	if err != nil {
		return nil, nil, err
	}
	issues := append([]ValidationIssue{}, loadIssues...)
	issues = append(issues, ValidateWithOptions(docs, ValidationOptions{})...)
	return docs, issues, nil
}

func indexableDocuments(docs []Document, issues []ValidationIssue) []Document {
	blockedIDs := map[string]bool{}
	blockedPaths := map[string]bool{}
	for _, issue := range issues {
		if !isBlockingIndexIssue(issue) {
			continue
		}
		if issue.ID != "" {
			blockedIDs[issue.ID] = true
		}
		if issue.Path != "" {
			blockedPaths[issue.Path] = true
		}
	}
	out := make([]Document, 0, len(docs))
	for _, doc := range docs {
		if doc.ID == "" || doc.Kind == "" || doc.Status == "" || doc.Title == "" {
			continue
		}
		if blockedIDs[doc.ID] || blockedPaths[doc.Path] {
			continue
		}
		out = append(out, doc)
	}
	return out
}

func isBlockingIndexIssue(issue ValidationIssue) bool {
	if issue.Severity != "error" {
		return false
	}
	switch issue.Code {
	case "parse_error", "duplicate_id", "missing_id", "missing_kind", "missing_status", "missing_title", "unsupported_kind":
		return true
	default:
		return false
	}
}

func createSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS documents (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			status TEXT NOT NULL,
			title TEXT NOT NULL,
			path TEXT NOT NULL,
			body TEXT NOT NULL,
			created TEXT,
			review_after TEXT,
			scope_json TEXT NOT NULL,
			symbols_json TEXT NOT NULL,
			relations_json TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sections (
			document_id TEXT NOT NULL,
			heading TEXT NOT NULL,
			anchor TEXT NOT NULL,
			level INTEGER NOT NULL,
			content TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			estimated_tokens INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS relations (
			source_id TEXT NOT NULL,
			relation TEXT NOT NULL,
			target_id TEXT NOT NULL,
			target_heading TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS scopes (
			document_id TEXT NOT NULL,
			scope_type TEXT NOT NULL,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS index_metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
			id UNINDEXED,
			title,
			path,
			body,
			headings,
			scope,
			symbols
		)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func openIndexDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func clearIndex(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"documents", "sections", "relations", "scopes", "index_metadata", "documents_fts"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	return nil
}

func insertDocument(ctx context.Context, tx *sql.Tx, doc Document) error {
	scopeJSON, _ := json.Marshal(doc.Scope)
	symbolsJSON, _ := json.Marshal(doc.Symbols)
	relationsJSON, _ := json.Marshal(doc.Relations)
	_, err := tx.ExecContext(ctx, `INSERT INTO documents (
		id, kind, status, title, path, body, created, review_after, scope_json, symbols_json, relations_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.Kind, doc.Status, doc.Title, doc.Path, doc.Body, doc.Created, doc.ReviewAfter,
		string(scopeJSON), string(symbolsJSON), string(relationsJSON),
	)
	if err != nil {
		return err
	}
	for _, section := range doc.Sections {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sections (
			document_id, heading, anchor, level, content, start_line, estimated_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			doc.ID, section.Heading, section.Anchor, section.Level, section.Content, section.StartLine, section.EstimatedTokens,
		); err != nil {
			return err
		}
	}
	for relation, targets := range doc.Relations {
		for _, target := range targets {
			targetID, targetHeading := splitTarget(target)
			if _, err := tx.ExecContext(ctx, `INSERT INTO relations (
				source_id, relation, target_id, target_heading
			) VALUES (?, ?, ?, ?)`, doc.ID, relation, targetID, targetHeading); err != nil {
				return err
			}
		}
	}
	for _, domain := range doc.Scope.Domains {
		if _, err := tx.ExecContext(ctx, `INSERT INTO scopes (document_id, scope_type, value) VALUES (?, ?, ?)`, doc.ID, "domain", domain); err != nil {
			return err
		}
	}
	for _, scopedPath := range doc.Scope.Paths {
		if _, err := tx.ExecContext(ctx, `INSERT INTO scopes (document_id, scope_type, value) VALUES (?, ?, ?)`, doc.ID, "path", scopedPath); err != nil {
			return err
		}
	}
	headings := make([]string, 0, len(doc.Sections))
	for _, section := range doc.Sections {
		headings = append(headings, section.Heading)
	}
	scopeText := strings.Join(append(doc.Scope.Domains, doc.Scope.Paths...), " ")
	if _, err := tx.ExecContext(ctx, `INSERT INTO documents_fts (
		id, title, path, body, headings, scope, symbols
	) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.Title, doc.Path, doc.Body, strings.Join(headings, " "), scopeText, strings.Join(doc.Symbols, " "),
	); err != nil {
		return err
	}
	return nil
}

func putMetadata(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO index_metadata (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func readIndexMetadata(ctx context.Context, dbPath string) (map[string]string, error) {
	metadata := map[string]string{}
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return metadata, nil
	} else if err != nil {
		return nil, err
	}
	db, err := openIndexDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM index_metadata`)
	if err != nil {
		return metadata, nil
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		metadata[key] = value
	}
	return metadata, rows.Err()
}

func indexNeedsRebuild(root, dbPath string) (bool, error) {
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	fileCount, maxMTime, err := corpusState(root)
	if err != nil {
		return false, err
	}
	db, err := openIndexDB(dbPath)
	if err != nil {
		return false, err
	}
	defer db.Close()
	if err := createSchema(context.Background(), db); err != nil {
		return true, nil
	}
	metadata := map[string]string{}
	rows, err := db.Query(`SELECT key, value FROM index_metadata`)
	if err != nil {
		return true, nil
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return true, nil
		}
		metadata[key] = value
	}
	return metadata["file_count"] != strconv.Itoa(fileCount) ||
		metadata["max_mtime_unix_nano"] != strconv.FormatInt(maxMTime, 10), nil
}

func corpusState(root string) (int, int64, error) {
	paths, err := Discover(root)
	if err != nil {
		return 0, 0, err
	}
	var maxMTime int64
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return 0, 0, err
		}
		if mt := info.ModTime().UnixNano(); mt > maxMTime {
			maxMTime = mt
		}
	}
	return len(paths), maxMTime, nil
}

func ftsScores(ctx context.Context, dbPath, query string) (map[string]float64, error) {
	scores := map[string]float64{}
	match := ftsQuery(query)
	if match == "" {
		return scores, nil
	}
	db, err := openIndexDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, bm25(documents_fts) AS rank FROM documents_fts WHERE documents_fts MATCH ? ORDER BY rank LIMIT 100`, match)
	if err != nil {
		return scores, nil
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var rank float64
		if err := rows.Scan(&id, &rank); err != nil {
			return nil, err
		}
		score := 25.0 / (1.0 + rank)
		if score < 0 {
			score = 0
		}
		if score > 40 {
			score = 40
		}
		scores[id] = score
	}
	return scores, rows.Err()
}

func lexicalScore(doc Document, terms []string, query string, ftsScore float64, includeHistorical bool) (float64, []string) {
	var score float64
	var reasons []string
	exactPhrase := false
	normalizedQuery := normalizePhrase(query)
	if normalizedQuery != "" {
		if phraseContains(doc.ID, normalizedQuery) {
			score += 320
			exactPhrase = true
			reasons = append(reasons, "exact stable id phrase match")
		}
		if phraseContains(doc.Title, normalizedQuery) {
			score += 300
			exactPhrase = true
			reasons = append(reasons, "exact title phrase match")
		}
	}
	idTokens := identifierTokens(doc.ID)
	titleTokens := identifierTokens(doc.Title)
	pathTokens := identifierTokens(doc.Path)
	idCoverage := tokenCoverage(idTokens, terms)
	if idCoverage > 0 {
		score += 140 * idCoverage
		reasons = append(reasons, "stable id term coverage")
	}
	titleCoverage := tokenCoverage(titleTokens, terms)
	if titleCoverage > 0 {
		score += 120 * titleCoverage
		reasons = append(reasons, "title term coverage")
	}
	pathCoverage := tokenCoverage(pathTokens, terms)
	if pathCoverage > 0 {
		score += 55 * pathCoverage
		reasons = append(reasons, "path term coverage")
	}
	if ftsScore > 0 {
		score += ftsScore
		reasons = append(reasons, "full-text match")
	}
	score += lifecycleWeightFor(doc.Status, includeHistorical, exactPhrase)
	if len(reasons) == 0 && strings.TrimSpace(strings.Join(terms, " ")) == "" {
		reasons = append(reasons, "filtered listing")
	}
	return score, dedupeStrings(reasons)
}

func contextScore(doc Document, terms []string, query string, paths, symbols []string, hints map[string]bool, ftsScore float64, includeHistorical bool) (float64, []string) {
	score, reasons := lexicalScore(doc, terms, query, ftsScore, includeHistorical)
	for _, path := range paths {
		if matchesPath(doc, path) {
			score += 70
			reasons = append(reasons, "direct code path match")
			break
		}
	}
	for _, symbol := range symbols {
		if containsFold(doc.Symbols, symbol) {
			score += 55
			reasons = append(reasons, "symbol match")
			break
		}
	}
	for _, domain := range doc.Scope.Domains {
		for _, term := range terms {
			if strings.Contains(strings.ToLower(domain), term) || strings.Contains(term, strings.ToLower(domain)) {
				score += 20
				reasons = append(reasons, "domain match")
				break
			}
		}
	}
	if hints["frontend"] && isGoverningDocForHint(doc, "frontend") {
		score += 150
		reasons = append(reasons, "governing frontend match")
	}
	return score, dedupeStrings(reasons)
}

func contextHintsFor(task string, paths, symbols []string) map[string]bool {
	hints := map[string]bool{}
	for _, term := range queryTerms(task) {
		if isFrontendTerm(term) {
			hints["frontend"] = true
		}
	}
	for _, symbol := range symbols {
		for _, term := range queryTerms(symbol) {
			if isFrontendTerm(term) {
				hints["frontend"] = true
			}
		}
	}
	for _, path := range paths {
		if isFrontendPath(path) {
			hints["frontend"] = true
		}
	}
	return hints
}

func isFrontendTerm(term string) bool {
	switch strings.ToLower(strings.TrimSpace(term)) {
	case "frontend", "react", "css", "tailwind", "heex", "component", "components", "layout", "ui":
		return true
	default:
		return false
	}
}

func isFrontendPath(path string) bool {
	path = filepath.ToSlash(strings.ToLower(strings.TrimSpace(path)))
	path = strings.TrimPrefix(path, "./")
	switch {
	case path == "assets/js" || strings.HasPrefix(path, "assets/js/"):
		return true
	case path == "assets/css" || strings.HasPrefix(path, "assets/css/"):
		return true
	case path == "priv/static" || strings.HasPrefix(path, "priv/static/"):
		return true
	case strings.HasPrefix(path, "lib/") && (strings.Contains(path, "_web/") || strings.HasSuffix(path, "_web")):
		return true
	default:
		return false
	}
}

func isGoverningDocForHint(doc Document, hint string) bool {
	if !isCurrentStatus(doc.Status) || !isGoverningCandidate(doc) {
		return false
	}
	switch hint {
	case "frontend":
		return documentIdentityHasAny(doc, []string{"frontend", "react", "css", "tailwind", "heex", "component", "layout", "ui"})
	default:
		return false
	}
}

func isGoverningCandidate(doc Document) bool {
	if doc.Kind != "principle" && !(doc.Kind == "spec" && strings.HasPrefix(filepath.ToSlash(doc.Path), "architecture/")) {
		return false
	}
	return documentIdentityHasAny(doc, []string{"constitution", "principle", "standard", "guideline"})
}

func documentIdentityHasAny(doc Document, terms []string) bool {
	identity := strings.ToLower(doc.ID + " " + doc.Title + " " + filepath.ToSlash(doc.Path) + " " + strings.Join(doc.Scope.Domains, " "))
	for _, term := range terms {
		if strings.Contains(identity, term) {
			return true
		}
	}
	return false
}

func isCurrentStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "current", "accepted", "active":
		return true
	default:
		return false
	}
}

func expandOneHop(candidates []contextCandidate, byID map[string]Document) []contextCandidate {
	out := append([]contextCandidate{}, candidates...)
	seen := map[string]bool{}
	for _, c := range candidates {
		seen[c.doc.ID] = true
	}
	for _, c := range candidates {
		if c.score < 50 {
			continue
		}
		for relation, targets := range c.doc.Relations {
			for _, target := range targets {
				targetID, _ := splitTarget(target)
				if seen[targetID] {
					continue
				}
				doc, ok := byID[targetID]
				if !ok {
					continue
				}
				seen[targetID] = true
				out = append(out, contextCandidate{doc: doc, score: 18 + lifecycleWeightFor(doc.Status, false, false), reasons: []string{"one-hop relation from " + c.doc.ID + " via " + relation}})
			}
		}
	}
	return out
}

func bestSection(doc Document, terms []string) Section {
	if len(doc.Sections) == 0 {
		return Section{Heading: "Document", Content: doc.Body, EstimatedTokens: EstimateTokens(doc.Body)}
	}
	best := doc.Sections[0]
	bestScore := -1
	for _, section := range doc.Sections {
		score := 0
		searchable := strings.ToLower(section.Heading + " " + section.Content)
		for _, term := range terms {
			if strings.Contains(searchable, term) {
				score++
			}
		}
		if score > bestScore {
			best = section
			bestScore = score
		}
	}
	if best.EstimatedTokens < 20 {
		for _, preferred := range []string{"Decision", "Goal", "Context", "Scope", "Constraints"} {
			if section, ok := SectionByHeading(doc, preferred); ok && section.EstimatedTokens >= 20 {
				return section
			}
		}
		for _, section := range doc.Sections {
			if section.EstimatedTokens >= 20 {
				return section
			}
		}
	}
	return best
}

func sortSearchResults(results []SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score > results[j].Score
	})
}

func queryTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	var terms []string
	for _, field := range fields {
		field = strings.Trim(field, `"'.,:;!?()[]{}<>`)
		if len(field) < 2 {
			continue
		}
		terms = append(terms, field)
	}
	return dedupeStrings(terms)
}

func identifierTokens(value string) map[string]bool {
	tokens := map[string]bool{}
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		tokens[b.String()] = true
		b.Reset()
	}
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func tokenCoverage(tokens map[string]bool, terms []string) float64 {
	if len(terms) == 0 {
		return 0
	}
	matches := 0
	for _, term := range terms {
		if tokens[term] {
			matches++
		}
	}
	return float64(matches) / float64(len(terms))
}

func phraseContains(value, normalizedQuery string) bool {
	if normalizedQuery == "" {
		return false
	}
	return strings.Contains(normalizePhrase(value), normalizedQuery)
}

func exactIdentifierOrTitleMatch(doc Document, query string) bool {
	normalizedQuery := normalizePhrase(query)
	return phraseContains(doc.ID, normalizedQuery) || phraseContains(doc.Title, normalizedQuery)
}

func normalizePhrase(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	space := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func ftsQuery(query string) string {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return ""
	}
	for i, term := range terms {
		terms[i] = `"` + strings.ReplaceAll(term, `"`, `""`) + `*"`
	}
	return strings.Join(terms, " OR ")
}

func lifecycleWeightFor(status string, includeHistorical, exactPhrase bool) float64 {
	switch strings.ToLower(status) {
	case "accepted", "current", "active", "open":
		return 14
	case "proposed", "draft", "exploring", "captured":
		return 5
	case "superseded", "rejected", "obsolete", "abandoned", "completed", "done", "deprecated", "resolved":
		if includeHistorical && exactPhrase {
			return 0
		}
		if includeHistorical {
			return -4
		}
		return -20
	default:
		return 0
	}
}

func matchesAny(value string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		if strings.EqualFold(value, item) {
			return true
		}
	}
	return false
}

func matchesScopeFilter(doc Document, scopes []string) bool {
	for _, scope := range scopes {
		if containsFold(doc.Scope.Domains, scope) || containsFold(doc.Scope.Paths, scope) {
			return true
		}
	}
	return false
}

func matchesAnyRequestedPath(doc Document, paths []string) bool {
	for _, path := range paths {
		if matchesPath(doc, path) {
			return true
		}
	}
	return false
}

func matchesPath(doc Document, changedPath string) bool {
	changedPath = filepath.ToSlash(strings.TrimSpace(changedPath))
	for _, scopedPath := range doc.Scope.Paths {
		if matchScopedPath(scopedPath, changedPath) {
			return true
		}
	}
	return false
}

func matchScopedPath(scopePath, changedPath string) bool {
	scopePath = filepath.ToSlash(strings.TrimSpace(scopePath))
	scopePath = strings.TrimPrefix(scopePath, "./")
	changedPath = strings.TrimPrefix(changedPath, "./")
	if scopePath == "" || changedPath == "" {
		return false
	}
	if scopePath == changedPath {
		return true
	}
	if strings.Contains(scopePath, "**") {
		prefix := strings.Split(scopePath, "**")[0]
		prefix = strings.TrimSuffix(prefix, "/")
		return changedPath == prefix || strings.HasPrefix(changedPath, prefix+"/")
	}
	if strings.Contains(scopePath, "*") {
		pattern := strings.ReplaceAll(scopePath, "*", "")
		pattern = strings.Trim(pattern, "/")
		return strings.Contains(changedPath, pattern)
	}
	scopePath = strings.TrimSuffix(scopePath, "/")
	return strings.HasPrefix(changedPath, scopePath+"/") || strings.HasPrefix(scopePath, changedPath+"/")
}

func containsFold(values []string, value string) bool {
	value = strings.TrimSpace(value)
	for _, item := range values {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func truncateByTokens(text string, maxTokens int) string {
	if maxTokens <= 0 {
		return text
	}
	maxChars := maxTokens * 4
	if len(text) <= maxChars {
		return text
	}
	return strings.TrimSpace(text[:maxChars]) + "\n\n[truncated]"
}
