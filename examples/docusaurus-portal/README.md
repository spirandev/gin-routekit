# Docusaurus docs portal example

End-to-end proof of the runtime-first docs portal (ADR 0002, Option B): one Go binary serving a Docusaurus portal at `/docs`, an API Reference page rendering [Stoplight Elements](https://github.com/stoplightio/elements) from the **runtime** `/openapi.json`, coexisting with `/swagger` and `/stoplight` on the same engine.

## Quickstart

Requirements: Go 1.25+ and Node 20+.

```bash
make run
```

This builds the Docusaurus site into `go/portal-build/` and starts the demo. Then verify:

| Endpoint | Check |
|---|---|
| `http://localhost:8080/docs` | Portal renders (dark mode by default), guides and changelog navigate |
| `http://localhost:8080/docs/endpoints` | Endpoint map derived from `x-routekit-docs.sections` + `operation.deprecated` |
| `http://localhost:8080/docs/api-reference` | Elements loads `/openapi.json` served by the Go process |
| `curl -s localhost:8080/openapi.json \| jq '.paths \| keys'` | Lists `/api/v1/instances` and `/api/v2/instances` |
| `curl -s localhost:8080/openapi.json \| jq '.paths."/api/v1/instances".get.deprecated'` | `true` — v1 group is deprecated via `WithDeprecatedEndpoints()` |
| `http://localhost:8080/stoplight` | Stoplight Elements (explicit path) |
| `http://localhost:8080/swagger` | Swagger UI (explicit path) |

To run the Go demo without Node (placeholder portal page):

```bash
make start
```

## How it fits together

```text
Go binary (single process, port 8080)
├── /openapi.json   ← built from the route snapshot at runtime (source of truth)
├── /docs           ← RegisterDocsPortal: static build + SPA fallback
│     └── /docs/api-reference ← client-side Elements consuming /openapi.json
├── /swagger        ← RegisterSwaggerUI (Path: "/swagger")
└── /stoplight      ← RegisterStoplightUI (Path: "/stoplight")
```

`go/embed.go` embeds `go/portal-build/` with `//go:embed all:portal-build`. `docusaurus.config.js` sets `outDir: 'go/portal-build'`, so `npm run build` regenerates the embedded directory in place.

## Pitfalls

### 1. `baseUrl` must match the Go `Path`

Docusaurus is configured with `baseUrl: '/docs/'` (trailing slash included). `RegisterDocsPortal` uses `Path: "/docs"` (no trailing slash). These **must** refer to the same location: if you change the Go `Path`, change Docusaurus `baseUrl` accordingly — otherwise the browser requests assets under the wrong prefix and the portal renders unstyled or 404s.

### 2. The committed placeholder build

`go/portal-build/index.html` is a committed placeholder so that `go build`, `go vet` and `go test` work **without Node**. It only prints instructions. `make build-portal` (i.e. `npm run build`) replaces its contents with the real site; the `.gitignore` keeps only the placeholder versioned.

## Search scope

The Docusaurus offline search (`@easyops-cn/docusaurus-search-local`) indexes **MDX content of this build only** — the runtime-rendered API Reference endpoints are not indexed. See ADR 0002 (`docs/decisions/0002-docusaurus-docs-portal.md` in the repository) and the optional build-time pipeline (phase 6) for the mitigation.

## OpenAPI URL note

`DocsPortalConfig` intentionally has no `OpenAPIURL` field: the OpenAPI renderer lives in the portal's own page (`src/components/ApiReference`), which points at `/openapi.json`. The Go library only knows "static assets + SPA fallback"; it has no Docusaurus-specific behavior.
