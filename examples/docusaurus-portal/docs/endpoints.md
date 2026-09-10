---
title: Endpoints
sidebar_position: 2
---

import EndpointTree from '@site/src/components/EndpointTree';

# Endpoint map

This index is **derived at runtime** from the OpenAPI document served by the Go binary:

- `x-routekit-docs.sections` provides the logical path (`Instances → V1`, `Instances → V2`), declared per route with `.Section(...)` in Go;
- the **Deprecated / Current** split derives from `operation.deprecated` (the v1 group uses `WithDeprecatedEndpoints()`).

<EndpointTree />

This is the recommended pattern for custom frontends: Elements itself groups by `tags`, while `x-routekit-docs.sections` enables deeper trees (resource → version → status → endpoint) without compressing anything into tags. See [ADR 0002](https://github.com/spirandev/gin-routekit/blob/main/docs/decisions/0002-docusaurus-docs-portal.md).
