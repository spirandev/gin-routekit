package routekit

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	orderdto "github.com/spirandev/gin-routekit/internal/testschema/order"
	userdto "github.com/spirandev/gin-routekit/internal/testschema/user"
)

type envelope[T any] struct {
	Data T `json:"data"`
}

func TestDocumentationModeCompatibilityAndOverrides(t *testing.T) {
	t.Run("empty mode preserves enabled by default false", func(t *testing.T) {
		engine := newEngine()
		group := newGroup(t, engine, "/api", "api", 1)
		group.group.GET("/hidden", okHandler, "hidden", 1)

		ar := newTestEngine(t, group)
		doc := buildDoc(t, ar, OpenAPIConfig{Title: "Test", Version: "1"})
		if len(doc.Paths) != 0 {
			t.Fatalf("expected no paths, got %v", doc.Paths)
		}
	})

	t.Run("document all includes route without Document", func(t *testing.T) {
		engine := newEngine()
		group := newGroup(t, engine, "/api", "api", 1)
		group.group.GET("/visible", okHandler, "visible", 1)

		config := baseConfig()
		config.EnabledByDefault = false
		config.DocumentationMode = DocumentAll
		doc := buildDoc(t, newTestEngine(t, group), config)
		if doc.Paths["/api/visible"].Get == nil {
			t.Fatal("expected GET /api/visible")
		}
	})

	t.Run("hide from docs wins over document all", func(t *testing.T) {
		engine := newEngine()
		group := newGroup(t, engine, "/api", "api", 1)
		group.group.GET("/hidden", okHandler, "hidden", 1).HideFromDocs()

		config := baseConfig()
		config.DocumentationMode = DocumentAll
		doc := buildDoc(t, newTestEngine(t, group), config)
		if _, ok := doc.Paths["/api/hidden"]; ok {
			t.Fatal("expected hidden route to be excluded")
		}
	})

	t.Run("unknown mode is rejected", func(t *testing.T) {
		_, err := BuildOpenAPI(nil, OpenAPIConfig{Title: "Test", Version: "1", DocumentationMode: DocumentationMode("bad")})
		if err == nil || !strings.Contains(err.Error(), "DocumentationMode") {
			t.Fatalf("expected DocumentationMode error, got %v", err)
		}
	})
}

func TestGroupDocumentationDefaultsAndTombstones(t *testing.T) {
	engine := newEngine()
	documented := groupRegistrar{
		group: NewRouterGroup(engine, "/doc",
			WithDocumentation(),
			WithDocProfiles("tenant"),
			WithDefaultHeader(DocParam{Name: "X-Tenant", In: DocParamInHeader, Type: "string", Required: true}),
			WithDefaultResponse(401, "Unauthorized", nil),
		),
		name: "doc", appID: 1,
	}
	plain := newGroup(t, engine, "/plain", "plain", 1)
	documented.group.GET("/resource", okHandler, "resource", 1).
		WithoutDocProfile("tenant").
		WithoutDefaultResponse(401).
		WithoutDefaultParameter(DocParamInHeader, "x-tenant").
		Header("x-tenant", "string", false, "endpoint override").
		Response(200, "OK", nil)
	plain.group.GET("/resource", okHandler, "resource", 1)

	config := baseConfig()
	config.EnabledByDefault = false
	config.Profiles = map[string]DocProfile{
		"tenant": {Headers: []DocParam{{Name: "X-Profile", In: DocParamInHeader, Type: "string", Required: true}}},
	}
	doc := buildDoc(t, newTestEngine(t, documented, plain), config)
	if _, ok := doc.Paths["/plain/resource"]; ok {
		t.Fatal("plain group should not inherit WithDocumentation from another group")
	}
	op := doc.Paths["/doc/resource"].Get
	if op == nil {
		t.Fatal("expected documented group operation")
	}
	if _, ok := op.Responses["401"]; ok {
		t.Fatal("default response should have been removed")
	}
	if findParam(op.Parameters, "X-Profile", "header") != nil {
		t.Fatal("profile tombstone should remove inherited profile effects")
	}
	param := findParam(op.Parameters, "X-Tenant", "header")
	if param == nil || param.Required || param.Description != "endpoint override" {
		t.Fatalf("expected endpoint header override, got %#v", param)
	}
}

