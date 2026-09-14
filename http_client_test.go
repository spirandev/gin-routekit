package routekit

import (
	"errors"
	"strings"
	"testing"
)

func TestMarshalHTTPClientValidation(t *testing.T) {
	if _, err := MarshalHTTPClient(nil, HTTPClientConfig{BaseURL: "https://api.example.com"}); err == nil || err.Error() != "OpenAPI document must not be nil" {
		t.Fatalf("MarshalHTTPClient nil error = %v", err)
	}

	document := httpClientTestDocument()
	document.Servers = []OpenAPIServer{{URL: "https://server.example.com"}}
	payload, err := MarshalHTTPClient(document, HTTPClientConfig{BaseURL: "https://configured.example.com", BaseURLVariable: "apiBase"})
	if err != nil {
		t.Fatalf("MarshalHTTPClient explicit base URL: %v", err)
	}
	if !strings.Contains(string(payload), "@apiBase = https://configured.example.com") || !strings.Contains(string(payload), "GET {{apiBase}}/ping") {
		t.Fatalf("payload did not use configured base URL and variable:\n%s", payload)
	}

	missingServer := httpClientTestDocument()
	missingServer.Servers = []OpenAPIServer{{URL: "/api"}}
	if _, err := MarshalHTTPClient(missingServer, HTTPClientConfig{}); err == nil {
		t.Fatal("expected missing absolute base URL error")
	}
	if _, err := MarshalHTTPClient(document, HTTPClientConfig{BaseURL: "ftp://api.example.com"}); err == nil {
		t.Fatal("expected invalid base URL error")
	}
	if _, err := MarshalHTTPClient(document, HTTPClientConfig{BaseURL: "https://api.example.com", BaseURLVariable: "bad-name"}); err == nil {
		t.Fatal("expected invalid base URL variable error")
	}
}

func TestMarshalHTTPClientServerFallback(t *testing.T) {
	document := httpClientTestDocument()
	document.Servers = []OpenAPIServer{{URL: "/api"}, {URL: "https://server.example.com/v1/"}}
	payload, err := MarshalHTTPClient(document, HTTPClientConfig{})
	if err != nil {
		t.Fatalf("MarshalHTTPClient fallback: %v", err)
	}
	if !strings.Contains(string(payload), "@baseUrl = https://server.example.com/v1") {
		t.Fatalf("payload did not use first absolute server:\n%s", payload)
	}
}

func TestMarshalHTTPClientDeterministicPathsAndMethods(t *testing.T) {
	document := &OpenAPIDocument{
		Servers: []OpenAPIServer{{URL: "https://api.example.com"}},
		Paths: OpenAPIPaths{
			"/z": {Post: httpClientTestOperation("post_z", "Post Z"), Get: httpClientTestOperation("get_z", "Get Z")},
			"/a": {Delete: httpClientTestOperation("delete_a", "Delete A"), Get: httpClientTestOperation("get_a", "Get A")},
		},
	}

	payload, err := MarshalHTTPClient(document, HTTPClientConfig{})
	if err != nil {
		t.Fatalf("MarshalHTTPClient: %v", err)
	}
	text := string(payload)
	assertOrdered(t, text, "GET {{baseUrl}}/a", "DELETE {{baseUrl}}/a", "GET {{baseUrl}}/z", "POST {{baseUrl}}/z")
	for _, expected := range []string{"### Get A", "# @name get_a", "### Delete A", "# @name delete_a"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("payload missing %q:\n%s", expected, text)
		}
	}
}

