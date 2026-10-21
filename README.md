# gin-routekit
gin-routekit is a reusable Go library that encapsulates a fluent route definition pattern for Gin applications. It supports route grouping, per-route metadata, authentication and authorization middleware configuration, route context injection, and a generic AppRouter for registering and syncing routes across projects.

For an end-to-end reference covering routes, middleware, AppRouter, OpenAPI, Swagger UI and typed JSON endpoints, see the [complete usage guide](guia-completo.md).

## OpenAPI Automation

OpenAPI generation targets OpenAPI 3.1.0 and JSON Schema 2020-12. Generation is explicit by default. Existing applications can keep using `EnabledByDefault`, `Document()`, `HideFromDocs()`, `Body()`, `Response()`, `DocProfile()` and route decorators.

New code should prefer `DocumentationMode`:

```go
doc, err := routekit.BuildOpenAPI(routes, routekit.OpenAPIConfig{
	Title:             "Example API",
	Version:           "1.0.0",
	BasePath:          "/api",
	PathMode:          routekit.PathsRelativeToBase,
	DocumentationMode: routekit.DocumentAll,
})
```

Use `DocumentOptIn` to require `Document()` per endpoint. `HideFromDocs()` always wins over global or group defaults.

`BasePath` and `PathMode` are mandatory. `PathsRelativeToBase` removes `BasePath` once from every emitted path and, when `Servers` is empty, emits a relative server with that base path. Every registered route must belong to the base path. `FullRegisteredPaths` preserves complete registered paths; server URLs must not repeat the same application prefix.

Every documented operation must declare at least one response. No implicit `200 OK` response is generated. An operation with responses but no 2xx response produces a warning rather than an error.

### Validate, Build and Marshal

Use `ValidateOpenAPI` in CI to collect all known errors and warnings in one pass:

```go
report, err := routekit.ValidateOpenAPI(routes, config)
for _, diagnostic := range report.Diagnostics {
	log.Printf("%s %s: %s", diagnostic.Severity, diagnostic.Code, diagnostic.Message)
}
if err != nil {
	return err
}

document, err := routekit.BuildOpenAPI(routes, config)
if err != nil {
	return err
}
payload, err := routekit.MarshalOpenAPI(document)
httpPayload, err := routekit.MarshalHTTPClient(document, routekit.HTTPClientConfig{
	BaseURL: "https://api.example.com",
})
```

`ValidateOpenAPI` returns a `DiagnosticReport` even when it also returns a `*DiagnosticsError`; warnings do not block a build. `BuildOpenAPI` returns no document when errors are present. `MarshalOpenAPI` produces the deterministic, indented JSON payload without registering an HTTP endpoint. The same built document can also be serialized with `MarshalHTTPClient` as a `.http` file for the IntelliJ/JetBrains HTTP Client.

An `AppRouter` exposes the same validation and build operations after its route snapshot has been registered:

```go
if err := appRouter.RegisterRoutes(engine); err != nil {
	return err
}
report, err := appRouter.ValidateOpenAPI(config)
document, err := appRouter.BuildOpenAPI(config)
err = appRouter.RegisterOpenAPI(engine, config)
httpPayload, err := appRouter.BuildHTTPClient(config, routekit.HTTPClientConfig{
	BaseURL: "https://api.example.com",
})
```

`AppRouter.ValidateOpenAPI`, `AppRouter.BuildOpenAPI` and `AppRouter.BuildHTTPClient` require `RegisterRoutes`. None of these build methods registers an OpenAPI endpoint or starts a server. `RegisterOpenAPI` builds and marshals through the same pipeline, then registers `JSONPath` (default `/openapi.json`). Package-level `ValidateOpenAPI` and `BuildOpenAPI` remain useful for snapshots and route subsets.

### Documentation endpoints