func TestEndpointOverridesDefaultResponseByStatus(t *testing.T) {
	engine := newEngine()
	group := groupRegistrar{
		group: NewRouterGroup(engine, "/api", WithDocumentation(), WithDefaultResponse(200, "Default", nil)),
		name:  "api",
		appID: 1,
	}
	group.group.GET("/resource", okHandler, "resource", 1).Response(200, "Endpoint", nil)

	config := baseConfig()
	config.EnabledByDefault = false
	doc := buildDoc(t, newTestEngine(t, group), config)
	if got := doc.Paths["/api/resource"].Get.Responses["200"].Description; got != "Endpoint" {
		t.Fatalf("response description = %q, want Endpoint", got)
	}
}

func TestConflictsWithinSameLayerReturnError(t *testing.T) {
	engine := newEngine()
	group := groupRegistrar{
		group: NewRouterGroup(engine, "/api", WithDocumentation(),
			WithDefaultHeader(DocParam{Name: "X-Test", In: DocParamInHeader, Type: "string", Required: true}),
			WithDefaultHeader(DocParam{Name: "x-test", In: DocParamInHeader, Type: "string", Required: false}),
		),
		name: "api", appID: 1,
	}
	group.group.GET("/resource", okHandler, "resource", 1)
	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	config := baseConfig()
	config.EnabledByDefault = false
	_, err := BuildOpenAPI(ar.routes, config)
	if err == nil || !strings.Contains(err.Error(), "conflicting parameter") {
		t.Fatalf("expected same-layer conflict error, got %v", err)
	}
}

func TestContractAndFluentPrecedence(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 1)
	DescribeJSON[loginDTO, loginResponseDTO](group.group.POST("/login", okHandler, "Login", 1).Document(), 201, "Created")
	group.group.POST("/override", okHandler, "Override", 2).
		Document().
		Contract(JSONContractOf[loginDTO, loginResponseDTO](201, "Created")).
		Response(201, "Accepted", SchemaOf[loginResponseDTO]())

	doc := buildDoc(t, newTestEngine(t, group), baseConfig())
	login := doc.Paths["/api/login"].Post
	if login.RequestBody == nil || login.RequestBody.Content["application/json"].Schema.Ref == "" {
		t.Fatal("expected contract request body component ref")
	}
	if _, ok := login.Responses["201"]; !ok {
		t.Fatal("expected contract response")
	}
	if got := doc.Paths["/api/override"].Post.Responses["201"].Description; got != "Accepted" {
		t.Fatalf("fluent response should override contract response, got %q", got)
	}
}

func TestSchemaDescriptorInputAndExamples(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 1)
	descriptorExample := loginResponseDTO{Token: "descriptor"}
	explicitExample := map[string]any{"token": "explicit"}
	group.group.POST("/login", okHandler, "Login", 1).
		Document().
		Body(SchemaOf[*loginDTO]()).
		ResponseWith(200, "OK", "application/json", SchemaWithExample[loginResponseDTO](descriptorExample))
	group.group.POST("/explicit", okHandler, "Explicit", 2).
		Document().
		Contract(Contract{Responses: []DocResponse{{Status: 200, Description: "OK", Schema: SchemaWithExample[loginResponseDTO](descriptorExample), Example: explicitExample}}})

	doc := buildDoc(t, newTestEngine(t, group), baseConfig())
	if got := doc.Paths["/api/login"].Post.RequestBody.Content["application/json"].Schema.Ref; got == "" {
		t.Fatal("expected SchemaOf pointer to create component ref")
	}
	example := doc.Paths["/api/login"].Post.Responses["200"].Content["application/json"].Example
	if !reflect.DeepEqual(example, descriptorExample) {
		t.Fatalf("descriptor example = %#v, want %#v", example, descriptorExample)
	}
	example = doc.Paths["/api/explicit"].Post.Responses["200"].Content["application/json"].Example
	if !reflect.DeepEqual(example, explicitExample) {
		t.Fatalf("explicit example = %#v, want %#v", example, explicitExample)
	}
}

