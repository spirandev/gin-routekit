package routekit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func buildDocPayload(t *testing.T, ar *AppRouter, config OpenAPIConfig) []byte {
	t.Helper()
	doc := buildDoc(t, ar, config)
	payload, err := MarshalOpenAPI(doc)
	if err != nil {
		t.Fatalf("MarshalOpenAPI: %v", err)
	}
	return payload
}

func TestDeprecatedEndpointFluent(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/old-resource", okHandler, "old resource", 1).Document().Deprecated()

	ar := newTestEngine(t, group)
	payload := buildDocPayload(t, ar, baseConfig())
	assertContains(t, string(payload), `"deprecated": true`)

	if !ar.routes[0].Handlers[0].Doc.Deprecated {
		t.Fatal("expected handler Doc.Deprecated to be true")
	}
}

func TestDeprecatedGroupDefaultsApply(t *testing.T) {
	engine := newEngine()
	group := groupRegistrar{group: NewRouterGroup(engine, "/api", WithDocumentation(), WithDeprecatedEndpoints()), name: "api", appID: 123}
	group.group.GET("/resource", okHandler, "resource", 1)

	ar := newTestEngine(t, group)
	payload := buildDocPayload(t, ar, baseConfig())
	assertContains(t, string(payload), `"deprecated": true`)
}

func TestDeprecatedORSemantics(t *testing.T) {
	scenarios := []struct {
		name           string
		groupOptions   []GroupOption
		endpoint       bool
		wantDeprecated bool
	}{
		{name: "none"},
		{name: "only group", groupOptions: []GroupOption{WithDeprecatedEndpoints()}, wantDeprecated: true},
		{name: "only endpoint", endpoint: true, wantDeprecated: true},
		{name: "both", groupOptions: []GroupOption{WithDeprecatedEndpoints()}, endpoint: true, wantDeprecated: true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			engine := newEngine()
			group := groupRegistrar{group: NewRouterGroup(engine, "/api", scenario.groupOptions...), name: "api", appID: 123}
			route := group.group.GET("/resource", okHandler, "resource", 1).Document()
			if scenario.endpoint {
				route = route.Deprecated()
			}

			ar := newTestEngine(t, group)
			doc := buildDoc(t, ar, baseConfig())
			operation := doc.Paths["/api/resource"].Get
			if operation == nil {
				t.Fatal("missing GET operation")
			}
			if operation.Deprecated != scenario.wantDeprecated {
				t.Errorf("Deprecated = %v, want %v", operation.Deprecated, scenario.wantDeprecated)
			}
		})
	}
}

func TestNotDeprecatedOmitted(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/resource", okHandler, "resource", 1).Document()

	ar := newTestEngine(t, group)
	payload := buildDocPayload(t, ar, baseConfig())
	assertNotContains(t, string(payload), `"deprecated"`)
}

func TestRoutesClonePreservesDeprecated(t *testing.T) {
	engine := newEngine()
	group := groupRegistrar{group: NewRouterGroup(engine, "/api", WithDeprecatedEndpoints()), name: "api", appID: 123}
	group.group.GET("/resource", okHandler, "resource", 1).Document().Deprecated()

	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	routes := ar.Routes()
	if !routes[0].Handlers[0].Doc.Deprecated {
		t.Error("cloneDocConfig did not preserve Deprecated")
	}
	if !routes[0].DocumentationDefaults.Deprecated {
		t.Error("cloneDocumentationDefaults did not preserve Deprecated")
	}

	routes[0].Handlers[0].Doc.Deprecated = false
	routes[0].DocumentationDefaults.Deprecated = false
	second := ar.Routes()
	if !second[0].Handlers[0].Doc.Deprecated || !second[0].DocumentationDefaults.Deprecated {
		t.Error("Routes() did not return a defensive copy of Deprecated flags")
	}
}

func TestDeprecatedVisibleInServedJSON(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "api", 123)
	group.group.GET("/old-resource", okHandler, "old resource", 1).Document().Deprecated()

	ar := newTestEngine(t, group)
	engine2 := newEngine()
	if err := ar.RegisterRoutes(engine2); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
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
	assertContains(t, rec.Body.String(), `"deprecated": true`)

	var doc OpenAPIDocument
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal served payload: %v", err)
	}
	operation := doc.Paths["/api/old-resource"].Get
	if operation == nil || !operation.Deprecated {
		t.Fatal("expected served operation to be deprecated")
	}
}
