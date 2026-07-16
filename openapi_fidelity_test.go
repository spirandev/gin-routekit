package routekit

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func fidelityConfig() OpenAPIConfig {
	return OpenAPIConfig{
		Title: "Fidelity", Version: "1", BasePath: "/", PathMode: FullRegisteredPaths,
		DocumentationMode: DocumentAll,
	}
}

func TestValidateOpenAPIAggregatesDiagnostics(t *testing.T) {
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{
		{Method: http.MethodGet, RelativePath: "/items/:id", Path: "/items/:id", Definition: "items"},
		{Method: http.MethodPost, RelativePath: "/items", Path: "/items", Definition: "create", Doc: &DocConfig{Responses: []DocResponse{{Status: 99, Description: "invalid"}}}},
	}}}
	report, err := ValidateOpenAPI(routes, fidelityConfig())
	if err == nil || !report.HasErrors() {
		t.Fatalf("report = %#v, error = %v", report, err)
	}
	var diagnosticsError *DiagnosticsError
	if !errors.As(err, &diagnosticsError) || len(diagnosticsError.Report.Diagnostics) != len(report.Diagnostics) {
		t.Fatalf("errors.As report = %#v", diagnosticsError)
	}
	codes := map[string]bool{}
	for _, diagnostic := range report.Diagnostics {
		codes[diagnostic.Code] = true
	}
	for _, code := range []string{"path.parameter.missing", "response.required", "response.status"} {
		if !codes[code] {
			t.Errorf("missing diagnostic %s in %#v", code, report.Diagnostics)
		}
	}
	if document, buildErr := BuildOpenAPI(routes, fidelityConfig()); buildErr == nil || document != nil {
		t.Fatalf("BuildOpenAPI = %#v, %v", document, buildErr)
	}
}

func TestOpenAPIPathModes(t *testing.T) {
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{{
		Method: http.MethodGet, RelativePath: "/users", Path: "/users", Definition: "users",
		Doc: &DocConfig{Responses: []DocResponse{{Status: 200, Description: "OK"}}},
	}}}}
	config := fidelityConfig()
	config.BasePath = "/api"
	config.PathMode = PathsRelativeToBase
	document, err := BuildOpenAPI(routes, config)
	if err != nil {
		t.Fatal(err)
	}
	if document.Paths["/users"].Get == nil || len(document.Servers) != 1 || document.Servers[0].URL != "/api" {
		t.Fatalf("document = %#v", document)
	}

	config.PathMode = FullRegisteredPaths
	config.Servers = []OpenAPIServer{{URL: "https://example.test/api"}}
	report, err := ValidateOpenAPI(routes, config)
	if err == nil || !hasDiagnosticCode(report, "server.base_path.duplicate") {
		t.Fatalf("report = %#v, error = %v", report, err)
	}
}

func TestProfileAndTombstoneWarningsDoNotBlockBuild(t *testing.T) {
	routes := []Route{{Path: "/api", Group: "api", DocumentationDefaults: DocumentationDefaults{Profiles: []string{"audit"}}, Handlers: []Handler{{
		Method: http.MethodGet, RelativePath: "/value", Path: "/value", Definition: "value",
		Doc:         &DocConfig{Profiles: []string{"audit"}, Responses: []DocResponse{{Status: 200, Description: "OK"}}},
		DocRemovals: docRemovals{Parameters: []paramTombstone{{In: DocParamInHeader, Name: "X-Missing"}}},
	}}}}
	config := fidelityConfig()
	config.Profiles = map[string]DocProfile{"audit": {Description: "Audit"}}
	report, err := ValidateOpenAPI(routes, config)
	if err != nil || report.HasErrors() {
		t.Fatalf("report = %#v, error = %v", report, err)
	}
	if !hasDiagnosticCode(report, "profile.duplicate") || !hasDiagnosticCode(report, "tombstone.no_target") {
		t.Fatalf("warnings = %#v", report.Diagnostics)
	}
	if _, err := BuildOpenAPI(routes, config); err != nil {
		t.Fatalf("warnings blocked build: %v", err)
	}
}

type fidelityEmbedded struct {
	Promoted string `json:"promoted"`
}
type fidelityDTO struct {
	fidelityEmbedded
	Required      string           `json:"required" binding:"required"`
	Optional      *string          `json:"optional,omitempty"`
	Bytes         []byte           `json:"bytes"`
	Raw           json.RawMessage  `json:"raw"`
	Number        json.Number      `json:"number"`
	Quoted        int              `json:"quoted,string"`
	StructValue   fidelityEmbedded `json:"struct,omitempty"`
	StringPointer *int             `json:"stringPointer,string"`
}

