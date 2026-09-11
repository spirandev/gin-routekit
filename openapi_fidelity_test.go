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

type deprecatedNested struct {
	Value string `json:"value"`
}

type deprecatedProvider string

func (deprecatedProvider) OpenAPISchema() OpenAPISchema {
	return OpenAPISchema{Type: "string", Deprecated: true}
}

type deprecatedFieldDTO struct {
	Current        string             `json:"current"`
	Legacy         string             `json:"legacy" routekit:"deprecated"`
	RequiredLegacy string             `json:"requiredLegacy" binding:"required" routekit:"deprecated"`
	Numbers        []string           `json:"numbers,omitempty" routekit:"deprecated"`
	Tags           []string           `json:"tags" routekit:"deprecated"`
	OptionalName   *string            `json:"optionalName,omitempty" routekit:"deprecated"`
	LegacyNested   deprecatedNested   `json:"legacyNested" routekit:"deprecated"`
	PlainNested    deprecatedNested   `json:"plainNested"`
	LegacyPtr      *deprecatedNested  `json:"legacyPtr,omitempty" routekit:"deprecated"`
	LegacyProvider deprecatedProvider `json:"legacyProvider,omitempty"`
}

func TestPropertyDeprecatedTagMarksSchema(t *testing.T) {
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{{
		Method: http.MethodPost, RelativePath: "/value", Path: "/value", Definition: "value",
		Contract: &Contract{RequestBody: &DocBody{Required: true, Schema: SchemaOf[deprecatedFieldDTO]()}, Responses: []DocResponse{{Status: 200, Description: "OK", Schema: SchemaOf[deprecatedFieldDTO]()}}},
	}}}}
	document, err := BuildOpenAPI(routes, fidelityConfig())
	if err != nil {
		t.Fatal(err)
	}
	if report, err := ValidateOpenAPI(routes, fidelityConfig()); err != nil {
		t.Fatalf("unexpected diagnostics for deprecated schemas: report=%#v error=%v", report, err)
	}

	operation := document.Paths["/api/value"].Post
	requestRoot := operation.RequestBody.Content["application/json"].Schema
	if requestRoot == nil || len(requestRoot.AnyOf) != 2 || requestRoot.AnyOf[1].Type != "null" {
		t.Fatalf("request root = %#v", requestRoot)
	}
	request := referencedSchema(t, document, &requestRoot.AnyOf[0])
	response := referencedSchema(t, document, operation.Responses["200"].Content["application/json"].Schema)

	if request.Properties["current"].Deprecated || response.Properties["current"].Deprecated {
		t.Fatalf("untagged property must not be deprecated: request=%#v response=%#v", request.Properties["current"], response.Properties["current"])
	}
	if !request.Properties["legacy"].Deprecated || !response.Properties["legacy"].Deprecated {
		t.Fatalf("tagged property must be deprecated in both directions: request=%#v response=%#v", request.Properties["legacy"], response.Properties["legacy"])
	}

	// binding:"required" keeps the field unwrapped in both directions: the flag
	// must land directly on the property, with no anyOf involved.
	requiredLegacy := request.Properties["requiredLegacy"]
	if len(requiredLegacy.AnyOf) != 0 || !requiredLegacy.Deprecated {
		t.Fatalf("required property must be deprecated directly, without anyOf: %#v", requiredLegacy)
	}
	if responseRequiredLegacy := response.Properties["requiredLegacy"]; len(responseRequiredLegacy.AnyOf) != 0 || !responseRequiredLegacy.Deprecated {
		t.Fatalf("required property must be deprecated directly in response too: %#v", responseRequiredLegacy)
	}

	// Optional slice in request: nullable wrapper. deprecated must land on both
	// the wrapper and the array branch (the exact bug being fixed).
	numbers := request.Properties["numbers"]
	if len(numbers.AnyOf) != 2 || numbers.AnyOf[1].Type != "null" || numbers.AnyOf[0].Type != "array" {
		t.Fatalf("numbers request = %#v", numbers)
	}
	if !numbers.Deprecated || !numbers.AnyOf[0].Deprecated {
		t.Fatalf("optional array must be deprecated on both the wrapper and the array branch: %#v", numbers)
	}
	// omitempty means the response field is never serialized as null, so it
	// stays unwrapped.
	if responseNumbers := response.Properties["numbers"]; len(responseNumbers.AnyOf) != 0 || responseNumbers.Type != "array" || !responseNumbers.Deprecated {
		t.Fatalf("numbers response (omitempty, never null) = %#v", responseNumbers)
	}

	// Without omitempty, the slice is nullable (and wrapped) in both directions.
	for label, tags := range map[string]OpenAPISchema{"request": request.Properties["tags"], "response": response.Properties["tags"]} {
		if len(tags.AnyOf) != 2 || tags.AnyOf[1].Type != "null" || tags.AnyOf[0].Type != "array" {
			t.Fatalf("tags %s = %#v", label, tags)
		}
		if !tags.Deprecated || !tags.AnyOf[0].Deprecated {
			t.Fatalf("tags %s must be deprecated on wrapper and array branch: %#v", label, tags)
		}
	}

	optionalName := request.Properties["optionalName"]
	if len(optionalName.AnyOf) != 2 || optionalName.AnyOf[1].Type != "null" || optionalName.AnyOf[0].Type != "string" {
		t.Fatalf("optionalName request = %#v", optionalName)
	}
	if !optionalName.Deprecated || !optionalName.AnyOf[0].Deprecated {
		t.Fatalf("optional pointer to primitive must be deprecated on wrapper and value branch: %#v", optionalName)
	}

	nested := request.Properties["legacyNested"]
	if len(nested.AnyOf) != 2 || nested.AnyOf[0].Ref == "" || !nested.Deprecated || !nested.AnyOf[0].Deprecated {
		t.Fatalf("optional $ref property must keep deprecated on wrapper and $ref branch: %#v", nested)
	}
	nestedComponentName := strings.TrimPrefix(nested.AnyOf[0].Ref, "#/components/schemas/")

	// A second, untagged field of the same struct type must never be
	// contaminated, and both fields must still share one component.
	plain := request.Properties["plainNested"]
	if len(plain.AnyOf) != 2 || plain.AnyOf[0].Ref == "" || plain.Deprecated || plain.AnyOf[0].Deprecated {
		t.Fatalf("untagged $ref property must never be deprecated: %#v", plain)
	}
	if plainComponentName := strings.TrimPrefix(plain.AnyOf[0].Ref, "#/components/schemas/"); plainComponentName != nestedComponentName {
		t.Fatalf("tagged and untagged fields of the same type must share one component: legacyNested=%s plainNested=%s", nestedComponentName, plainComponentName)
	}
	if component := document.Components.Schemas[nestedComponentName]; component == nil || component.Deprecated {
		t.Fatalf("shared component must never carry deprecated: %#v", component)
	}

	nestedPtr := request.Properties["legacyPtr"]
	if len(nestedPtr.AnyOf) != 2 || nestedPtr.AnyOf[0].Ref == "" || !nestedPtr.Deprecated || !nestedPtr.AnyOf[0].Deprecated {
		t.Fatalf("nullable $ref property must keep deprecated on wrapper and $ref branch: %#v", nestedPtr)
	}

	responseNested := response.Properties["legacyNested"]
	if responseNested.Ref == "" || !responseNested.Deprecated {
		t.Fatalf("response $ref property must expose deprecated as a $ref sibling: %#v", responseNested)
	}

	// A provider-declared Deprecated:true must keep working on an optional
	// field, without relying on the routekit tag at all. Provider-backed
	// types short-circuit schemaForType before the nullable-wrap logic (see
	// schemaForType, provider branch), so the field stays a plain $ref and
	// the flag already lives inside the referenced component.
	legacyProvider := request.Properties["legacyProvider"]
	if legacyProvider.Ref == "" || len(legacyProvider.AnyOf) != 0 {
		t.Fatalf("provider-backed property must stay a plain $ref: %#v", legacyProvider)
	}
	providerComponent := referencedSchema(t, document, &legacyProvider)
	if !providerComponent.Deprecated {
		t.Fatalf("provider-declared deprecated must survive without the routekit tag: %#v", providerComponent)
	}
}