func TestSchemaComponentsHandlePackagesAndGenerics(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 1)
	group.group.GET("/users", okHandler, "users", 1).Document().Response(200, "OK", SchemaOf[userdto.UserDTO]())
	group.group.GET("/orders", okHandler, "orders", 2).Document().Response(200, "OK", SchemaOf[orderdto.UserDTO]())
	group.group.GET("/user-envelope", okHandler, "user envelope", 3).Document().Response(200, "OK", SchemaOf[envelope[userdto.UserDTO]]())
	group.group.GET("/order-envelope", okHandler, "order envelope", 4).Document().Response(200, "OK", SchemaOf[envelope[orderdto.UserDTO]]())

	doc := buildDoc(t, newTestEngine(t, group), baseConfig())
	if len(doc.Components.Schemas) < 4 {
		t.Fatalf("expected distinct package/generic components, got %#v", doc.Components.Schemas)
	}
	refs := map[string]bool{}
	refs[doc.Paths["/api/users"].Get.Responses["200"].Content["application/json"].Schema.Ref] = true
	refs[doc.Paths["/api/orders"].Get.Responses["200"].Content["application/json"].Schema.Ref] = true
	refs[doc.Paths["/api/user-envelope"].Get.Responses["200"].Content["application/json"].Schema.Ref] = true
	refs[doc.Paths["/api/order-envelope"].Get.Responses["200"].Content["application/json"].Schema.Ref] = true
	if len(refs) != 4 {
		t.Fatalf("expected four distinct refs, got %v", refs)
	}

	reversedEngine := newEngine()
	reversedGroup := newGroup(t, reversedEngine, "/api", "api", 1)
	reversedGroup.group.GET("/order-envelope", okHandler, "order envelope", 4).Document().Response(200, "OK", SchemaOf[envelope[orderdto.UserDTO]]())
	reversedGroup.group.GET("/user-envelope", okHandler, "user envelope", 3).Document().Response(200, "OK", SchemaOf[envelope[userdto.UserDTO]]())
	reversedGroup.group.GET("/orders", okHandler, "orders", 2).Document().Response(200, "OK", SchemaOf[orderdto.UserDTO]())
	reversedGroup.group.GET("/users", okHandler, "users", 1).Document().Response(200, "OK", SchemaOf[userdto.UserDTO]())
	reversedDoc := buildDoc(t, newTestEngine(t, reversedGroup), baseConfig())

	paths := []string{"/api/users", "/api/orders", "/api/user-envelope", "/api/order-envelope"}
	for _, path := range paths {
		got := reversedDoc.Paths[path].Get.Responses["200"].Content["application/json"].Schema.Ref
		want := doc.Paths[path].Get.Responses["200"].Content["application/json"].Schema.Ref
		if got != want {
			t.Errorf("component ref for %s depends on discovery order: got %q, want %q", path, got, want)
		}
	}
	typesByPath := map[string]reflect.Type{
		"/api/users":          reflect.TypeOf(userdto.UserDTO{}),
		"/api/orders":         reflect.TypeOf(orderdto.UserDTO{}),
		"/api/user-envelope":  reflect.TypeOf(envelope[userdto.UserDTO]{}),
		"/api/order-envelope": reflect.TypeOf(envelope[orderdto.UserDTO]{}),
	}
	publicNameCounts := map[string]int{}
	for _, typ := range typesByPath {
		publicNameCounts[publicSchemaName(typ)]++
	}
	for path, typ := range typesByPath {
		name := publicSchemaName(typ)
		if publicNameCounts[name] > 1 {
			name += "_" + shortIdentityHash(schemaIdentity(typ))
		}
		want := "#/components/schemas/" + name
		got := doc.Paths[path].Get.Responses["200"].Content["application/json"].Schema.Ref
		if got != want {
			t.Errorf("component ref for %s = %q, want deterministic %q", path, got, want)
		}
	}
}

func TestUseDocumentedMetadataAndRuntimeOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	counts := middlewareCounts{}
	engine := gin.New()
	group := NewRouterGroup(engine, "/api")
	group.UseDocumented(countingMiddleware(&counts.auth), MiddlewareMetadata{
		Parameters: []DocParam{{Name: "X-Group", In: DocParamInHeader, Type: "string"}},
	})
	group.GET("/with", okHandler, "with", 1).Document().UseDocumented(countingMiddleware(&counts.custom), MiddlewareMetadata{
		Responses: []DocResponse{{Status: 429, Description: "Too Many Requests"}},
	})
	group.GET("/without", okHandler, "without", 2).Document()

	registrar := groupRegistrar{group: group, name: "api", appID: 1}
	ar := newTestEngine(t, registrar)
	doc := buildDoc(t, ar, baseConfig())
	with := doc.Paths["/api/with"].Get
	if findParam(with.Parameters, "X-Group", "header") == nil || with.Responses["429"].Description == "" {
		t.Fatalf("expected group and route middleware metadata, got %#v", with)
	}
	without := doc.Paths["/api/without"].Get
	if _, ok := without.Responses["429"]; ok {
		t.Fatal("route middleware metadata leaked to another route")
	}
	rec := httptestNewRecorder()
	req := httptestNewRequest(http.MethodGet, "/api/with")
	engine.ServeHTTP(rec, req)
	if counts.auth != 1 || counts.custom != 1 {
		t.Fatalf("documented middlewares did not run, counts=%+v", counts)
	}
}

func TestSecurityRequirementAndValidation(t *testing.T) {
	op := &OpenAPIOperation{}
	op.AddSecurityRequirement("ClientID", "ClientSecret")
	op.AddSecurity("BearerAuth")
	if len(op.Security) != 2 || len(op.Security[0]) != 2 || len(op.Security[1]) != 1 {
		t.Fatalf("unexpected security requirements: %#v", op.Security)
	}

	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 1)
	group.group.GET("/secure", okHandler, "secure", 1).Document()
	config := baseConfig()
	config.Components.SecuritySchemes = map[string]*OpenAPISecurityScheme{"Known": BearerSecurityScheme("")}
	config.RouteDecorators = []RouteDocDecorator{func(ctx *RouteDocContext) error {
		ctx.Operation.AddSecurity("Missing")
		return nil
	}}
	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	_, err := BuildOpenAPI(ar.routes, config)
	if err == nil || !strings.Contains(err.Error(), "security scheme") {
		t.Fatalf("expected security scheme validation error, got %v", err)
	}
}

