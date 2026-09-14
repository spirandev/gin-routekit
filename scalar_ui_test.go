package routekit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterScalarUIDefaults(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterScalarUI(engine, ScalarUIConfig{}); err != nil {
		t.Fatalf("RegisterScalarUI: %v", err)
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
	assertContains(t, body, `data-url="/openapi.json"`)
	assertContains(t, body, `src="`+defaultScalarCDN+`"`)
	assertContains(t, body, "&#34;theme&#34;:&#34;default&#34;")
	assertContains(t, body, "&#34;layout&#34;:&#34;modern&#34;")
	assertContains(t, body, "&#34;showSidebar&#34;:true")
	assertNotContains(t, body, "hideDownloadButton")
	assertNotContains(t, body, "hideTestRequestButton")
	assertNotContains(t, body, "hideModels")
}

func TestRegisterScalarUICustomConfig(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterScalarUI(engine, ScalarUIConfig{
		Path:       "/documentation",
		OpenAPIURL: "/spec/openapi.json",
		Title:      "Custom API Docs",
		CDNBaseURL: "https://cdn.example.com/scalar.js",
		Theme:      ScalarThemePurple,
		Layout:     ScalarLayoutClassic,
	}); err != nil {
		t.Fatalf("RegisterScalarUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/documentation", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, "<title>Custom API Docs</title>")
	assertContains(t, body, `data-url="/spec/openapi.json"`)
	assertContains(t, body, `src="https://cdn.example.com/scalar.js"`)
	assertContains(t, body, "&#34;theme&#34;:&#34;purple&#34;")
	assertContains(t, body, "&#34;layout&#34;:&#34;classic&#34;")
}

func TestRegisterScalarUIHideOptions(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterScalarUI(engine, ScalarUIConfig{
		HideSidebar:           true,
		HideDownloadButton:    true,
		HideTestRequestButton: true,
		HideModels:            true,
	}); err != nil {
		t.Fatalf("RegisterScalarUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, "&#34;showSidebar&#34;:false")
	assertContains(t, body, "&#34;hideDownloadButton&#34;:true")
	assertContains(t, body, "&#34;hideTestRequestButton&#34;:true")
	assertContains(t, body, "&#34;hideModels&#34;:true")
}

func TestRegisterScalarUIValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  ScalarUIConfig
		wantErr string
	}{
		{
			name:    "path without slash",
			config:  ScalarUIConfig{Path: "docs"},
			wantErr: "scalar UI path must start with /",
		},
		{
			name:    "path with parameter",
			config:  ScalarUIConfig{Path: "/docs/:name"},
			wantErr: "scalar UI path must not contain gin path parameters or wildcards",
		},
		{
			name:    "path with wildcard",
			config:  ScalarUIConfig{Path: "/docs/*path"},
			wantErr: "scalar UI path must not contain gin path parameters or wildcards",
		},
		{
			name:    "invalid openapi url",
			config:  ScalarUIConfig{OpenAPIURL: "openapi.json"},
			wantErr: "scalar UI OpenAPIURL must be an absolute path or URL",
		},
		{
			name:    "invalid cdn base url",
			config:  ScalarUIConfig{CDNBaseURL: "/assets/scalar.js"},
			wantErr: "scalar UI CDNBaseURL must start with http:// or https://",
		},
		{
			name:    "invalid theme",
			config:  ScalarUIConfig{Theme: "galaxy"},
			wantErr: "scalar UI Theme must be one of default, alternate, moon, purple, solarized, bluePlanet, saturn, kepler, mars, deepSpace, none",
		},
		{
			name:    "invalid layout",
			config:  ScalarUIConfig{Layout: "wide"},
			wantErr: "scalar UI Layout must be one of modern, classic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newEngine()
			ar := NewAppRouter(nil, nil)
			err := ar.RegisterScalarUI(engine, tt.config)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestRegisterScalarUIRequiresEngine(t *testing.T) {
	ar := NewAppRouter(nil, nil)
	if err := ar.RegisterScalarUI(nil, ScalarUIConfig{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterScalarUIDoesNotRequireOpenAPIRegistration(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterScalarUI(engine, ScalarUIConfig{}); err != nil {
		t.Fatalf("RegisterScalarUI: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRenderScalarUIHTMLEscapesTitle(t *testing.T) {
	html, err := renderScalarUIHTML(normalizeScalarUIConfig(ScalarUIConfig{
		Title: "Docs <script>alert(1)</script>",
	}))
	if err != nil {
		t.Fatalf("renderScalarUIHTML: %v", err)
	}

	assertContains(t, html, "Docs &lt;script&gt;alert(1)&lt;/script&gt;")
	if strings.Contains(html, "<title>Docs <script>alert(1)</script></title>") {
		t.Fatal("title was not escaped")
	}
}

func TestRenderScalarUIHTMLEscapesConfigurationAttribute(t *testing.T) {
	// data-url is treated as a URL-typed attribute by html/template, so the
	// malicious value comes out percent-encoded rather than HTML-entity
	// escaped. Either form is safe as long as it cannot break out of the
	// data-url attribute or inject a literal <script> tag.
	malicious := `/openapi.json?x="><script>alert(1)</script>`
	html, err := renderScalarUIHTML(normalizeScalarUIConfig(ScalarUIConfig{
		OpenAPIURL: malicious,
	}))
	if err != nil {
		t.Fatalf("renderScalarUIHTML: %v", err)
	}

	if strings.Contains(html, malicious) {
		t.Fatal("attribute value was rendered without escaping")
	}
	if strings.Contains(html, `<script>alert(1)</script>`) {
		t.Fatal("script tag was injected unescaped")
	}
	if strings.Contains(html, `data-url="/openapi.json?x="`) {
		t.Fatal("attribute breakout detected")
	}
}