| Endpoint | Source | Renderer |
|---|---|---|
| `/openapi.json` | `RegisterOpenAPI` (dynamic) | — |
| `/swagger` | `RegisterSwaggerUI(Path: "/swagger")` | Swagger UI |
| `/stoplight` | `RegisterStoplightUI(Path: "/stoplight")` | Stoplight Elements |
| `/scalar` | `RegisterScalarUI(Path: "/scalar")` | Scalar API Reference |

All UIs default to `/docs` for backward compatibility; when registering more than one on the same engine, give each an explicit `Path`. See [ADR 0003](docs/decisions/0003-runtime-documentation-ui.md) for the documentation strategy.

### Marking endpoints as deprecated

Mark a single endpoint as deprecated:

```go
group.GET("/v1/users", listUsers, "List users", 20).
	Document().
	Deprecated()
```

Deprecate every endpoint of a group at once:

```go
group := routekit.NewRouterGroup(engine, "/api/v1",
	routekit.WithDocumentation(),
	routekit.WithDeprecatedEndpoints(),
)
```

Resolution follows OR semantics: an operation is deprecated when the global `OpenAPIConfig.Defaults.Deprecated`, the group `DocumentationDefaults.Deprecated` or the endpoint `Deprecated` flag is set. There is no option to opt out of a deprecated group default; route decorators remain the escape hatch for unusual cases. Non-deprecated operations never emit the `deprecated` key in the OpenAPI document.

### Marking schema properties as deprecated

A single field of a request/response struct can be flagged as deprecated with the `routekit:"deprecated"` tag, read by the same reflector that already understands `json` and `binding`:

```go
type Product struct {
	Name     string  `json:"name"`
	OldPrice float64 `json:"old_price" routekit:"deprecated"`
}
```

This emits `"deprecated": true` on that property's schema, in both the request and response representations, without affecting sibling properties or `required`. Fields without the tag never emit the `deprecated` key.

Optional fields are nullable (`"anyOf": [<type>, {"type": "null"}]`); the flag is set on that wrapper *and* on every non-null branch, so tools that resolve a specific `anyOf` branch (e.g. Stoplight Elements) still see the field as deprecated. Struct, registered, and provider-backed fields (`$ref`) mark only the field's usage site — the shared component in `components/schemas` is never modified, so other fields referencing the same type are unaffected.

### Hierarchical docs metadata

Two independent, opt-in extensions represent a `Resource → Version → Status → Endpoint` view without changing `tags` semantics or the default document:

**`x-tagGroups`** (Redoc convention, natively consumed by Redoc) groups existing tags into categories:

```go
config.TagGroups = []routekit.OpenAPITagGroup{
	{Name: "Instances", Tags: []string{"instances-v1", "instances-v2"}, Description: "Instance management"},
}
```

Referencing a tag the document does not emit, empty group names and duplicate group names are validation errors (`tag_groups.*` diagnostics). Operation tags never change; groups are a view over tags.

**`x-routekit-docs.sections`** records a logical section path per operation, keyed by the final `operationId`, for custom frontends to build deeper trees:

```go
group.GET("/v1/instances", listInstances, "List instances", 10).
	Document().
	Section("Instances", "V1").
	Response(200, "OK", nil)
```

```json
"x-routekit-docs": {
	"sections": {
		"get_instances_v1_list_instances": ["Instances", "V1"]
	}
}
```

The "Deprecated vs Current" dimension is deliberately **not** recorded in the extension: derive it from `operation.deprecated` so the contract stays the single source of truth. Without `TagGroups` or `Section` the document is byte-identical to before (guarded by a golden test).

### Group Defaults

Groups can provide shared documentation defaults without mutating endpoint `DocConfig`:

```go
group := routekit.NewRouterGroup(engine, "/api",
	routekit.WithDocumentation(),
	routekit.WithDocProfiles("tenant"),
	routekit.WithDefaultHeader(routekit.DocParam{Name: "X-Tenant", Type: "string", Required: true}),
	routekit.WithDefaultResponse(401, "Unauthorized", nil),
	routekit.WithDefaultContentTypes("application/json", "application/json"),
)
```

Endpoint exceptions are explicit:

