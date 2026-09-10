package routekit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterStoplightUIDefaults(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterStoplightUI(engine, StoplightUIConfig{}); err != nil {
		t.Fatalf("RegisterStoplightUI: %v", err)
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
	assertContains(t, body, "<title>API Docs</title>")
	assertContains(t, body, `apiDescriptionUrl="/openapi.json"`)
	assertContains(t, body, `layout="sidebar"`)
	assertContains(t, body, `router="hash"`)
	assertContains(t, body, defaultStoplightCDN+"/styles.min.css")
	assertContains(t, body, defaultStoplightCDN+"/web-components.min.js")
	assertNotContains(t, body, "docs.hideTryIt = true;")
	assertNotContains(t, body, "docs.hideTryItPanel = true;")
	assertNotContains(t, body, "docs.hideExport = true;")
	assertNotContains(t, body, "docs.hideSchemas = true;")
	assertNotContains(t, body, `logo="`)
	assertNotContains(t, body, `basePath="`)
}

func TestRegisterStoplightUICustomConfig(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterStoplightUI(engine, StoplightUIConfig{
		Path:       "/documentation",
		OpenAPIURL: "/spec/openapi.json",
		Title:      "Custom API Docs",
		CDNBaseURL: "https://cdn.example.com/elements",
		Logo:       "/assets/logo.png",
		Layout:     StoplightLayoutResponsive,
	}); err != nil {
		t.Fatalf("RegisterStoplightUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/documentation", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, "<title>Custom API Docs</title>")
	assertContains(t, body, `apiDescriptionUrl="/spec/openapi.json"`)
	assertContains(t, body, `layout="responsive"`)
	assertContains(t, body, `router="hash"`)
	assertContains(t, body, `logo="/assets/logo.png"`)
	assertContains(t, body, "https://cdn.example.com/elements/styles.min.css")
	assertContains(t, body, "https://cdn.example.com/elements/web-components.min.js")
}

func TestRegisterStoplightUIHideOptions(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterStoplightUI(engine, StoplightUIConfig{
		HideExport:     true,
		HideSchemas:    true,
		HideTryIt:      true,
		HideTryItPanel: true,
	}); err != nil {
		t.Fatalf("RegisterStoplightUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, "docs.hideTryIt = true;")
	assertContains(t, body, "docs.hideTryItPanel = true;")
	assertContains(t, body, "docs.hideExport = true;")
	assertContains(t, body, "docs.hideSchemas = true;")
}

func TestRegisterStoplightUIRouterHistory(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterStoplightUI(engine, StoplightUIConfig{
		Path:   "/documentation",
		Router: StoplightRouterHistory,
	}); err != nil {
		t.Fatalf("RegisterStoplightUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/documentation", nil)
	engine.ServeHTTP(rec, req)

	body := rec.Body.String()
	assertContains(t, body, `router="history"`)
	assertContains(t, body, `basePath="/documentation"`)
}

func TestRegisterStoplightUIValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  StoplightUIConfig
		wantErr string
	}{
		{
			name:    "path without slash",
			config:  StoplightUIConfig{Path: "docs"},
			wantErr: "stoplight UI path must start with /",
		},
		{
			name:    "path with parameter",
			config:  StoplightUIConfig{Path: "/docs/:name"},
			wantErr: "stoplight UI path must not contain gin path parameters or wildcards",
		},
		{
			name:    "path with wildcard",
			config:  StoplightUIConfig{Path: "/docs/*path"},
			wantErr: "stoplight UI path must not contain gin path parameters or wildcards",
		},
		{
			name:    "invalid openapi url",
			config:  StoplightUIConfig{OpenAPIURL: "openapi.json"},
			wantErr: "stoplight UI OpenAPIURL must be an absolute path or URL",
		},
		{
			name:    "invalid cdn base url",
			config:  StoplightUIConfig{CDNBaseURL: "/assets/elements"},
			wantErr: "stoplight UI CDNBaseURL must start with http:// or https://",
		},
		{
			name:    "invalid layout",
			config:  StoplightUIConfig{Layout: "wide"},
			wantErr: "stoplight UI Layout must be one of sidebar, responsive, stacked",
		},
		{
			name:    "invalid router",
			config:  StoplightUIConfig{Router: "browser"},
			wantErr: "stoplight UI Router must be one of hash, history, memory, static",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newEngine()
			ar := NewAppRouter(nil, nil)
			err := ar.RegisterStoplightUI(engine, tt.config)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestRegisterStoplightUIRequiresEngine(t *testing.T) {
	ar := NewAppRouter(nil, nil)
	if err := ar.RegisterStoplightUI(nil, StoplightUIConfig{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterStoplightUIDoesNotRequireOpenAPIRegistration(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterStoplightUI(engine, StoplightUIConfig{}); err != nil {
		t.Fatalf("RegisterStoplightUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRenderStoplightUIHTMLEscapesTitle(t *testing.T) {
	html, err := renderStoplightUIHTML(normalizeStoplightUIConfig(StoplightUIConfig{
		Title: "Docs <script>alert(1)</script>",
	}))
	if err != nil {
		t.Fatalf("renderStoplightUIHTML: %v", err)
	}

	assertContains(t, html, "Docs &lt;script&gt;alert(1)&lt;/script&gt;")
	if strings.Contains(html, "<title>Docs <script>alert(1)</script></title>") {
		t.Fatal("title was not escaped")
	}
}

func TestRenderStoplightUIHTMLEscapesAttributeValues(t *testing.T) {
	malicious := `/openapi.json?x="><script>alert(1)</script>`
	html, err := renderStoplightUIHTML(normalizeStoplightUIConfig(StoplightUIConfig{
		OpenAPIURL: malicious,
		Logo:       malicious,
	}))
	if err != nil {
		t.Fatalf("renderStoplightUIHTML: %v", err)
	}

	assertContains(t, html, "&lt;script&gt;")
	if strings.Contains(html, malicious) {
		t.Fatal("attribute value was rendered without escaping")
	}
	if strings.Contains(html, `apiDescriptionUrl="/openapi.json?x="`) {
		t.Fatal("attribute breakout detected")
	}
	if strings.Contains(html, `logo="/openapi.json?x="`) {
		t.Fatal("attribute breakout detected in logo")
	}
}