type embeddedPointerDTO struct{ *fidelityEmbedded }

func TestDirectionalSchemasMatchJSONFields(t *testing.T) {
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{{
		Method: http.MethodPost, RelativePath: "/value", Path: "/value", Definition: "value",
		Contract: &Contract{RequestBody: &DocBody{Required: true, Schema: SchemaOf[fidelityDTO]()}, Responses: []DocResponse{{Status: 200, Description: "OK", Schema: SchemaOf[fidelityDTO]()}}},
	}}}}
	document, err := BuildOpenAPI(routes, fidelityConfig())
	if err != nil {
		t.Fatal(err)
	}
	operation := document.Paths["/api/value"].Post
	requestRoot := operation.RequestBody.Content["application/json"].Schema
	if requestRoot == nil || len(requestRoot.AnyOf) != 2 || requestRoot.AnyOf[1].Type != "null" {
		t.Fatalf("request root = %#v", requestRoot)
	}
	request := referencedSchema(t, document, &requestRoot.AnyOf[0])
	response := referencedSchema(t, document, operation.Responses["200"].Content["application/json"].Schema)
	if _, nested := request.Properties["fidelityEmbedded"]; nested {
		t.Fatal("embedded field was not promoted")
	}
	if _, promoted := request.Properties["promoted"]; !promoted {
		t.Fatalf("request properties = %#v", request.Properties)
	}
	if !reflect.DeepEqual(request.Required, []string{"required"}) {
		t.Fatalf("request required = %v", request.Required)
	}
	if contains(response.Required, "optional") || !contains(response.Required, "promoted") || !contains(response.Required, "bytes") {
		t.Fatalf("response required = %v", response.Required)
	}
	if request.Properties["bytes"].AnyOf[0].Format != "byte" || response.Properties["bytes"].AnyOf[0].Format != "byte" {
		t.Fatalf("byte schemas = %#v %#v", request.Properties["bytes"], response.Properties["bytes"])
	}
	if request.Properties["quoted"].AnyOf[0].Type != "string" || response.Properties["quoted"].Type != "string" {
		t.Fatalf("quoted schemas = %#v %#v", request.Properties["quoted"], response.Properties["quoted"])
	}
	if !contains(response.Required, "struct") {
		t.Fatalf("omitempty struct must remain required: %v", response.Required)
	}
	if len(response.Properties["stringPointer"].AnyOf) != 2 {
		t.Fatalf("pointer ,string must be nullable: %#v", response.Properties["stringPointer"])
	}

	pointerRoutes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{{Method: http.MethodGet, RelativePath: "/embedded", Path: "/embedded", Definition: "embedded", Doc: &DocConfig{Responses: []DocResponse{{Status: 200, Description: "OK", Schema: SchemaOf[embeddedPointerDTO]()}}}}}}}
	pointerDocument, err := BuildOpenAPI(pointerRoutes, fidelityConfig())
	if err != nil {
		t.Fatal(err)
	}
	pointerSchema := referencedSchema(t, pointerDocument, pointerDocument.Paths["/api/embedded"].Get.Responses["200"].Content["application/json"].Schema)
	if contains(pointerSchema.Required, "promoted") {
		t.Fatalf("promoted field through nil embedded pointer cannot be required: %v", pointerSchema.Required)
	}
}

type codecDTO struct{}

func (codecDTO) MarshalJSON() ([]byte, error) { return []byte(`"codec"`), nil }

type panicProvider struct{}

func (*panicProvider) OpenAPISchema() OpenAPISchema { panic("provider failed") }

type pointerProvider struct{}

func (*pointerProvider) OpenAPISchemaFor(direction SchemaDirection) OpenAPISchema {
	return OpenAPISchema{Type: "string", Format: string(direction)}
}