func TestAddResponseInlinesDeprecatedRefSibling(t *testing.T) {
	operation := &OpenAPIOperation{}
	operation.AddResponse(200, "OK", deprecatedFieldDTO{})
	standalone := operation.Responses["200"].Content["application/json"].Schema
	if standalone == nil || standalone.Ref != "" || standalone.Type != "object" {
		t.Fatalf("standalone AddResponse schema = %#v", standalone)
	}
	legacyNested := standalone.Properties["legacyNested"]
	if legacyNested.Ref != "" || legacyNested.Type != "object" || !legacyNested.Deprecated {
		t.Fatalf("inlining a tagged $ref must keep deprecated: %#v", legacyNested)
	}
	plainNested := standalone.Properties["plainNested"]
	if plainNested.Ref != "" || plainNested.Type != "object" || plainNested.Deprecated {
		t.Fatalf("inlining an untagged $ref must not invent deprecated: %#v", plainNested)
	}
}

func TestPropertyDeprecatedOmittedByDefault(t *testing.T) {
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{{
		Method: http.MethodGet, RelativePath: "/value", Path: "/value", Definition: "value",
		Doc: &DocConfig{Responses: []DocResponse{{Status: 200, Description: "OK", Schema: SchemaOf[fidelityDTO]()}}},
	}}}}
	document, err := BuildOpenAPI(routes, fidelityConfig())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := MarshalOpenAPI(document)
	if err != nil {
		t.Fatal(err)
	}
	assertNotContains(t, string(payload), `"deprecated"`)
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

