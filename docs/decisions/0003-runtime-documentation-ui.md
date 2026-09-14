# ADR 0003: Runtime documentation UIs only

Date: 2026-09-10

## Status

Accepted. Supersedes [ADR 0002](0002-docusaurus-docs-portal.md).

## Context

ADR 0002 planned a self-hosted Docusaurus portal served by the Go binary (`RegisterDocsPortal`). In practice it required a Node build pipeline, a committed placeholder build, `baseUrl`/`Path` coupling and an optional CI pipeline — significant maintenance weight for a documentation feature.

Swagger UI and Stoplight Elements already render the runtime contract from a single HTML template each, with no build step, and the OpenAPI document remains generated from the route snapshot at runtime.

## Decision

- OpenAPI remains the single source of truth, built from the route snapshot and served at runtime by `RegisterOpenAPI`.
- Documentation UIs are lightweight runtime integrations following the existing `Register<UI>(engine, <UI>Config)` pattern: normalize, validate, register one HTML page that renders `/openapi.json` client-side. Swagger UI and Stoplight Elements exist today; comparable simple renderers may follow the same pattern.
- No embedded static portal and no Node tooling in this repository. `RegisterDocsPortal` and the Docusaurus example were removed.
- Contract-level features introduced with the portal work remain because they are renderer-agnostic and opt-in: the `deprecated` flag (endpoint, group defaults, OR semantics) and the `x-tagGroups` / `x-routekit-docs.sections` metadata extensions. Documents without the new configuration are byte-identical to before.
- Long-form guides and docs search are out of scope for this library; host them elsewhere if needed.

## Consequences

Consumers get API documentation with zero build tooling: register the contract, optionally register a UI, done. Navigation depth is limited to tags and the opt-in `x-tagGroups` view (natively consumed by Redoc, should that UI be added). There is no MDX guides home and no portal search; `x-routekit-docs.sections` remains available for custom frontends that need deeper trees.
