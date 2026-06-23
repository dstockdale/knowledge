# Knowledge

Knowledge is a Git-native Markdown compiler and read-only context server for coding agents.

The source of truth is Markdown in Git. The SQLite database under `.knowledge/` is a disposable index used for fast retrieval and MCP responses.

## Setup

```sh
mise install
```

## Common Commands

```sh
mise exec -- go test ./...
mise exec -- go run ./cmd/knowledge check --root testdata/corpus
mise exec -- go run ./cmd/knowledge check --root testdata/obsidian
mise exec -- go run ./cmd/knowledge check --root testdata/obsidian --warning-limit 20
mise exec -- go run ./cmd/knowledge check --root testdata/obsidian --strict
mise exec -- go run ./cmd/knowledge index --root testdata/corpus
mise exec -- go run ./cmd/knowledge status --root testdata/corpus
mise exec -- go run ./cmd/knowledge scope-suggest --root testdata/obsidian --code-root .
mise exec -- go run ./cmd/knowledge context --root testdata/corpus --task "add passkeys" --path lib/boop/accounts --token-budget 2000
mise exec -- go build -o bin/knowledge ./cmd/knowledge
```

## Document Contract

Documents are Markdown files with YAML frontmatter.

Required fields:

- `id`
- `kind`
- `status`
- `title`

Optional fields:

- `scope.domains`
- `scope.paths`
- `symbols`
- `relations`
- `created`
- `review_after`

Supported kinds are `adr`, `spec`, `plan`, `idea`, `spike`, `research`, `runbook`, `incident`, `principle`, and `glossary`.

Historical statuses such as `superseded`, `rejected`, `obsolete`, `abandoned`, and `completed` are excluded from task context unless explicitly requested.

## Obsidian-Style Notes

The default parser is permissive so existing Obsidian vaults can be indexed without a migration.

Accepted source shapes include:

- `type: adr` or `type: Architecture Decision Record`, normalized to `kind: adr`.
- `type: architecture`, normalized to `kind: spec`.
- `type: spike`, `/spikes/`, `/ideas/**/spike`, or `.spike.md`, normalized to `kind: spike`.
- `area: identity`, added as a scope domain.
- `source: "[[ideas/example]]"`, added as a `source` relation.
- Files without YAML frontmatter, with `id`, `kind`, `status`, and `title` derived from path and headings.
- Unknown source-specific `type` values, normalized to `kind: research` with a warning in permissive mode.

Use strict mode for curated docs that should explicitly follow the knowledge contract:

```sh
knowledge check --root docs --strict
knowledge index --root docs --strict
```

Permissive mode exits successfully when only derived metadata warnings are present. Strict mode turns derived metadata into errors.

Indexing, search, and context retrieval are best-effort in permissive mode. Structural problems such as duplicate IDs or unparseable files are reported and skipped for affected documents, but valid documents remain searchable. MCP `validate`, `search`, `context_for_task`, and `status` return compact validation summaries so agents can see corpus health without receiving hundreds of warnings by default.

## Scope Paths

`scope.paths` connect docs to code paths for `affected_documents` and path-aware retrieval.

Use repo-relative paths. Prefer directory globs for owned areas:

```yaml
scope:
  paths:
    - lib/boopbup/analytics/**
    - test/boopbup/analytics/**
```

Use exact files only for cross-cutting entrypoints such as `mix.exs`, `lib/boopbup_web/router.ex`, or other single-file boundaries.

`scope-suggest` reports existing coverage, invalid scoped paths, and suggested path globs for unscoped docs. It is read-only and does not mutate Markdown.
