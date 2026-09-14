# ADR 0001: Typed JSON endpoint package

Date: 2026-07-15

## Status

Accepted. Updated by the OpenAPI 3.1 fidelity work; publication remains a release decision.

## Context

The typed endpoint must derive runtime behavior and its `Contract` from the same configuration without changing the existing Gin API. Go does not support generic methods on `RouterGroup`, and adding binding and response concerns to the root package would increase its surface for applications that only use legacy handlers.

## Decision

- Add the optional `jsonendpoint` subpackage. It imports the root package; the root package does not import it.
- Expose one generic `Handle` function instead of method-specific wrappers.
- Return the original `*routekit.RouteConfig` so all existing fluents remain available.
- Register through `RouterGroup.Handle` and attach the derived `Contract` before returning.
- Preserve documentation opt-in behavior. A typed endpoint is not documented automatically.
- Treat later `Contract`, `Body`, or `Response` fluents as explicit escape hatches that void the structural runtime/documentation guarantee.
- Panic at registration for invalid adapter configuration because the target API has no error return. Messages use a `jsonendpoint:` prefix. Schema errors are instead reported by `ValidateOpenAPI` and `BuildOpenAPI`, where registrations and providers are available.
- Accept the JSON shapes handled by `encoding/json`, including pointers, aliases, embedded fields, recursive containers, byte sequences, `json.Number`, `json.RawMessage`, supported map keys and `json:",string"`. OpenAPI reflection is directional and follows the same field-selection and omission rules.
- Require an explicit schema, schema registration or provider for arbitrary `MarshalJSON`/`UnmarshalJSON` implementations. Executing a codec on a zero value to guess its shape is not allowed.
- Reject an absent body when required. An optional absent body produces the request zero value and still runs application validators. JSON `null` is distinct from absence, counts as a present body and follows `encoding/json`: it can produce nil pointers/containers or leave a non-pointer value at its zero value. For present bodies, Gin binding tags run before application validators.
- Use Gin `ShouldBindJSON` after buffering and restoring the body. This preserves Gin binding validation without invoking it twice.
- Accept pointer and container responses. Nil values serialize as JSON `null`, including nested values, and are represented by the response-direction schema.
- Use fixed success status and direct JSON responses. Status 204 and 205 never serialize a body.
- Default binding and handler error policies own both runtime writing and their documented responses. Custom policies and `Result` are deferred to the next phase.
- If a handler already wrote a response, never write a second response. Strict mode records a contract violation on the Gin context.

## Consequences

The subpackage remains additive and avoids import cycles. Runtime and documentation share status, directional schemas, and default error responses. Required request body now means presence rather than non-nullness, and nil responses no longer produce an adapter error. This is a behavioral change from the spike.

Schema fidelity is checked when OpenAPI is validated or built, not when `Handle` is called. A custom codec can execute successfully at runtime while still requiring an OpenAPI provider or override. Streaming, multipart, WebSocket, proxy, downloads, and handlers with unconventional response flows remain legacy Gin handlers with manual contracts.
