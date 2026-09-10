# ADR 0002: Docusaurus docs portal

Date: 2026-09-10

## Status

Superseded by [ADR 0003](0003-runtime-documentation-ui.md). The static portal and its Node build pipeline were removed; the runtime-first contract principles below remain in force.

## Context

Swagger UI and Stoplight Elements solve API visualization, but navigation stays limited to a flat tag list. The default tag of an operation is its route group (`openapi_resolve.go`), and the document emits a flat, sorted tag list (`document.Tags = sortedTags(...)`, `openapi_builder.go`). There is no way to express a `Resource → Version → Status → Endpoint` hierarchy or to give long-lived documentation pages (getting started, guides, changelog, error catalog) the same home as the API reference.

The OpenAPI document is built from the route snapshot and served at runtime by `RegisterOpenAPI`. This runtime-first characteristic is a core property of routekit and must be preserved: the contract always reflects the registered routes of the running binary.

## Decision

- OpenAPI remains the single source of truth. No alternative description format is introduced and no documentation content duplicates generated endpoint metadata.
- Option B (runtime) is the default: a static, self-hosted Docusaurus portal plus a client-side OpenAPI renderer (Stoplight Elements) that consumes `/openapi.json` at runtime. Generation is not duplicated; Node is required only in dev-time to build the static assets.
- Option A (build-time via `docusaurus-plugin-openapi-docs`) remains an optional CI pipeline that consumes the same exported `openapi.json`. It is an accelerator for search and static pages, never a second generator of truth.
- `deprecated` stays in the contract (`deprecated: true`). Any visual "Deprecated/Current" separation is derived from the contract by the frontend, not encoded as separate route trees or docs.
- The `Resource → Version → Status → Endpoint` hierarchy is a view (portal sidebar, opt-in extensions such as `x-tagGroups`), never a compression of operations into tags. Existing `tags` semantics do not change.
- The new `RegisterDocsPortal` follows the existing `Register<UI>(engine, <UI>Config)` pattern. `RegisterSwaggerUI` and `RegisterStoplightUI` remain unchanged, including their default paths.

## Consequences

Docusaurus search indexes only MDX content present in the build, not endpoints rendered at runtime. The optional build-time pipeline can mitigate this, but runtime-only operations stay out of static search until then.

The Docusaurus site `baseUrl` must match the portal `Path` in Go. A mismatch produces broken asset URLs; the example documents this pitfall.

Coexisting `/docs` (portal) with the UIs requires an explicit `Path` on the UI configs: Swagger UI and Stoplight Elements keep `/docs` as their default for backward compatibility. The portal detects route overlap and returns a friendly error instead of letting Gin panic.
