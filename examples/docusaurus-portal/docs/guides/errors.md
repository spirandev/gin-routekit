---
title: Errors
sidebar_position: 2
---

# Errors

All errors share a stable JSON envelope:

```json
{
  "code": "invalid_request",
  "message": "Size must be one of small, medium, large"
}
```

| Status | Code | When |
|---|---|---|
| 400 | `invalid_request` | Body failed binding or validation |
| 401 | `unauthorized` | Missing or expired credentials |
| 404 | `not_found` | Resource does not exist |
| 500 | `internal_error` | Unexpected failure |

In gin-routekit, declare this envelope once as a default response (`WithDefaultResponse(500, ...)` or `WithJSONDefaults`) and it shows up on every documented operation.