func TestAdditionalOpenAPIValidations(t *testing.T) {
	t.Run("invalid path param", func(t *testing.T) {
		engine := newEngine()
		group := newGroup(t, engine, "/api", "api", 1)
		group.group.GET("/items", okHandler, "items", 1).Document().PathParam("missing", "string", true, "")
		ar := newTestEngine(t, group)
		if err := ar.RegisterRoutes(engine); err != nil {
			t.Fatalf("RegisterRoutes: %v", err)
		}
		_, err := BuildOpenAPI(ar.routes, baseConfig())
		if err == nil || !strings.Contains(err.Error(), "not present") {
			t.Fatalf("expected path param error, got %v", err)
		}
	})
	t.Run("path param must be required", func(t *testing.T) {
		engine := newEngine()
		group := newGroup(t, engine, "/api", "api", 1)
		group.group.GET("/items/:id", okHandler, "items", 1).Document().PathParam("id", "string", false, "")
		ar := newTestEngine(t, group)
		if err := ar.RegisterRoutes(engine); err != nil {
			t.Fatalf("RegisterRoutes: %v", err)
		}
		_, err := BuildOpenAPI(ar.routes, baseConfig())
		if err == nil || !strings.Contains(err.Error(), "must be required") {
			t.Fatalf("expected required path param error, got %v", err)
		}
	})
	t.Run("duplicate path method", func(t *testing.T) {
		doc := &DocConfig{Enabled: boolPtr(true)}
		_, err := BuildOpenAPI([]Route{{Path: "/api", Group: "api", Handlers: []Handler{
			{Method: http.MethodGet, Path: "/same", RelativePath: "/same", Definition: "same", Doc: doc},
			{Method: http.MethodGet, Path: "/same", RelativePath: "/same", Definition: "same", Doc: doc},
		}}}, baseConfig())
		if err == nil || !strings.Contains(err.Error(), "duplicate operation") {
			t.Fatalf("expected duplicate operation error, got %v", err)
		}
	})
	t.Run("invalid status", func(t *testing.T) {
		engine := newEngine()
		group := newGroup(t, engine, "/api", "api", 1)
		group.group.GET("/resource", okHandler, "resource", 1).Document().Response(99, "Invalid", nil)
		ar := newTestEngine(t, group)
		if err := ar.RegisterRoutes(engine); err != nil {
			t.Fatalf("RegisterRoutes: %v", err)
		}
		_, err := BuildOpenAPI(ar.routes, baseConfig())
		if err == nil || !strings.Contains(err.Error(), "invalid response status") {
			t.Fatalf("expected invalid status error, got %v", err)
		}
	})
	t.Run("unknown parameter location", func(t *testing.T) {
		engine := newEngine()
		group := newGroup(t, engine, "/api", "api", 1)
		group.group.GET("/resource", okHandler, "resource", 1).Document().Contract(Contract{Parameters: []DocParam{{Name: "bad", In: DocParamIn("cookie"), Type: "string"}}})
		ar := newTestEngine(t, group)
		if err := ar.RegisterRoutes(engine); err != nil {
			t.Fatalf("RegisterRoutes: %v", err)
		}
		_, err := BuildOpenAPI(ar.routes, baseConfig())
		if err == nil || !strings.Contains(err.Error(), "unknown parameter location") {
			t.Fatalf("expected parameter location error, got %v", err)
		}
	})
	t.Run("empty security requirement", func(t *testing.T) {
		engine := newEngine()
		group := newGroup(t, engine, "/api", "api", 1)
		group.group.GET("/resource", okHandler, "resource", 1).Document()
		config := baseConfig()
		config.RouteDecorators = []RouteDocDecorator{func(ctx *RouteDocContext) error {
			ctx.Operation.AddSecurityRequirement()
			return nil
		}}
		ar := newTestEngine(t, group)
		if err := ar.RegisterRoutes(engine); err != nil {
			t.Fatalf("RegisterRoutes: %v", err)
		}
		_, err := BuildOpenAPI(ar.routes, config)
		if err == nil || !strings.Contains(err.Error(), "security requirement") {
			t.Fatalf("expected empty security requirement error, got %v", err)
		}
	})
}

func TestRoutesCloneMutableDocumentationMetadata(t *testing.T) {
	engine := newEngine()
	group := groupRegistrar{group: NewRouterGroup(engine, "/api", WithDocumentation(), WithDefaultResponse(400, "Bad", nil)), name: "api", appID: 1}
	group.group.options.documentationDefaults.Responses[0].Example = map[string]any{"error": []any{"old"}}
	group.group.GET("/resource", okHandler, "resource", 1).
		Contract(JSONContractOf[loginDTO, loginResponseDTO](200, "OK", WithResponseExample(map[string]any{"token": []any{"old"}}))).
		UseDocumented(okHandler, MiddlewareMetadata{Parameters: []DocParam{{Name: "X-Test", In: DocParamInHeader, Example: map[string]any{"a": []any{"b"}}}}})
	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	first := ar.Routes()
	first[0].DocumentationDefaults.Responses[0].Example.(map[string]any)["error"].([]any)[0] = "changed"
	first[0].Handlers[0].Contract.Responses[0].Example.(map[string]any)["token"].([]any)[0] = "changed"
	first[0].Handlers[0].MiddlewareMetadata[0].Parameters[0].Example.(map[string]any)["a"].([]any)[0] = "changed"
	second := ar.Routes()
	if second[0].DocumentationDefaults.Responses[0].Example.(map[string]any)["error"].([]any)[0] != "old" {
		t.Fatal("documentation defaults example was shared")
	}
	if second[0].Handlers[0].Contract.Responses[0].Example.(map[string]any)["token"].([]any)[0] != "old" {
		t.Fatal("contract example was shared")
	}
	if second[0].Handlers[0].MiddlewareMetadata[0].Parameters[0].Example.(map[string]any)["a"].([]any)[0] != "b" {
		t.Fatal("middleware metadata example was shared")
	}
}

func findParam(params []OpenAPIParameter, name, in string) *OpenAPIParameter {
	for i := range params {
		if strings.EqualFold(params[i].Name, name) && params[i].In == in {
			return &params[i]
		}
	}
	return nil
}

func httptestNewRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

func httptestNewRequest(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}
