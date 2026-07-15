# ADR 0001: Typed JSON endpoint package

Date: 2026-07-15

## Status

Accepted for the minimal implementation spike. Consumer validation was explicitly waived for this working-tree implementation, so publication remains a release decision.

## Context

The typed endpoint must derive runtime behavior and its `Contract` from the same configuration without changing the existing Gin API. Go does not support generic methods on `RouterGroup`, and adding binding and response concerns to the root package would increase its surface for applications that only use legacy handlers.

## Decision

- Add the optional `jsonendpoint` subpackage. It imports the root package; the root package does not import it.
- Expose one generic `Handle` function instead of method-specific wrappers.
- Return the original `*routekit.RouteConfig` so all existing fluents remain available.
- Register through `RouterGroup.Handle` and attach the derived `Contract` before returning.
- Preserve documentation opt-in behavior. A typed endpoint is not documented automatically.
- Treat later `Contract`, `Body`, or `Response` fluents as explicit escape hatches that void the structural runtime/documentation guarantee.
- Panic at registration for invalid static configuration because the target API has no error return. Messages use a `jsonendpoint:` prefix.
- Initially support concrete struct, pointer-to-struct, slice, and string-keyed map request types whose standard JSON representation matches reflection. Reject interfaces, recursive containers, byte sequences, custom JSON codecs, implicit embedded-field flattening, and `json:",string"` because they do not yet produce an exact reflected contract.
- Reject an absent body when required. An optional absent body produces the request zero value, except that pointer-to-struct requests remain allocated according to the pointer binding rule. JSON `null` is distinct from absence and is rejected because reflected request schemas are not nullable.
- Use Gin `ShouldBindJSON` after buffering and restoring the body. This preserves Gin binding validation without invoking it twice.
- Initially reject pointer response types because nil response semantics are not yet represented by the schema model.
- Use fixed success status and direct JSON responses. Status 204 and 205 never serialize a body.
- Default binding and handler error policies own both runtime writing and their documented responses. Custom policies and `Result` are deferred to the next phase.
- If a handler already wrote a response, never write a second response. Strict mode records a contract violation on the Gin context.

## Consequences

The subpackage remains additive and avoids import cycles. Runtime and documentation share status, schemas, and default error responses. Streaming, multipart, WebSocket, proxy, downloads, and handlers with unconventional response flows remain legacy Gin handlers with manual contracts.

The consumer gate from the future plan is not proven by repository evidence. This implementation must not be considered release-ready solely because its local tests pass.