```go
group.GET("/health", health, "Health", 1).
	WithoutDocProfile("tenant").
	WithoutDefaultResponse(401).
	WithoutDefaultParameter(routekit.DocParamInHeader, "X-Tenant").
	Response(200, "OK", nil)
```

### Contracts

Contracts describe legacy `gin.HandlerFunc` request/response shapes without changing runtime execution:

```go
routekit.DescribeJSON[LoginRequest, LoginResponse](
	group.POST("/login", login, "Login", 10),
	200,
	"OK",
)
```

The constructor equivalents cover request/response, response-only and no-body operations without placeholder `struct{}` types:

```go
group.POST("/login", login, "Login", 10).
	Contract(routekit.JSONRequestContractOf[LoginRequest, LoginResponse](200, "OK"))

group.GET("/health", health, "Health", 11).
	Contract(routekit.JSONResponseContractOf[HealthResponse](200, "OK"))

group.DELETE("/sessions/:id", deleteSession, "Delete session", 12).
	Contract(routekit.EmptyJSONResponseContract(
		204,
		"No Content",
		routekit.WithContractParameter(routekit.DocParam{
			Name: "id", In: routekit.DocParamInPath,
			Type: "string", Required: true,
		}),
	))
```

`JSONContractOf` remains as a deprecated alias of `JSONRequestContractOf`. `ResponseOf[T]` creates a JSON `DocResponse` for contracts or middleware contributions. `DefaultResponseOf[T]` and `WithJSONDefaults` create typed global defaults:

```go
config.Defaults = routekit.WithJSONDefaults(
	routekit.DefaultResponseOf[ErrorResponse](500, "Internal Server Error"),
)
```

Equivalent fluent style remains supported:

```go
group.POST("/login", login, "Login", 10).
	Document().
	Body(routekit.SchemaOf[LoginRequest]()).
	Response(200, "OK", routekit.SchemaOf[LoginResponse]())
```

Fluent endpoint declarations override contract values by response status and parameter key.

Useful contract options include `WithOptionalRequestBody`, `WithoutRequestBody`, `WithoutResponseBody`, `WithRequestContentType`, `WithResponseContentType`, `WithRequestExample`, `WithResponseExample`, `WithRequestExamples`, `WithResponseExamples`, `WithAdditionalResponse`, `WithContractProfiles` and `WithContractParameter`.

Named examples serialize as the OpenAPI `examples` map, so Swagger UI offers a scenario dropdown in "Try it out"; combining `WithRequestExample` with `WithRequestExamples` (or the response pair) records a `contract.option.incoherent` diagnostic:

```go
routekit.JSONRequestContractOf[NotifyRequest, NotifyResponse](200, "OK",
	routekit.WithRequestExamples(
		routekit.NamedExample{Name: "whatsapp", Summary: "WhatsApp", Value: NotifyRequest{Channel: "whatsapp"}},
		routekit.NamedExample{Name: "email", Summary: "E-mail", Value: NotifyRequest{Channel: "email"}},
	),
)
```

### Typed JSON Endpoints

The experimental `jsonendpoint` subpackage binds a typed JSON request, runs a typed handler, writes the JSON response and derives the route `Contract` from the same configuration:

See the [complete jsonendpoint usage guide](docs/jsonendpoint.md) for binding rules, validation, OpenAPI integration, testing and supported DTO shapes.

```go
route := jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/login",
	func(c *gin.Context, request LoginRequest) (LoginResponse, error) {
		return login(c, request)
	},
	"Login",
	10,
	jsonendpoint.WithSuccess(http.StatusCreated, "Created"),
)

route.Public().Tags("authentication")
```

The returned value is the original `*routekit.RouteConfig`, so authentication, middleware, scopes and documentation fluents remain available. Typed endpoints preserve the existing documentation mode; use `Document()`, `WithDocumentation` or `DocumentAll` as usual.

The adapter provides:

- required JSON bodies by default, with `WithOptionalBody` for an absent body;
- Gin binding validation plus `WithValidator` for application validation;
- a 1 MiB default body limit, configurable with `WithBodyLimit`;
- fixed success responses through `WithSuccess`;
- documented and runtime-consistent JSON errors for binding (400) and handler failures (500);
- `WithStrictWriter` to record direct handler writes as contract violations without writing a second response.

Request decoding and response encoding follow `encoding/json`. A required body must be present, but the JSON value may be `null`; pointers, maps and slices retain the corresponding Go nil semantics. Nil pointer and container responses serialize as JSON `null`. Status 204 and 205 never serialize a body.

Schema generation understands promoted embedded fields, aliases, recursive containers, `json:",string"`, `omitempty`, `omitzero`, `[]byte`, `json.Number`, `json.RawMessage`, supported map keys and directional text codecs. Arbitrary `MarshalJSON` or `UnmarshalJSON` behavior cannot be inferred safely and requires an explicit schema, provider or registration. See the [complete jsonendpoint usage guide](docs/jsonendpoint.md) for runtime details.

Streaming, SSE, WebSocket, proxy/pass-through, downloads, multipart and handlers that intentionally control multiple responses continue to use `gin.HandlerFunc` with a manual `Contract`.

### Schema Descriptors

`SchemaOf[T]` documents a type without constructing a value. It works with pointers, slices, maps and generic envelopes:

```go
group.GET("/users", listUsers, "List users", 20).
	Document().
	Response(200, "OK", routekit.SchemaOf[Page[UserDTO]]())
```

`SchemaWithExample[T](example)` attaches a descriptor-level example. A `DocBody.Example` or `DocResponse.Example` value takes precedence.

### Directional Schemas

Request and response schemas are analyzed separately because `encoding/json` can accept and emit different shapes. Request requiredness follows `binding:"required"`; response requiredness follows field promotion and omission rules. Nullability, `MarshalText`/`UnmarshalText`, `MarshalJSON`/`UnmarshalJSON` and map-key support are also evaluated in the relevant direction. Equivalent request and response schemas may share a component; different schemas receive separate components.

An explicit `OpenAPISchema` in `DocBody.Schema` or `DocResponse.Schema` bypasses reflection. Reusable type registrations can name reflected components or override a type:

```go
config.SchemaRegistrations = []routekit.SchemaRegistration{
	routekit.RegisterSchemaAs[CreateUserRequest](
		"CreateUserInput",
		routekit.ForSchemaRequest(),
	),
	routekit.OverrideSchemaOf[ExternalDate](
		routekit.OpenAPISchema{Type: "string", Format: "date"},
		routekit.ForSchemaDirections(routekit.SchemaRequest, routekit.SchemaResponse),
	),
}
```

Omit a direction option to apply a registration to both directions. `ForSchemaRequest`, `ForSchemaResponse` and `ForSchemaDirections` constrain it. `Components.Schemas` remains available for independent manual components.

Types can also implement `OpenAPISchemaProvider` or `DirectionalOpenAPISchemaProvider`:

```go
func (ExternalDate) OpenAPISchemaFor(direction routekit.SchemaDirection) routekit.OpenAPISchema {
	return routekit.OpenAPISchema{Type: "string", Format: "date"}
}
```

Providers may use pointer receivers; routekit invokes them on a new zero-value instance. Invalid schemas, provider panics, duplicate registrations and component-name collisions are diagnostics rather than silent fallbacks.

### Documented Middleware

`UseDocumented` registers the same Gin middleware in the runtime chain and accepts composable documentation contributions for routes that include it:

```go
group.UseDocumented(
	authMiddleware,
	routekit.RequireSecurityScheme("BearerAuth"),
	routekit.RespondsWith(routekit.ResponseOf[ErrorResponse](401, "Unauthorized")),
)

group.GET("/reports", reports, "Reports", 30).
	UseDocumented(
		rateLimitMiddleware,
		routekit.RequiresHeader("X-Request-ID", "string", true, "Request correlation ID"),
		routekit.RespondsWith(routekit.ResponseOf[ErrorResponse](429, "Too Many Requests")),
	)
```

