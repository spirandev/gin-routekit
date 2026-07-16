package routekit

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type groupRegistrar struct {
	group *RouterGroup
	name  string
	appID int64
}

func (g groupRegistrar) Register(engine *gin.Engine) Route {
	return g.group.Export(g.name, g.appID)
}

func newTestEngine(t *testing.T, groups ...groupRegistrar) *AppRouter {
	t.Helper()
	registrars := make([]RouteRegistrar, len(groups))
	for i, g := range groups {
		registrars[i] = g
	}
	return NewAppRouterFromRegistrars(registrars, nil)
}

func newEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func newGroup(t *testing.T, engine *gin.Engine, path string, name string, appID int64) groupRegistrar {
	t.Helper()
	return groupRegistrar{group: NewRouterGroup(engine, path), name: name, appID: appID}
}

func TestRoutesSnapshotAfterRegister(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1)
	group.group.GET("/users", okHandler, "users", 2)

	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	routes := ar.Routes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if len(routes[0].Handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(routes[0].Handlers))
	}
}

func TestRoutesReturnsDefensiveCopy(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1)

	ar := newTestEngine(t, group)
	_ = ar.RegisterRoutes(engine)

	first := ar.Routes()
	first[0].Handlers[0].Method = "PATCH"
	second := ar.Routes()
	if second[0].Handlers[0].Method != http.MethodGet {
		t.Errorf("Routes() did not return a defensive copy; got %q want %q",
			second[0].Handlers[0].Method, http.MethodGet)
	}
}

func TestRoutesMiddlewareCompatibility(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1).
		Use(countingMiddleware(new(int)))

	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func baseConfig() OpenAPIConfig {
	return OpenAPIConfig{
		Title:            "Test API",
		Version:          "1.0.0",
		JSONPath:         "/openapi.json",
		BasePath:         "/",
		PathMode:         FullRegisteredPaths,
		EnabledByDefault: true,
		Defaults:         DocumentationDefaults{Responses: []DocResponse{{Status: 200, Description: "OK"}}},
	}
}

func buildDoc(t *testing.T, ar *AppRouter, config OpenAPIConfig) *OpenAPIDocument {
	t.Helper()
	engine := newEngine()
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	doc, err := BuildOpenAPI(ar.routes, config)
	if err != nil {
		t.Fatalf("BuildOpenAPI: %v", err)
	}
	return doc
}

func TestDocumentIncludesRouteWhenDisabledByDefault(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.POST("/login", okHandler, "Login", 1).Public().
		Document().
		Response(200, "OK", nil)

	config := baseConfig()
	config.EnabledByDefault = false
	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, config)

	path, ok := doc.Paths["/api/login"]
	if !ok {
		t.Fatalf("expected path /api/login, got %v", doc.Paths)
	}
	if path.Post == nil {
		t.Fatal("expected post operation")
	}
}

func TestHideFromDocsExcludesRouteWhenEnabledByDefault(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/hidden", okHandler, "Hidden", 1).HideFromDocs()
	group.group.GET("/visible", okHandler, "Visible", 1).Document()

	config := baseConfig()
	config.EnabledByDefault = true
	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, config)

	if _, ok := doc.Paths["/api/hidden"]; ok {
		t.Error("expected /api/hidden to be excluded")
	}
	if _, ok := doc.Paths["/api/visible"]; !ok {
		t.Error("expected /api/visible to be present")
	}
}

func TestSummaryDefaultsToDefinition(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "Custom Definition", 1).Document()

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())
	if doc.Paths["/api/resource"].Get.Summary != "Custom Definition" {
		t.Errorf("summary = %q, want %q",
			doc.Paths["/api/resource"].Get.Summary, "Custom Definition")
	}
}

func TestTagsDefaultFromGroup(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "mygroup", 123)
	group.group.GET("/resource", okHandler, "resource", 1).Document()

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())
	if len(doc.Paths["/api/resource"].Get.Tags) == 0 || doc.Paths["/api/resource"].Get.Tags[0] != "mygroup" {
		t.Errorf("expected tag mygroup, got %v", doc.Paths["/api/resource"].Get.Tags)
	}
	if len(doc.Tags) != 1 || doc.Tags[0].Name != "mygroup" {
		t.Errorf("expected tag list [mygroup], got %v", doc.Tags)
	}
}

