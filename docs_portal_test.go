package routekit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func docsPortalTestFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":             {Data: []byte("<html><body>docs portal index</body></html>")},
		"guides.html":            {Data: []byte("<html>guides</html>")},
		"assets/main-abc123.js":  {Data: []byte("console.log('portal');")},
		"assets/styles-main.css": {Data: []byte("body{}")},
		"assets/logo.svg":        {Data: []byte("<svg/>")},
		"build/index.html":       {Data: []byte("<html><body>prefixed index</body></html>")},
	}
}

func registerTestDocsPortal(t *testing.T, engine *gin.Engine, config DocsPortalConfig) error {
	t.Helper()
	ar := NewAppRouter(nil, nil)
	return ar.RegisterDocsPortal(engine, config)
}

func TestRegisterDocsPortalServesIndex(t *testing.T) {
	engine := newEngine()
	if err := registerTestDocsPortal(t, engine, DocsPortalConfig{Assets: docsPortalTestFS()}); err != nil {
		t.Fatalf("RegisterDocsPortal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertContains(t, rec.Body.String(), "docs portal index")
	assertContains(t, rec.Header().Get("Cache-Control"), "no-store")
	assertContains(t, rec.Header().Get("Content-Type"), "text/html")
}

func TestRegisterDocsPortalServesAssetsWithContentType(t *testing.T) {
	tests := []struct {
		path        string
		contentType string
	}{
		{path: "/docs/assets/main-abc123.js", contentType: "text/javascript"},
		{path: "/docs/assets/styles-main.css", contentType: "text/css"},
		{path: "/docs/assets/logo.svg", contentType: "image/svg+xml"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			engine := newEngine()
			if err := registerTestDocsPortal(t, engine, DocsPortalConfig{Assets: docsPortalTestFS()}); err != nil {
				t.Fatalf("RegisterDocsPortal: %v", err)
			}

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			engine.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			assertContains(t, rec.Header().Get("Content-Type"), tt.contentType)
		})
	}
}

func TestRegisterDocsPortalAssetsCacheImmutable(t *testing.T) {
	engine := newEngine()
	if err := registerTestDocsPortal(t, engine, DocsPortalConfig{Assets: docsPortalTestFS()}); err != nil {
		t.Fatalf("RegisterDocsPortal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/assets/main-abc123.js", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	cacheControl := rec.Header().Get("Cache-Control")
	assertContains(t, cacheControl, "immutable")
	assertContains(t, cacheControl, "max-age=31536000")
}

func TestRegisterDocsPortalSPAFallback(t *testing.T) {
	engine := newEngine()
	if err := registerTestDocsPortal(t, engine, DocsPortalConfig{Assets: docsPortalTestFS()}); err != nil {
		t.Fatalf("RegisterDocsPortal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/guides/intro", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertContains(t, rec.Body.String(), "docs portal index")
	assertContains(t, rec.Header().Get("Cache-Control"), "no-store")
}

func TestRegisterDocsPortalUnknownWithExtension404(t *testing.T) {
	engine := newEngine()
	if err := registerTestDocsPortal(t, engine, DocsPortalConfig{Assets: docsPortalTestFS()}); err != nil {
		t.Fatalf("RegisterDocsPortal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/missing.js", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRegisterDocsPortalAssetsPrefix(t *testing.T) {
	engine := newEngine()
	if err := registerTestDocsPortal(t, engine, DocsPortalConfig{Assets: docsPortalTestFS(), AssetsPrefix: "build"}); err != nil {
		t.Fatalf("RegisterDocsPortal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertContains(t, rec.Body.String(), "prefixed index")
}

func TestRegisterDocsPortalRootPath(t *testing.T) {
	engine := newEngine()
	if err := registerTestDocsPortal(t, engine, DocsPortalConfig{Path: "/", Assets: docsPortalTestFS()}); err != nil {
		t.Fatalf("RegisterDocsPortal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertContains(t, rec.Body.String(), "docs portal index")

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/assets/main-abc123.js", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("assets status = %d, want 200", rec.Code)
	}
	assertContains(t, rec.Header().Get("Cache-Control"), "immutable")
}

func TestRegisterDocsPortalValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  DocsPortalConfig
		wantErr string
	}{
		{
			name:    "path without slash",
			config:  DocsPortalConfig{Path: "docs", Assets: docsPortalTestFS()},
			wantErr: "docs portal path must start with /",
		},
		{
			name:    "path with parameter",
			config:  DocsPortalConfig{Path: "/docs/:name", Assets: docsPortalTestFS()},
			wantErr: "docs portal path must not contain gin path parameters or wildcards",
		},
		{
			name:    "path with wildcard",
			config:  DocsPortalConfig{Path: "/docs/*path", Assets: docsPortalTestFS()},
			wantErr: "docs portal path must not contain gin path parameters or wildcards",
		},
		{
			name:    "nil assets",
			config:  DocsPortalConfig{},
			wantErr: "docs portal Assets is required",
		},
		{
			name:    "absolute assets prefix",
			config:  DocsPortalConfig{Assets: docsPortalTestFS(), AssetsPrefix: "/build"},
			wantErr: "docs portal AssetsPrefix must be a relative path",
		},
		{
			name:    "parent assets prefix",
			config:  DocsPortalConfig{Assets: docsPortalTestFS(), AssetsPrefix: "../build"},
			wantErr: "docs portal AssetsPrefix must be a relative path",
		},
		{
			name: "missing index",
			config: DocsPortalConfig{
				Assets: fstest.MapFS{
					"main.js": {Data: []byte("console.log('portal');")},
				},
			},
			wantErr: `docs portal Index "index.html" was not found in Assets`,
		},
		{
			name:    "missing custom index",
			config:  DocsPortalConfig{Assets: docsPortalTestFS(), Index: "custom.html"},
			wantErr: `docs portal Index "custom.html" was not found in Assets`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newEngine()
			err := registerTestDocsPortal(t, engine, tt.config)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestRegisterDocsPortalRequiresEngine(t *testing.T) {
	ar := NewAppRouter(nil, nil)
	if err := ar.RegisterDocsPortal(nil, DocsPortalConfig{Assets: docsPortalTestFS()}); err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterDocsPortalDoesNotRequireRegisterRoutes(t *testing.T) {
	engine := newEngine()
	ar := NewAppRouter(nil, nil)

	if err := ar.RegisterDocsPortal(engine, DocsPortalConfig{Assets: docsPortalTestFS()}); err != nil {
		t.Fatalf("RegisterDocsPortal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRegisterDocsPortalConflictReturnsError(t *testing.T) {
	tests := []struct {
		name       string
		existing   string
		portalPath string
	}{
		{
			name:       "exact conflict",
			existing:   "/docs",
			portalPath: "/docs",
		},
		{
			name:       "overlap under docs",
			existing:   "/docs/spec/attendance-system",
			portalPath: "/docs",
		},
		{
			name:       "root path conflicts with existing route",
			existing:   "/openapi.json",
			portalPath: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newEngine()
			engine.GET(tt.existing, func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			err := registerTestDocsPortal(t, engine, DocsPortalConfig{Path: tt.portalPath, Assets: docsPortalTestFS()})
			if err == nil {
				t.Fatal("expected error")
			}
			wantErr := `docs portal path "` + tt.portalPath + `" conflicts with registered route "` + tt.existing + `"`
			if err.Error() != wantErr {
				t.Fatalf("error = %q, want %q", err.Error(), wantErr)
			}
		})
	}
}

func TestRegisterDocsPortalTraversalBlocked(t *testing.T) {
	engine := newEngine()
	if err := registerTestDocsPortal(t, engine, DocsPortalConfig{Assets: docsPortalTestFS()}); err != nil {
		t.Fatalf("RegisterDocsPortal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/../secret", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	assertNotContains(t, rec.Body.String(), "docs portal index")
}