func TestSchemaRegistrationsAndProviders(t *testing.T) {
	makeRoutes := func(schema any) []Route {
		return []Route{{Path: "/api", Group: "api", Handlers: []Handler{{Method: http.MethodGet, RelativePath: "/value", Path: "/value", Definition: "value", Doc: &DocConfig{Responses: []DocResponse{{Status: 200, Description: "OK", Schema: schema}}}}}}}
	}
	config := fidelityConfig()
	config.SchemaRegistrations = []SchemaRegistration{OverrideSchemaOf[codecDTO](OpenAPISchema{Type: "string", Format: "codec"}, ForSchemaResponse())}
	document, err := BuildOpenAPI(makeRoutes(SchemaOf[codecDTO]()), config)
	if err != nil {
		t.Fatal(err)
	}
	schema := referencedSchema(t, document, document.Paths["/api/value"].Get.Responses["200"].Content["application/json"].Schema)
	if schema.Type != "string" || schema.Format != "codec" {
		t.Fatalf("override schema = %#v", schema)
	}

	document, err = BuildOpenAPI(makeRoutes(SchemaOf[pointerProvider]()), fidelityConfig())
	if err != nil {
		t.Fatal(err)
	}
	schema = referencedSchema(t, document, document.Paths["/api/value"].Get.Responses["200"].Content["application/json"].Schema)
	if schema.Format != string(SchemaResponse) {
		t.Fatalf("provider schema = %#v", schema)
	}

	report, err := ValidateOpenAPI(makeRoutes(SchemaOf[panicProvider]()), fidelityConfig())
	if err == nil || !hasDiagnosticCode(report, "schema.provider.panic") {
		t.Fatalf("report = %#v, error = %v", report, err)
	}
}