func TestGinPathParamConversion(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/clients/:id", okHandler, "get client", 1).Document().PathParam("id", "string", true, "client ID")

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())
	if _, ok := doc.Paths["/api/clients/{id}"]; !ok {
		t.Errorf("expected path /api/clients/{id}, got %v", doc.Paths)
	}
}

func TestGinWildcardPathConversion(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/files", "files", 123)
	group.group.GET("/*path", okHandler, "get file", 1).Document().PathParam("path", "string", true, "file path")

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())
	if _, ok := doc.Paths["/files/{path}"]; !ok {
		t.Errorf("expected path /files/{path}, got %v", doc.Paths)
	}
}

func TestPathParamsMustBeDeclared(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/clients/:id", okHandler, "get client", 1).Document()

	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	_, err := BuildOpenAPI(ar.routes, baseConfig())
	if err == nil || !strings.Contains(err.Error(), "has no matching parameter") {
		t.Fatalf("expected missing path parameter error, got %v", err)
	}
}

func TestManualParamsAppearInOpenAPI(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/items", okHandler, "list", 1).Document().
		Header("X-Token", "string", true, "auth token").
		Query("q", "string", false, "search")
	_ = group

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())
	params := doc.Paths["/api/items"].Get.Parameters

	hasHeader := false
	hasQuery := false
	for _, p := range params {
		if p.Name == "X-Token" && p.In == "header" {
			hasHeader = true
		}
		if p.Name == "q" && p.In == "query" {
			hasQuery = true
		}
	}
	if !hasHeader {
		t.Errorf("missing header param, got %v", params)
	}
	if !hasQuery {
		t.Errorf("missing query param, got %v", params)
	}
}

func TestProfilesAddParamsAndSecurity(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/context", okHandler, "context", 1).
		Public().
		Document().
		DocProfile("core:app-context")

	config := baseConfig()
	config.Profiles = map[string]DocProfile{
		"core:app-context": {
			Headers: []DocParam{
				{Name: "Device", In: DocParamInHeader, Type: "string", Required: true},
			},
			Security: []string{"BearerAuth"},
		},
	}
	config.Components.SecuritySchemes = map[string]*OpenAPISecurityScheme{"BearerAuth": BearerSecurityScheme("Bearer")}
	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, config)

	op := doc.Paths["/api/context"].Get
	hasHeader := false
	for _, p := range op.Parameters {
		if p.Name == "Device" && p.In == "header" {
			hasHeader = true
		}
	}
	if !hasHeader {
		t.Errorf("expected header Device from profile")
	}
	hasSecurity := false
	for _, s := range op.Security {
		if _, ok := s["BearerAuth"]; ok {
			hasSecurity = true
		}
	}
	if !hasSecurity {
		t.Errorf("expected security from profile")
	}
}

func TestMissingProfileReturnsError(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/context", okHandler, "context", 1).Document().DocProfile("missing")

	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	_, err := BuildOpenAPI(ar.routes, baseConfig())
	if err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("expected profile error, got %v", err)
	}
}

func TestDecoratorModifiesOperation(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1).Document()

	config := baseConfig()
	config.Components.SecuritySchemes = map[string]*OpenAPISecurityScheme{"BearerAuth": BearerSecurityScheme("Bearer")}
	config.RouteDecorators = []RouteDocDecorator{
		func(ctx *RouteDocContext) error {
			ctx.Operation.AddHeader("X-Custom", "string", true, "custom header")
			ctx.Operation.AddSecurity("BearerAuth")
			return nil
		},
	}
	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, config)

	op := doc.Paths["/api/resource"].Get
	hasHeader := false
	for _, p := range op.Parameters {
		if p.Name == "X-Custom" {
			hasHeader = true
		}
	}
	if !hasHeader {
		t.Error("decorator did not add header")
	}
	if len(op.Security) == 0 {
		t.Error("decorator did not add security")
	}
}

