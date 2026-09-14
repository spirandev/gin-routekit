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
	assertNotContains(t, body, "        urls:")
	assertNotContains(t, body, `"urls.primaryName"`)
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
	assertNotContains(t, body, "        urls:")
	assertNotContains(t, body, `"urls.primaryName"`)
	assertContains(t, body, "https://cdn.example.com/swagger-ui/swagger-ui-bundle.js")
	assertContains(t, body, "https://cdn.example.com/swagger-ui/swagger-ui-standalone-preset.js")
}

func TestRegisterSwaggerUIMultipleSpecs(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterSwaggerUI(engine, SwaggerUIConfig{
		Path:       "/docs",
		OpenAPIURL: "openapi.json",
		OpenAPIURLs: []SwaggerUISpec{
			{Name: "Core API", URL: "/openapi.json"},
			{Name: "Attendance System", URL: "/docs/spec/attendance-system"},
			{Name: "EvoBridge", URL: "https://example.com/openapi.json"},
		},
		PrimaryOpenAPIName: "Core API",
	}); err != nil {
		t.Fatalf("RegisterSwaggerUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, `urls: [{"name":"Core API","url":"/openapi.json"},{"name":"Attendance System","url":"/docs/spec/attendance-system"},{"name":"EvoBridge","url":"https://example.com/openapi.json"}]`)
	assertContains(t, body, `"urls.primaryName": "Core API"`)
	assertNotContains(t, body, "        url:")
}

func TestRegisterSwaggerUIMultipleSpecsDefaultsPrimary(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterSwaggerUI(engine, SwaggerUIConfig{
		OpenAPIURLs: []SwaggerUISpec{
			{Name: "Core API", URL: "/openapi.json"},
			{Name: "Admin API", URL: "/admin/openapi.json"},
		},
	}); err != nil {
		t.Fatalf("RegisterSwaggerUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertContains(t, rec.Body.String(), `"urls.primaryName": "Core API"`)
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

func TestRegisterSwaggerUIMultipleSpecsValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  SwaggerUIConfig
		wantErr string
	}{
		{
			name: "primary missing",
			config: SwaggerUIConfig{
				OpenAPIURLs: []SwaggerUISpec{
					{Name: "Core API", URL: "/openapi.json"},
				},
				PrimaryOpenAPIName: "Missing API",
			},
			wantErr: "swagger UI PrimaryOpenAPIName must match an OpenAPIURLs name",
		},
		{
			name: "empty name",
			config: SwaggerUIConfig{
				OpenAPIURLs: []SwaggerUISpec{
					{Name: " ", URL: "/openapi.json"},
				},
			},
			wantErr: "swagger UI OpenAPIURLs name is required",
		},
		{
			name: "duplicate name",
			config: SwaggerUIConfig{
				OpenAPIURLs: []SwaggerUISpec{
					{Name: "Core API", URL: "/openapi.json"},
					{Name: " Core API ", URL: "/admin/openapi.json"},
				},
			},
			wantErr: "swagger UI OpenAPIURLs name must be unique",
		},
		{
			name: "invalid spec url",
			config: SwaggerUIConfig{
				OpenAPIURLs: []SwaggerUISpec{
					{Name: "Core API", URL: "openapi.json"},
				},
			},
			wantErr: "swagger UI OpenAPIURLs URL must be an absolute path or URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newEngine()
			ar := NewAppRouter(nil, nil)
			err := ar.RegisterSwaggerUI(engine, tt.config)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
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

func TestRenderSwaggerUIHTMLSerializesOpenAPIURLsSafely(t *testing.T) {
	openAPIName := `Core "</script><script>alert(1)</script>`
	openAPIURL := `/openapi.json?name="</script><script>alert(1)</script>`
	html, err := renderSwaggerUIHTML(normalizeSwaggerUIConfig(SwaggerUIConfig{
		OpenAPIURLs: []SwaggerUISpec{
			{Name: openAPIName, URL: openAPIURL},
		},
	}))
	if err != nil {
		t.Fatalf("renderSwaggerUIHTML: %v", err)
	}

	assertContains(t, html, `urls: [{"name":"Core \"\u003c/script\u003e\u003cscript\u003ealert(1)\u003c/script\u003e","url":"/openapi.json?name=\"\u003c/script\u003e\u003cscript\u003ealert(1)\u003c/script\u003e"}]`)
	assertContains(t, html, `"urls.primaryName": "Core \"\u003c/script\u003e\u003cscript\u003ealert(1)\u003c/script\u003e"`)
	if strings.Contains(html, openAPIName) {
		t.Fatal("OpenAPIURLs name was rendered without JSON escaping")
	}
	if strings.Contains(html, openAPIURL) {
		t.Fatal("OpenAPIURLs URL was rendered without JSON escaping")
	}
}

func assertContains(t *testing.T, value string, substring string) {
	t.Helper()
	if !strings.Contains(value, substring) {
		t.Fatalf("expected %q to contain %q", value, substring)
	}
}

func assertNotContains(t *testing.T, value string, substring string) {
	t.Helper()
	if strings.Contains(value, substring) {
		t.Fatalf("expected %q not to contain %q", value, substring)
	}
}
