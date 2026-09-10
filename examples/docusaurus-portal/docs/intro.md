---
title: Introduction
sidebar_position: 1
slug: /
---

# Instances API

This portal is the documentation home of the **Instances API**, a demo service built with [gin-routekit](https://github.com/spirandev/gin-routekit).

The Go binary serves everything from a single process:

| Endpoint | What it is |
|---|---|
| `/docs` | This Docusaurus portal (static build, embedded in the binary) |
| `/docs/api-reference` | API Reference rendered client-side from the runtime contract |
| `/openapi.json` | The OpenAPI 3.1 document, generated at runtime from registered routes |
| `/swagger` | Swagger UI (explicit path) |
| `/stoplight` | Stoplight Elements (explicit path) |

## Single source of truth

The OpenAPI document is **not** duplicated anywhere. The same `/openapi.json`:

- feeds the API Reference page on this portal (Stoplight Elements, runtime);
- feeds `/swagger` and `/stoplight`;
- can be exported to a build-time pipeline (`docusaurus-plugin-openapi-docs`) when static search over endpoints is required.

## Quick start

From `examples/docusaurus-portal`:

```bash
make run
```

Then open [http://localhost:8080/docs](http://localhost:8080/docs).