func TestDecoratorAddResponseUsesSharedComponents(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.POST("/resource", okHandler, "resource", 1).Document()

	config := baseConfig()
	config.RouteDecorators = []RouteDocDecorator{
		func(ctx *RouteDocContext) error {
			ctx.Operation.AddResponse(201, "Created", loginDTO{})
			return nil
		},
	}
	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, config)

	schema := doc.Paths["/api/resource"].Post.Responses["201"].Content["application/json"].Schema
	if schema == nil {
		t.Fatal("expected response schema")
	}
	if schema.Ref == "" {
		t.Fatalf("decorator response schema should reuse a component, got %#v", schema)
	}
}

func TestDuplicateParamWithNonComparableExampleDoesNotPanic(t *testing.T) {
	op := &OpenAPIOperation{}
	param := OpenAPIParameter{
		Name:    "X-Example",
		In:      "header",
		Schema:  &OpenAPISchema{Type: "string"},
		Example: map[string]any{"values": []string{"a", "b"}},
	}
	op.AddParameter(param)
	op.AddParameter(param)

	if len(op.Parameters) != 1 {
		t.Fatalf("expected deduplicated parameter, got %d", len(op.Parameters))
	}
}

func TestDecoratorErrorPropagates(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1).Document()

	config := baseConfig()
	config.RouteDecorators = []RouteDocDecorator{
		func(ctx *RouteDocContext) error {
			return http.ErrAbortHandler
		},
	}
	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	_, err := BuildOpenAPI(ar.routes, config)
	if err == nil {
		t.Fatal("expected decorator error to propagate")
	}
}

func TestDuplicateOperationIDReturnsError(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/a", okHandler, "a", 1).Document()
	group.group.GET("/b", okHandler, "a", 2).Document()

	config := baseConfig()
	// Force identical operation ids manually via decorator.
	config.RouteDecorators = []RouteDocDecorator{
		func(ctx *RouteDocContext) error {
			ctx.Operation.OperationID = "sameId"
			return nil
		},
	}
	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	_, err := BuildOpenAPI(ar.routes, config)
	if err == nil || !strings.Contains(err.Error(), "duplicate operationId") {
		t.Fatalf("expected duplicate operationId error, got %v", err)
	}
}

func TestConflictingParamReturnsError(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1).
		Document().
		Header("X-Both", "string", true, "from manual")

	config := baseConfig()
	config.RouteDecorators = []RouteDocDecorator{
		func(ctx *RouteDocContext) error {
			ctx.Operation.AddHeader("X-Both", "string", false, "from decorator")
			return nil
		},
	}
	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	_, err := BuildOpenAPI(ar.routes, config)
	if err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("expected conflicting param error, got %v", err)
	}
}

type loginDTO struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password"`
}

type loginResponseDTO struct {
	Token string `json:"token"`
}

func TestBodyAndResponseUseJSONDefault(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.POST("/login", okHandler, "Login", 1).
		Public().
		Document().
		Body(loginDTO{}).
		Response(200, "OK", loginResponseDTO{})

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())

	op := doc.Paths["/api/login"].Post
	if op.RequestBody == nil {
		t.Fatal("expected request body")
	}
	if _, ok := op.RequestBody.Content["application/json"]; !ok {
		t.Errorf("expected application/json, got %v", op.RequestBody.Content)
	}
	resp, ok := op.Responses["200"]
	if !ok {
		t.Fatal("expected 200 response")
	}
	if _, ok := resp.Content["application/json"]; !ok {
		t.Errorf("expected application/json content, got %v", resp.Content)
	}
}

func TestRouteWithoutResponseIsRejected(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1).Document()

	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	config := baseConfig()
	config.Defaults.Responses = nil
	_, err := BuildOpenAPI(ar.routes, config)
	if err == nil || !strings.Contains(err.Error(), "at least one response") {
		t.Fatalf("expected missing response error, got %v", err)
	}
}

func TestSchemaReflection(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.POST("/login", okHandler, "Login", 1).
		Public().
		Document().
		Body(loginDTO{}).
		Response(200, "OK", loginResponseDTO{})

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())

	if doc.Components == nil {
		t.Fatal("expected components with generated schemas")
	}
	hasLoginSchema := false
	for name := range doc.Components.Schemas {
		if strings.Contains(name, "loginDTO") {
			hasLoginSchema = true
		}
	}
	if !hasLoginSchema {
		t.Errorf("expected loginDTO schema in components, got %v", doc.Components.Schemas)
	}

	op := doc.Paths["/api/login"].Post
	bodySchema := op.RequestBody.Content["application/json"].Schema
	if bodySchema == nil || len(bodySchema.AnyOf) != 2 || bodySchema.AnyOf[0].Ref == "" {
		t.Errorf("expected request body to use $ref, got %+v", bodySchema)
	}
}