`RequireOperationHeader` (`RequiresHeader` is its concise alias), `RequireSecurityScheme`, `RequiresSecurity`, `RespondsWith` and `UsesProfiles` compose without mutating their inputs. Existing `MiddlewareMetadata` values still implement `DocumentationContribution`. Contributions describe behavior declaratively; routekit does not inspect middleware code.

### Security Requirements

`OpenAPIOperation.AddSecurity("BearerAuth")` keeps the legacy behavior: each call adds an OR alternative.

Use `AddSecurityRequirement` when multiple schemes are required together:

```go
ctx.Operation.AddSecurityRequirement("ClientID", "ClientSecret") // ClientID AND ClientSecret
ctx.Operation.AddSecurity("BearerAuth")                          // OR BearerAuth
```

A security scheme and an operation parameter are distinct OpenAPI concepts. `RequireSecurityScheme("APIKey")` references a scheme from `OpenAPIConfig.Components.SecuritySchemes`; it does not also add the key as a header parameter. Declare a separate operation header only when the API contract truly has both. Declaring the same API-key header in both forms is a validation error.

Route security rules map exported route flags to documentation without inspecting runtime middleware:

```go
config.Components.SecuritySchemes = map[string]*routekit.OpenAPISecurityScheme{
	"BearerAuth": routekit.BearerSecurityScheme("Bearer token"),
}
config.SecurityRules = []routekit.RouteSecurityRule{
	routekit.WhenAuthenticated(routekit.RequireSecurityScheme("BearerAuth")),
	routekit.WhenM2M(routekit.UsesProfiles("m2m")),
	routekit.WhenIntegration(routekit.RequiresHeader(
		"X-Integration-ID", "string", true, "Integration identifier",
	)),
}
```

`WhenAuthenticated`, `WhenM2M` and `WhenIntegration` match the corresponding `Handler` flags. Use `NewRouteSecurityRule` with a `RouteSecurityPredicate` for another policy. Missing schemes, invalid scopes and duplicate API-key headers are errors; declared but unused schemes produce warnings.

### Profiles

Profiles can include a description in addition to parameters and security requirements:

```go
config.Profiles = map[string]routekit.DocProfile{
	"tenant": {
		Description: "Requests scoped to one tenant",
		Headers: []routekit.DocParam{{
			Name: "X-Tenant", In: routekit.DocParamInHeader,
			Type: "string", Required: true,
		}},
	},
}
```

Profiles actually used by documented operations are emitted in the top-level `x-routekit-profiles` extension, including `description` when set. Unknown profiles and incompatible profile parameters are errors; duplicate use is a warning.

See [Migrating to OpenAPI 3.1 and fidelity diagnostics](migration-openapi-3.1.md) for breaking changes and before/after examples.

## Manifest sync

`AppRouter.RegisterRoutes` calls its `RouteSyncer` once all routes are registered. `NewManifestSyncer` is a ready-made `RouteSyncer` that publishes those routes to an external service (route registry, gateway, authorization service) over HTTP. It uses only the standard library and does not read environment variables or validate domain values such as which access types exist; that belongs to the application and the destination.

### Route metadata

Two route fluents feed the manifest:

```go
group.POST("/items", createItem, "Create item", 1001).
	Category("items").        // trimmed; falls back to the Export groupName
	AccessTypes("admin")      // copies the input slice, like Scopes
```

Both default to empty (`""` and `[]string{}`).

### Configuration

