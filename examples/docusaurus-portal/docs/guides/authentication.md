---
title: Authentication
sidebar_position: 1
---

# Authentication

The demo service does not enforce authentication, but the API contract is designed around bearer credentials.

```http
Authorization: Bearer <token>
```

Tokens are scoped per tenant. Expired tokens answer `401 Unauthorized` with the standard error envelope described in [Errors](./errors).

When integrating with gin-routekit, declare security schemes in `OpenAPIConfig.Components.SecuritySchemes` and attach them with documented middleware (`RequireSecurityScheme`) so the contract stays the single source of truth.