func TestSchemaReflectionPrimitives(t *testing.T) {
	reflector := newSchemaReflector()
	cases := []struct {
		value      any
		wantType   string
		wantFormat string
	}{
		{"x", "string", ""},
		{true, "boolean", ""},
		{int(5), "integer", "int32"},
		{int64(5), "integer", "int64"},
		{float32(1.0), "number", "float"},
		{float64(1.0), "number", "double"},
	}
	for _, c := range cases {
		s := reflector.schemaFromValue(c.value)
		if s.Type != c.wantType || s.Format != c.wantFormat {
			t.Errorf("got %+v, want type=%q format=%q", s, c.wantType, c.wantFormat)
		}
	}
}

func TestSchemaReflectionSliceAndPointer(t *testing.T) {
	reflector := newSchemaReflector()
	s := reflector.schemaFromValue([]string{"x"})
	if len(s.AnyOf) != 2 || s.AnyOf[0].Type != "array" || s.AnyOf[0].Items == nil || s.AnyOf[0].Items.Type != "string" {
		t.Errorf("slice schema = %+v", s)
	}

	s2 := reflector.schemaFromValue((*string)(nil))
	if s2 == nil {
		t.Skip("nil pointer type not reflectable")
	}
	if len(s2.AnyOf) != 2 || s2.AnyOf[0].Type != "string" || s2.AnyOf[1].Type != "null" {
		t.Errorf("pointer schema = %+v", s2)
	}
}

func TestSecuritySchemesInComponents(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1).Document()

	config := baseConfig()
	config.Components.SecuritySchemes = map[string]*OpenAPISecurityScheme{
		"BearerAuth": BearerSecurityScheme("jwt"),
		"ApiKey":     APIKeyHeaderSecurityScheme("X-Key", "key"),
	}
	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, config)

	if doc.Components == nil {
		t.Fatal("expected components")
	}
	if _, ok := doc.Components.SecuritySchemes["BearerAuth"]; !ok {
		t.Error("missing BearerAuth")
	}
	if _, ok := doc.Components.SecuritySchemes["ApiKey"]; !ok {
		t.Error("missing ApiKey")
	}
}

func TestRegisterOpenAPIFailsBeforeRegisterRoutes(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)
	err := ar.RegisterOpenAPI(engine, baseConfig())
	if err != errRegisterRoutesRequired {
		t.Errorf("expected errRegisterRoutesRequired, got %v", err)
	}
}

func TestOpenAPIEndpointServesCachedJSON(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1).Document()

	ar := newTestEngine(t, group)
	engine2 := newEngine()
	if err := ar.RegisterRoutes(engine2); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	expectedDocument, err := ar.BuildOpenAPI(baseConfig())
	if err != nil {
		t.Fatalf("BuildOpenAPI: %v", err)
	}
	expectedPayload, err := MarshalOpenAPI(expectedDocument)
	if err != nil {
		t.Fatalf("MarshalOpenAPI: %v", err)
	}
	if err := ar.RegisterOpenAPI(engine2, baseConfig()); err != nil {
		t.Fatalf("RegisterOpenAPI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	engine2.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), expectedPayload) {
		t.Fatalf("served payload differs from MarshalOpenAPI\nserved: %s\nexpected: %s", rec.Body.Bytes(), expectedPayload)
	}
	var doc OpenAPIDocument
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid json: %v\nbody: %s", err, rec.Body.String())
	}
	if doc.OpenAPI != "3.1.0" || doc.JSONSchemaDialect != jsonSchemaDialect202012 {
		t.Errorf("OpenAPI metadata = %q %q", doc.OpenAPI, doc.JSONSchemaDialect)
	}
	if _, ok := doc.Paths["/api/resource"]; !ok {
		t.Errorf("expected /api/resource in served doc, got %s", rec.Body.String())
	}
}