func TestSecurityRulesAndContributions(t *testing.T) {
	authenticated := true
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{{Method: http.MethodGet, RelativePath: "/secure", Path: "/secure", Definition: "secure", IsAuthentication: &authenticated, Doc: &DocConfig{Responses: []DocResponse{{Status: 200, Description: "OK"}}}}}}}
	config := fidelityConfig()
	config.Components.SecuritySchemes = map[string]*OpenAPISecurityScheme{"BearerAuth": BearerSecurityScheme("Bearer")}
	config.Profiles = map[string]DocProfile{"audit": {Description: "Audit context"}}
	config.SecurityRules = []RouteSecurityRule{WhenAuthenticated(RequireSecurityScheme("BearerAuth"), RequiresHeader("X-Trace", "string", true, "trace"), UsesProfiles("audit"))}
	document, err := BuildOpenAPI(routes, config)
	if err != nil {
		t.Fatal(err)
	}
	operation := document.Paths["/api/secure"].Get
	_, hasBearer := operation.Security[0]["BearerAuth"]
	if len(operation.Security) != 1 || !hasBearer {
		t.Fatalf("security = %#v", operation.Security)
	}
	if findParam(operation.Parameters, "X-Trace", "header") == nil {
		t.Fatalf("parameters = %#v", operation.Parameters)
	}
	payload, err := MarshalOpenAPI(document)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"BearerAuth": []`) {
		t.Fatalf("security scopes must serialize as an empty array: %s", payload)
	}
	if document.RoutekitProfiles["audit"].Description != "Audit context" {
		t.Fatalf("profiles = %#v", document.RoutekitProfiles)
	}
}

func TestExplicitSchemasAndDecoratorPanicsAreDiagnosed(t *testing.T) {
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{
		{Method: http.MethodPost, RelativePath: "/body", Path: "/body", Definition: "body", Doc: &DocConfig{RequestBody: &DocBody{Required: true}, Responses: []DocResponse{{Status: 200, Description: "OK"}}}},
		{Method: http.MethodGet, RelativePath: "/schema", Path: "/schema", Definition: "schema", Doc: &DocConfig{Responses: []DocResponse{{Status: 200, Description: "OK", Schema: OpenAPISchema{Type: "wrong"}}}}},
	}}}
	config := fidelityConfig()
	config.RouteDecorators = []RouteDocDecorator{func(*RouteDocContext) error { panic("boom") }}
	report, err := ValidateOpenAPI(routes, config)
	if err == nil {
		t.Fatal("expected diagnostics error")
	}
	for _, code := range []string{"request_body.schema.required", "schema.type.invalid", "decorator.error"} {
		if !hasDiagnosticCode(report, code) {
			t.Errorf("missing %s in %#v", code, report.Diagnostics)
		}
	}

	operation := &OpenAPIOperation{}
	operation.AddResponse(200, "OK", fidelityDTO{})
	standalone := operation.Responses["200"].Content["application/json"].Schema
	if standalone == nil || standalone.Ref != "" || standalone.Type != "object" {
		t.Fatalf("standalone AddResponse schema = %#v", standalone)
	}
}

func TestInvalidLargeStatusAndCyclicSchemaBecomeDiagnostics(t *testing.T) {
	cyclic := &OpenAPISchema{Type: "array"}
	cyclic.Items = cyclic
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{
		{Method: http.MethodGet, RelativePath: "/status", Path: "/status", Definition: "status", Doc: &DocConfig{Responses: []DocResponse{{Status: math.MaxInt, Description: "invalid"}}}},
		{Method: http.MethodGet, RelativePath: "/cycle", Path: "/cycle", Definition: "cycle", Doc: &DocConfig{Responses: []DocResponse{{Status: 200, Description: "OK", Schema: cyclic}}}},
	}}}
	report, err := ValidateOpenAPI(routes, fidelityConfig())
	if err == nil || !hasDiagnosticCode(report, "response.status") || !hasDiagnosticCode(report, "schema.cycle.invalid") {
		t.Fatalf("report = %#v, error = %v", report, err)
	}
}

type pointerTextKey struct{ Value int }

func (*pointerTextKey) MarshalText() ([]byte, error) { return []byte("key"), nil }

func TestResponseMapKeyRequiresValueTextMarshaler(t *testing.T) {
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{{Method: http.MethodGet, RelativePath: "/map", Path: "/map", Definition: "map", Doc: &DocConfig{Responses: []DocResponse{{Status: 200, Description: "OK", Schema: SchemaOf[map[pointerTextKey]string]()}}}}}}}
	report, err := ValidateOpenAPI(routes, fidelityConfig())
	if err == nil || !hasDiagnosticCode(report, "schema.map.key") {
		t.Fatalf("report = %#v, error = %v", report, err)
	}
}

func TestContractConstructorsAndRouterLifecycle(t *testing.T) {
	responseOnly := JSONResponseContractOf[fidelityDTO](http.StatusOK, "OK")
	if responseOnly.RequestBody != nil || responseOnly.Responses[0].Schema == nil {
		t.Fatalf("response-only contract = %#v", responseOnly)
	}
	empty := EmptyJSONResponseContract(http.StatusNoContent, "No Content")
	if empty.RequestBody != nil || empty.Responses[0].Schema != nil {
		t.Fatalf("empty contract = %#v", empty)
	}
	defaults := WithJSONDefaults(DefaultResponseOf[fidelityDTO](http.StatusOK, "OK"))
	if len(defaults.Responses) != 1 || defaults.Responses[0].ContentType != "application/json" {
		t.Fatalf("defaults = %#v", defaults)
	}

	gin.SetMode(gin.TestMode)
	router := NewAppRouter(nil, nil)
	if _, err := router.BuildOpenAPI(fidelityConfig()); !errors.Is(err, errRegisterRoutesRequired) {
		t.Fatalf("pre-registration error = %v", err)
	}
	if err := router.RegisterRoutes(gin.New()); err != nil {
		t.Fatal(err)
	}
	document, err := router.BuildOpenAPI(fidelityConfig())
	if err != nil || len(document.Paths) != 0 {
		t.Fatalf("empty document = %#v, %v", document, err)
	}
	first, err := MarshalOpenAPI(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalOpenAPI(document)
	if err != nil || string(first) != string(second) {
		t.Fatalf("marshal is not deterministic: %v", err)
	}
}

func TestMarshalOpenAPIIsIndependentOfRouteOrder(t *testing.T) {
	handlers := []Handler{
		{Method: http.MethodGet, RelativePath: "/a", Path: "/a", Definition: "a", Doc: &DocConfig{Responses: []DocResponse{{Status: 200, Description: "OK", Schema: SchemaOf[fidelityDTO]()}}}},
		{Method: http.MethodGet, RelativePath: "/b", Path: "/b", Definition: "b", Doc: &DocConfig{Responses: []DocResponse{{Status: 200, Description: "OK", Schema: SchemaOf[embeddedPointerDTO]()}}}},
	}
	first, err := BuildOpenAPI([]Route{{Path: "/api", Group: "api", Handlers: handlers}}, fidelityConfig())
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildOpenAPI([]Route{{Path: "/api", Group: "api", Handlers: []Handler{handlers[1], handlers[0]}}}, fidelityConfig())
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := MarshalOpenAPI(first)
	secondJSON, _ := MarshalOpenAPI(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("route order changed output\n%s\n%s", firstJSON, secondJSON)
	}
}

func referencedSchema(t *testing.T, document *OpenAPIDocument, schema *OpenAPISchema) *OpenAPISchema {
	t.Helper()
	if schema == nil {
		t.Fatal("schema is nil")
	}
	if schema.Ref == "" {
		return schema
	}
	name := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
	resolved := document.Components.Schemas[name]
	if resolved == nil {
		t.Fatalf("unresolved schema %q", schema.Ref)
	}
	return resolved
}

func hasDiagnosticCode(report DiagnosticReport, code string) bool {
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