func TestNamedExamplesSerialization(t *testing.T) {
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{{
		Method: http.MethodPost, RelativePath: "/notify", Path: "/notify", Definition: "notify",
		Contract: &Contract{
			RequestBody: &DocBody{Required: true, Schema: SchemaOf[fidelityDTO](), Examples: []NamedExample{
				{Name: "whatsapp", Summary: "WhatsApp", Value: map[string]any{"required": "whatsapp"}},
				{Name: "email", Summary: "E-mail", Value: map[string]any{"required": "email"}},
			}},
			Responses: []DocResponse{{Status: 200, Description: "OK", Schema: SchemaOf[fidelityDTO](),
				Examples: []NamedExample{{Name: "external", ExternalValue: "https://example.test/response.json"}}}},
		},
	}}}}
	document, err := BuildOpenAPI(routes, fidelityConfig())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := MarshalOpenAPI(document)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, expected := range []string{
		`"examples": {`,
		`"summary": "E-mail"`,
		`"summary": "WhatsApp"`,
		`"required": "email"`,
		`"required": "whatsapp"`,
		`"externalValue": "https://example.test/response.json"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("payload missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, `"example"`) {
		t.Fatalf("singular example must be omitted when named examples exist:\n%s", text)
	}
	if strings.Index(text, `"email"`) > strings.Index(text, `"whatsapp"`) {
		t.Fatal("named examples must serialize in alphabetical key order")
	}
	again, err := MarshalOpenAPI(document)
	if err != nil || string(again) != text {
		t.Fatalf("marshal is not deterministic: %v", err)
	}
}

func TestExampleOptionsConflictIsDiagnostic(t *testing.T) {
	requestConflict := JSONRequestContractOf[fidelityDTO, fidelityDTO](http.StatusOK, "OK",
		WithRequestExample(map[string]any{"required": "singular"}),
		WithRequestExamples(NamedExample{Name: "whatsapp", Value: map[string]any{"required": "named"}}))
	responseConflict := JSONResponseContractOf[fidelityDTO](http.StatusOK, "OK",
		WithResponseExample(map[string]any{"required": "singular"}),
		WithResponseExamples(NamedExample{Name: "success", Value: map[string]any{"required": "named"}}))
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{
		{Method: http.MethodPost, RelativePath: "/request", Path: "/request", Definition: "request", Contract: &requestConflict},
		{Method: http.MethodGet, RelativePath: "/response", Path: "/response", Definition: "response", Contract: &responseConflict},
	}}}
	report, err := ValidateOpenAPI(routes, fidelityConfig())
	if err == nil || !report.HasErrors() {
		t.Fatalf("report = %#v, error = %v", report, err)
	}
	count := 0
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == "contract.option.incoherent" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected two contract.option.incoherent diagnostics, got %d in %#v", count, report.Diagnostics)
	}
	if _, err := BuildOpenAPI(routes, fidelityConfig()); err == nil {
		t.Fatal("conflicting example options must block the build")
	}
}

func TestInvalidNamedExamplesAreDiagnosed(t *testing.T) {
	routes := []Route{{Path: "/api", Group: "api", Handlers: []Handler{
		{Method: http.MethodPost, RelativePath: "/body", Path: "/body", Definition: "body", Doc: &DocConfig{
			RequestBody: &DocBody{Required: true, Schema: SchemaOf[fidelityDTO](), Examples: []NamedExample{
				{Name: "", Value: "missing name"},
				{Name: "both", Value: "value", ExternalValue: "https://example.test/both.json"},
			}},
			Responses: []DocResponse{{Status: 200, Description: "OK"}},
		}},
		{Method: http.MethodGet, RelativePath: "/response", Path: "/response", Definition: "response", Doc: &DocConfig{
			Responses: []DocResponse{{Status: 200, Description: "OK", Schema: SchemaOf[fidelityDTO](),
				Examples: []NamedExample{{Name: "neither"}}}},
		}},
	}}}
	report, err := ValidateOpenAPI(routes, fidelityConfig())
	if err == nil || !hasDiagnosticCode(report, "media_type.example.invalid") {
		t.Fatalf("report = %#v, error = %v", report, err)
	}
	messages := ""
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == "media_type.example.invalid" {
			messages += diagnostic.Message + "\n"
		}
	}
	for _, expected := range []string{
		"example name must not be empty",
		`example "both" must define exactly one of value or externalValue`,
		`example "neither" must define value or externalValue`,
	} {
		if !strings.Contains(messages, expected) {
			t.Fatalf("missing %q in %#v", expected, messages)
		}
	}
	if _, err := BuildOpenAPI(routes, fidelityConfig()); err == nil {
		t.Fatal("invalid named examples must block the build")
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
