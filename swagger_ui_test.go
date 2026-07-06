package routekit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterSwaggerUIDefaults(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterSwaggerUI(engine, SwaggerUIConfig{}); err != nil {
		t.Fatalf("RegisterSwaggerUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html; charset=utf-8") {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", contentType)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cacheControl)
	}

	body := rec.Body.String()
	assertContains(t, body, "SwaggerUIBundle")
	assertContains(t, body, "<title>API Docs</title>")
	assertContains(t, body, `url: "/openapi.json"`)
	assertContains(t, body, defaultSwaggerCDN+"/swagger-ui.css")
}

func TestRegisterSwaggerUICustomConfig(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterSwaggerUI(engine, SwaggerUIConfig{
		Path:       "/swagger",
		OpenAPIURL: "/api-docs/openapi.json",
		Title:      "Custom API Docs",
		CDNBaseURL: "https://cdn.example.com/swagger-ui",
	}); err != nil {
		t.Fatalf("RegisterSwaggerUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, "<title>Custom API Docs</title>")
	assertContains(t, body, `url: "/api-docs/openapi.json"`)
	assertContains(t, body, "https://cdn.example.com/swagger-ui/swagger-ui-bundle.js")
	assertContains(t, body, "https://cdn.example.com/swagger-ui/swagger-ui-standalone-preset.js")
}

func TestRegisterSwaggerUIValidation(t *testing.T) {
	tests := []struct {
		name   string
		config SwaggerUIConfig
	}{
		{
			name: "path without slash",
			config: SwaggerUIConfig{
				Path: "docs",
			},
		},
		{
			name: "path with parameter",
			config: SwaggerUIConfig{
				Path: "/docs/:name",
			},
		},
		{
			name: "path with wildcard",
			config: SwaggerUIConfig{
				Path: "/docs/*path",
			},
		},
		{
			name: "invalid openapi url",
			config: SwaggerUIConfig{
				OpenAPIURL: "openapi.json",
			},
		},
		{
			name: "invalid cdn base url",
			config: SwaggerUIConfig{
				CDNBaseURL: "/assets/swagger-ui",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newEngine()
			ar := NewAppRouter(nil, nil)
			if err := ar.RegisterSwaggerUI(engine, tt.config); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestRegisterSwaggerUIRequiresEngine(t *testing.T) {
	ar := NewAppRouter(nil, nil)
	if err := ar.RegisterSwaggerUI(nil, SwaggerUIConfig{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterSwaggerUIDoesNotRequireOpenAPIRegistration(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterSwaggerUI(engine, SwaggerUIConfig{}); err != nil {
		t.Fatalf("RegisterSwaggerUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRenderSwaggerUIHTMLEscapesTitle(t *testing.T) {
	html, err := renderSwaggerUIHTML(normalizeSwaggerUIConfig(SwaggerUIConfig{
		Title: "Docs <script>alert(1)</script>",
	}))
	if err != nil {
		t.Fatalf("renderSwaggerUIHTML: %v", err)
	}

	assertContains(t, html, "Docs &lt;script&gt;alert(1)&lt;/script&gt;")
	if strings.Contains(html, "<title>Docs <script>alert(1)</script></title>") {
		t.Fatal("title was not escaped")
	}
}

func TestRenderSwaggerUIHTMLSerializesOpenAPIURLSafely(t *testing.T) {
	openAPIURL := `/openapi.json?name="</script><script>alert(1)</script>`
	html, err := renderSwaggerUIHTML(normalizeSwaggerUIConfig(SwaggerUIConfig{
		OpenAPIURL: openAPIURL,
	}))
	if err != nil {
		t.Fatalf("renderSwaggerUIHTML: %v", err)
	}

	assertContains(t, html, `url: "/openapi.json?name=\"\u003c/script\u003e\u003cscript\u003ealert(1)\u003c/script\u003e"`)
	if strings.Contains(html, openAPIURL) {
		t.Fatal("OpenAPIURL was rendered without JSON escaping")
	}
}

func assertContains(t *testing.T, value string, substring string) {
	t.Helper()
	if !strings.Contains(value, substring) {
		t.Fatalf("expected %q to contain %q", value, substring)
	}
}