func TestMarshalHTTPClientParameters(t *testing.T) {
	document := &OpenAPIDocument{
		Servers: []OpenAPIServer{{URL: "https://api.example.com"}},
		Paths: OpenAPIPaths{
			"/users/{id}": {Get: &OpenAPIOperation{
				Summary:     "Get user",
				OperationID: "get_user",
				Responses:   OpenAPIResponses{"200": {Description: "OK"}},
				Parameters: []OpenAPIParameter{
					{Name: "id", In: "path", Example: "u 1"},
					{Name: "search", In: "query", Example: "a b"},
					{Name: "page", In: "query", Schema: &OpenAPISchema{Type: "integer"}},
					{Name: "X-Request-ID", In: "header"},
					{Name: "X-Trace", In: "header", Example: "a\nb"},
				},
			}},
		},
	}

	payload, err := MarshalHTTPClient(document, HTTPClientConfig{})
	if err != nil {
		t.Fatalf("MarshalHTTPClient: %v", err)
	}
	text := string(payload)
	for _, expected := range []string{
		"GET {{baseUrl}}/users/u%201?page={{queryPage}}&search=a+b",
		"X-Request-ID: {{headerXRequestID}}",
		`X-Trace: "a\nb"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("payload missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "X-Trace: a\n") {
		t.Fatalf("header example contains raw newline:\n%s", text)
	}
}

func TestMarshalHTTPClientJSONBodyExamplesAndSchemaPlaceholders(t *testing.T) {
	document := &OpenAPIDocument{
		Servers: []OpenAPIServer{{URL: "https://api.example.com"}},
		Components: &OpenAPIComponents{Schemas: map[string]*OpenAPISchema{
			"CreateUser": {Type: "object", Properties: map[string]OpenAPISchema{
				"tags":   {Type: "array", Items: &OpenAPISchema{Type: "string"}},
				"name":   {AnyOf: []OpenAPISchema{{Type: "null"}, {Type: "string"}}},
				"active": {Type: "boolean"},
				"count":  {Type: "integer"},
			}},
		}},
		Paths: OpenAPIPaths{
			"/example": {Post: httpClientBodyOperation("post_example", OpenAPIMediaType{Example: map[string]any{"message": "line\n<tag>"}})},
			"/schema":  {Post: httpClientBodyOperation("post_schema", OpenAPIMediaType{Schema: &OpenAPISchema{Ref: "#/components/schemas/CreateUser"}})},
		},
	}

	payload, err := MarshalHTTPClient(document, HTTPClientConfig{})
	if err != nil {
		t.Fatalf("MarshalHTTPClient: %v", err)
	}
	text := string(payload)
	for _, expected := range []string{
		"Content-Type: application/json",
		`"message": "line\n\u003ctag\u003e"`,
		`"active": false`,
		`"count": 0`,
		`"name": ""`,
		`"tags": [`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("payload missing %q:\n%s", expected, text)
		}
	}
	assertOrdered(t, text, `"active": false`, `"count": 0`, `"name": ""`, `"tags": [`)
}

func TestMarshalHTTPClientNamedExamplesFallback(t *testing.T) {
	document := &OpenAPIDocument{
		Servers: []OpenAPIServer{{URL: "https://api.example.com"}},
		Paths: OpenAPIPaths{
			"/example": {Post: httpClientBodyOperation("post_example", OpenAPIMediaType{Examples: map[string]OpenAPIExample{
				"whatsapp": {Summary: "WhatsApp", Value: map[string]any{"channel": "whatsapp"}},
				"email":    {Summary: "E-mail", Value: map[string]any{"channel": "email"}},
				"remote":   {ExternalValue: "https://example.test/payload.json"},
			}})},
			"/plain": {Post: httpClientBodyOperationWithContent("post_plain", map[string]OpenAPIMediaType{"text/plain": {Examples: map[string]OpenAPIExample{
				"greeting": {Value: "hello"},
			}}})},
		},
	}

	payload, err := MarshalHTTPClient(document, HTTPClientConfig{})
	if err != nil {
		t.Fatalf("MarshalHTTPClient: %v", err)
	}
	text := string(payload)
	if !strings.Contains(text, `"channel": "email"`) {
		t.Fatalf("payload did not use the alphabetically first named example:\n%s", text)
	}
	if strings.Contains(text, "whatsapp") || strings.Contains(text, "remote") {
		t.Fatalf("payload must not render other named examples:\n%s", text)
	}
	if !strings.Contains(text, "Content-Type: text/plain\n\nhello") {
		t.Fatalf("payload missing text/plain named example:\n%s", text)
	}
}

func TestMarshalHTTPClientNonJSONBodies(t *testing.T) {
	document := &OpenAPIDocument{
		Servers: []OpenAPIServer{{URL: "https://api.example.com"}},
		Paths: OpenAPIPaths{
			"/xml":   {Post: httpClientBodyOperationWithContent("post_xml", map[string]OpenAPIMediaType{"application/xml": {Example: "<user>1</user>"}})},
			"/plain": {Post: httpClientBodyOperationWithContent("post_plain", map[string]OpenAPIMediaType{"text/plain": {}})},
		},
	}

	payload, err := MarshalHTTPClient(document, HTTPClientConfig{})
	if err != nil {
		t.Fatalf("MarshalHTTPClient: %v", err)
	}
	text := string(payload)
	for _, expected := range []string{
		"Content-Type: application/xml\n\n<user>1</user>",
		"Content-Type: text/plain\n\n# No example available for text/plain body",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("payload missing %q:\n%s", expected, text)
		}
	}
}

func TestMarshalHTTPClientSecurity(t *testing.T) {
	document := &OpenAPIDocument{
		Servers: []OpenAPIServer{{URL: "https://api.example.com"}},
		Components: &OpenAPIComponents{SecuritySchemes: map[string]*OpenAPISecurityScheme{
			"BearerAuth": BearerSecurityScheme("jwt"),
			"HeaderKey":  APIKeyHeaderSecurityScheme("X-API-Key", "key"),
			"QueryKey":   APIKeyQuerySecurityScheme("api_key", "key"),
			"BasicAuth":  {Type: "http", Scheme: "basic"},
			"OAuth":      {Type: "oauth2"},
			"Other":      APIKeyHeaderSecurityScheme("X-Other", "other"),
		}},
		Paths: OpenAPIPaths{"/secure": {Get: &OpenAPIOperation{
			Summary:     "Secure",
			OperationID: "get_secure",
			Responses:   OpenAPIResponses{"200": {Description: "OK"}},
			Security: []OpenAPISecurityRequirement{
				{"QueryKey": nil, "HeaderKey": nil, "BearerAuth": nil, "BasicAuth": nil, "OAuth": nil},
				{"Other": nil},
			},
		}}},
	}

	payload, err := MarshalHTTPClient(document, HTTPClientConfig{})
	if err != nil {
		t.Fatalf("MarshalHTTPClient: %v", err)
	}
	text := string(payload)
	for _, expected := range []string{
		"GET {{baseUrl}}/secure?api_key={{QueryKeyApiKey}}",
		"Authorization: Bearer {{BearerAuthToken}}",
		"X-API-Key: {{HeaderKeyApiKey}}",
		"# Security scheme BasicAuth uses unsupported HTTP scheme basic",
		"# Security scheme OAuth is not supported by HTTP client generation",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("payload missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "X-Other") {
		t.Fatalf("security alternative was rendered, expected only first requirement:\n%s", text)
	}
}

func TestBuildHTTPClientRequiresRoutesAndDelegates(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1).Document().Response(200, "OK", nil)
	ar := newTestEngine(t, group)

	config := baseConfig()
	config.Servers = []OpenAPIServer{{URL: "https://api.example.com"}}
	if _, err := ar.BuildHTTPClient(config, HTTPClientConfig{}); !errors.Is(err, errRegisterRoutesRequired) {
		t.Fatalf("BuildHTTPClient before RegisterRoutes error = %v", err)
	}
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	payload, err := ar.BuildHTTPClient(config, HTTPClientConfig{})
	if err != nil {
		t.Fatalf("BuildHTTPClient: %v", err)
	}
	document, err := ar.BuildOpenAPI(config)
	if err != nil {
		t.Fatalf("BuildOpenAPI: %v", err)
	}
	expected, err := MarshalHTTPClient(document, HTTPClientConfig{})
	if err != nil {
		t.Fatalf("MarshalHTTPClient: %v", err)
	}
	if string(payload) != string(expected) {
		t.Fatalf("BuildHTTPClient differs from BuildOpenAPI+MarshalHTTPClient\nactual:\n%s\nexpected:\n%s", payload, expected)
	}
}

func httpClientTestDocument() *OpenAPIDocument {
	return &OpenAPIDocument{Paths: OpenAPIPaths{"/ping": {Get: httpClientTestOperation("get_ping", "Ping")}}}
}

func httpClientTestOperation(operationID, summary string) *OpenAPIOperation {
	return &OpenAPIOperation{OperationID: operationID, Summary: summary, Responses: OpenAPIResponses{"200": {Description: "OK"}}}
}

func httpClientBodyOperation(operationID string, media OpenAPIMediaType) *OpenAPIOperation {
	return httpClientBodyOperationWithContent(operationID, map[string]OpenAPIMediaType{"application/json": media})
}

func httpClientBodyOperationWithContent(operationID string, content map[string]OpenAPIMediaType) *OpenAPIOperation {
	operation := httpClientTestOperation(operationID, operationID)
	operation.RequestBody = &OpenAPIRequestBody{Content: content}
	return operation
}

func assertOrdered(t *testing.T, text string, values ...string) {
	t.Helper()
	previous := -1
	for _, value := range values {
		index := strings.Index(text, value)
		if index < 0 {
			t.Fatalf("missing %q in:\n%s", value, text)
		}
		if index <= previous {
			t.Fatalf("%q is not ordered after previous value in:\n%s", value, text)
		}
		previous = index
	}
}