```go
syncer, err := routekit.NewManifestSyncer(routekit.ManifestSyncConfig{
	URL:     os.Getenv("ROUTES_SYNC_URL"), // absolute http/https URL, query string kept as is
	Method:  http.MethodPut,               // optional, defaults to PUT
	Headers: http.Header{"Authorization": {"Bearer " + os.Getenv("ROUTES_SYNC_TOKEN")}},
	Version: version,                      // required, forwarded to the format
	OnResult: func(r routekit.ManifestSyncResult) {
		log.Printf("manifest: attempt %d %s %d", r.Attempt, r.Status, r.StatusCode)
	},
})
if err != nil {
	log.Fatal(err)
}
appRouter := routekit.NewAppRouter(routers, routekit.ChainRouteSyncers(syncer, otherSyncer))
```

`NewManifestSyncer` rejects a non-absolute or non-http(s) URL, a method outside `routekit.ValidMethods` (used exactly as given, so `"put"` is rejected) and an empty `Version`. `Headers` is copied at construction time; the format's `Content-Type` always wins over a `Content-Type` header in the config. A nil `Client` becomes an `http.Client` with a 15s timeout. Response bodies are read up to 1 MiB.

`ChainRouteSyncers` runs syncers in order, skips nil entries and stops at the first error.

### Background and blocking modes

The body is always built synchronously first. Build errors (for example `route_id` 0 or a duplicated `route_id`) are returned as `*routekit.ManifestBuildError` from `RegisterRoutes` in both modes, because they are code bugs rather than delivery failures.

- **Background (default):** `RegisterRoutes` returns immediately and a goroutine sends the request. It retries with exponential backoff only on network errors, 5xx and 429; any other 4xx is final. `Retry` defaults to 5 attempts in total (including the first), a 2s initial delay doubling up to 1min; each zero field falls back to its default independently. The goroutine cannot be cancelled: it keeps retrying until it succeeds or runs out of attempts, and calling `RegisterRoutes` again starts another independent goroutine.
- **Blocking (`Blocking: true`):** a single attempt; a non-2xx response or network error is returned from `RegisterRoutes` as `*routekit.ManifestSyncFailure`:

```go
var failure *routekit.ManifestSyncFailure
if errors.As(err, &failure) {
	for _, routeErr := range failure.Result.Errors {
		log.Printf("route %v: %s %s", routeErr.RouteID, routeErr.Field, routeErr.Message)
	}
}
```

`OnResult` is called after every finished attempt in both modes. `ManifestSyncResult.Status` is one of `applied`, `unchanged`, `valid`, `rejected` or `error`; `Err` is set only for network errors and `Body` holds the raw response.

### The `padrao` format

The built-in format (`routekit.PadraoBodyFormat()`, name `routekit.BodyFormatPadrao`) sends `application/json` with exactly these fields, routes sorted by `route_id`:

```json
{"version":"1.4.0","routes":[{"route_id":1001,"method":"POST","path":"/api/v1/items",
 "definition":"Create item","category":"items","is_authentication":true,"is_authorization":true,
 "requires_client_context":false,"access_types":["admin"],"scopes":[],"is_integration":false,
 "m2m":false,"is_basic":false,"restricted":false}]}
```

`path` is the full registered path, booleans are always explicit (unset flags use the route defaults) and `access_types`/`scopes` are never `null`. `IsSameApplicationRequired` is not part of this format.

Expected responses:

- 2xx: `{"success":true,"message":"...","data":{"status":"applied|unchanged|valid",...}}`. `data` is exposed as `Details`; a missing or unknown status is reported as `applied`.
- Non-2xx: `{"success":false,"message":"...","code":"...","errors":[{"route_index":0,"route_id":1001,"field":"access_types","message":"..."}],"details":{}}`. The status is `rejected` (or `error` for 5xx). When `errors` is not that structured list (for example `["CODE"]`), `Errors` is empty.

### Custom formats

Implement `routekit.BodyFormat` and set it as `Format`:

```go
type BodyFormat interface {
	Name() string
	ContentType() string
	Build(input ManifestInput) ([]byte, error)
	ParseResponse(statusCode int, body []byte) ManifestSyncResult
}
```

The syncer fills `Attempt`, `StatusCode` and `Body` on the result returned by `ParseResponse`. Success and retry decisions are based on the HTTP status code, not on the parsed `Status`.
