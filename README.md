# gin-routekit
gin-routekit is a reusable Go library that encapsulates a fluent route definition pattern for Gin applications. It supports route grouping, per-route metadata, authentication and authorization middleware configuration, route context injection, and a generic AppRouter for registering and syncing routes across projects.

## OpenAPI Automation

OpenAPI generation is explicit by default. Existing applications can keep using `EnabledByDefault`, `Document()`, `HideFromDocs()`, `Body()`, `Response()`, `DocProfile()` and route decorators.

New code should prefer `DocumentationMode`:

```go
doc, err := routekit.BuildOpenAPI(routes, routekit.OpenAPIConfig{
	Title:             "Example API",
	Version:           "1.0.0",
	DocumentationMode: routekit.DocumentAll,
})
```

Use `DocumentOptIn` to require `Document()` per endpoint. `HideFromDocs()` always wins over global or group defaults.

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
	WithoutDefaultParameter(routekit.DocParamInHeader, "X-Tenant")
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

Equivalent fluent style remains supported:

```go
group.POST("/login", login, "Login", 10).
	Document().
	Body(routekit.SchemaOf[LoginRequest]()).
	Response(200, "OK", routekit.SchemaOf[LoginResponse]())
```

Fluent endpoint declarations override contract values by response status and parameter key.

Useful contract options include `WithOptionalRequestBody`, `WithoutRequestBody`, `WithoutResponseBody`, `WithRequestContentType`, `WithResponseContentType`, `WithRequestExample`, `WithResponseExample`, `WithAdditionalResponse`, `WithContractProfiles` and `WithContractParameter`.

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

The minimal adapter provides:

- required JSON bodies by default, with `WithOptionalBody` for an absent body;
- Gin binding validation plus `WithValidator` for application validation;
- a 1 MiB default body limit, configurable with `WithBodyLimit`;
- fixed success responses through `WithSuccess`;
- documented and runtime-consistent JSON errors for binding (400) and handler failures (500);
- `WithStrictWriter` to record direct handler writes as contract violations without writing a second response.

Request and response types must have a standard JSON representation that the current reflector can describe exactly. Pointer responses, interfaces, custom JSON codecs, byte sequences, implicit embedded-field flattening and `json:",string"` are rejected during registration. Status 204 and 205 never serialize a body.

Streaming, SSE, WebSocket, proxy/pass-through, downloads, multipart and handlers that intentionally control multiple responses continue to use `gin.HandlerFunc` with a manual `Contract`.

### Schema Descriptors

`SchemaOf[T]` documents a type without constructing a value. It works with pointers, slices, maps and generic envelopes:

```go
group.GET("/users", listUsers, "List users", 20).
	Document().
	Response(200, "OK", routekit.SchemaOf[Page[UserDTO]]())
```

`SchemaWithExample[T](example)` attaches a descriptor-level example. A `DocBody.Example` or `DocResponse.Example` value takes precedence.

### Documented Middleware

`UseDocumented` registers the same Gin middleware in the runtime chain and stores documentation metadata for routes that include it:

```go
group.UseDocumented(authMiddleware, routekit.MiddlewareMetadata{
	Parameters: []routekit.DocParam{{Name: "Authorization", In: routekit.DocParamInHeader, Type: "string", Required: true}},
	Responses:  []routekit.DocResponse{{Status: 401, Description: "Unauthorized"}},
})

group.GET("/reports", reports, "Reports", 30).
	UseDocumented(rateLimitMiddleware, routekit.MiddlewareMetadata{
		Responses: []routekit.DocResponse{{Status: 429, Description: "Too Many Requests"}},
	})
```

### Security Requirements

`OpenAPIOperation.AddSecurity("BearerAuth")` keeps the legacy behavior: each call adds an OR alternative.

Use `AddSecurityRequirement` when multiple schemes are required together:

```go
ctx.Operation.AddSecurityRequirement("ClientID", "ClientSecret") // ClientID AND ClientSecret
ctx.Operation.AddSecurity("BearerAuth")                          // OR BearerAuth
```
