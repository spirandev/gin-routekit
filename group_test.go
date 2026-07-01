package routekit

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestExportMiddlewareBehavior(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name              string
		configureRoute    func(*RouteConfig, *middlewareCounts)
		wantAuth          int
		wantAuthorization int
		wantSameApp       int
		wantCustom        int
	}{
		{
			name:              "authenticated route uses same application middleware",
			wantAuth:          1,
			wantAuthorization: 1,
			wantSameApp:       1,
		},
		{
			name: "m2m route keeps authenticated middleware behavior",
			configureRoute: func(route *RouteConfig, _ *middlewareCounts) {
				route.M2MRoute()
			},
			wantAuth:          1,
			wantAuthorization: 1,
			wantSameApp:       1,
		},
		{
			name: "allow any session app skips same application middleware",
			configureRoute: func(route *RouteConfig, _ *middlewareCounts) {
				route.AllowAnySessionApp()
			},
			wantAuth:          1,
			wantAuthorization: 1,
		},
		{
			name: "public route skips authentication and authorization middlewares",
			configureRoute: func(route *RouteConfig, _ *middlewareCounts) {
				route.Public()
			},
		},
		{
			name: "allow any session app keeps custom route middleware",
			configureRoute: func(route *RouteConfig, counts *middlewareCounts) {
				route.AllowAnySessionApp().Use(countingMiddleware(&counts.custom))
			},
			wantAuth:          1,
			wantAuthorization: 1,
			wantCustom:        1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts := middlewareCounts{}
			engine := gin.New()
			group := NewRouterGroup(
				engine,
				"/api",
				WithAuthMiddlewareFactory(func() gin.HandlerFunc {
					return countingMiddleware(&counts.auth)
				}),
				WithAuthorizationMiddleware(countingMiddleware(&counts.authorization)),
				WithSameApplicationMiddleware(countingMiddleware(&counts.sameApp)),
			)

			route := group.GET("/resource", okHandler, "resource", 1)
			if tt.configureRoute != nil {
				tt.configureRoute(route, &counts)
			}

			group.Export("api", 123)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
			engine.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
			}

			if counts.auth != tt.wantAuth {
				t.Errorf("auth middleware count = %d, want %d", counts.auth, tt.wantAuth)
			}
			if counts.authorization != tt.wantAuthorization {
				t.Errorf("authorization middleware count = %d, want %d", counts.authorization, tt.wantAuthorization)
			}
			if counts.sameApp != tt.wantSameApp {
				t.Errorf("same app middleware count = %d, want %d", counts.sameApp, tt.wantSameApp)
			}
			if counts.custom != tt.wantCustom {
				t.Errorf("custom middleware count = %d, want %d", counts.custom, tt.wantCustom)
			}
		})
	}
}

func TestExportM2MFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		configure func(*RouteConfig)
		want      bool
	}{
		{
			name: "default route is not m2m",
		},
		{
			name: "m2m route is marked",
			configure: func(route *RouteConfig) {
				route.M2MRoute()
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := gin.New()
			group := NewRouterGroup(engine, "/api")

			routeConfig := group.GET("/resource", okHandler, "resource", 1)
			if tt.configure != nil {
				tt.configure(routeConfig)
			}

			route := group.Export("api", 123)
			if len(route.Handlers) != 1 {
				t.Fatalf("expected 1 handler, got %d", len(route.Handlers))
			}
			if route.Handlers[0].IsM2M == nil {
				t.Fatal("expected IsM2M to be set")
			}
			if got := *route.Handlers[0].IsM2M; got != tt.want {
				t.Errorf("IsM2M = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestExportIntegrationAndScopesMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name            string
		configure       func(*RouteConfig)
		wantIntegration bool
		wantScopes      []string
	}{
		{
			name:       "default route is not integration and has empty scopes",
			wantScopes: []string{},
		},
		{
			name: "integration route is marked without scopes",
			configure: func(route *RouteConfig) {
				route.IntegrationRoute()
			},
			wantIntegration: true,
			wantScopes:      []string{},
		},
		{
			name: "scoped route keeps integration false",
			configure: func(route *RouteConfig) {
				route.Scopes("scope:a", "scope:b")
			},
			wantScopes: []string{"scope:a", "scope:b"},
		},
		{
			name: "integration route with scopes exports both",
			configure: func(route *RouteConfig) {
				route.IntegrationRoute().Scopes("scope:a")
			},
			wantIntegration: true,
			wantScopes:      []string{"scope:a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := gin.New()
			group := NewRouterGroup(engine, "/api")
			routeConfig := group.GET("/resource", okHandler, "resource", 1)
			if tt.configure != nil {
				tt.configure(routeConfig)
			}
			route := group.Export("api", 123)
			if len(route.Handlers) != 1 {
				t.Fatalf("expected 1 handler, got %d", len(route.Handlers))
			}
			handler := route.Handlers[0]
			if handler.IsIntegration == nil {
				t.Fatal("expected IsIntegration to be set")
			}
			if got := *handler.IsIntegration; got != tt.wantIntegration {
				t.Errorf("IsIntegration = %t, want %t", got, tt.wantIntegration)
			}
			if handler.Scopes == nil {
				t.Fatal("expected Scopes to be an empty slice, got nil")
			}
			if !slices.Equal(handler.Scopes, tt.wantScopes) {
				t.Errorf("Scopes = %v, want %v", handler.Scopes, tt.wantScopes)
			}
		})
	}
}
func TestScopesCopiesInputSlice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := NewRouterGroup(engine, "/api")
	scopes := []string{"scope:a", "scope:b"}
	group.GET("/resource", okHandler, "resource", 1).Scopes(scopes...)
	scopes[0] = "scope:changed"
	route := group.Export("api", 123)
	if len(route.Handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(route.Handlers))
	}
	want := []string{"scope:a", "scope:b"}
	if !slices.Equal(route.Handlers[0].Scopes, want) {
		t.Errorf("Scopes = %v, want %v", route.Handlers[0].Scopes, want)
	}
}

func TestExportTreatsNilSameApplicationRequirementAsRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)

	counts := middlewareCounts{}
	engine := gin.New()
	group := NewRouterGroup(
		engine,
		"/api",
		WithAuthMiddlewareFactory(func() gin.HandlerFunc {
			return countingMiddleware(&counts.auth)
		}),
		WithSameApplicationMiddleware(countingMiddleware(&counts.sameApp)),
	)

	group.GET("/resource", okHandler, "resource", 1)
	group.definitions[0].IsSameApplicationRequired = nil
	group.Export("api", 123)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if counts.auth != 1 {
		t.Errorf("auth middleware count = %d, want %d", counts.auth, 1)
	}
	if counts.sameApp != 1 {
		t.Errorf("same app middleware count = %d, want %d", counts.sameApp, 1)
	}
}

func TestExportWithoutSameApplicationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	counts := middlewareCounts{}
	engine := gin.New()
	group := NewRouterGroup(
		engine,
		"/api",
		WithAuthMiddlewareFactory(func() gin.HandlerFunc {
			return countingMiddleware(&counts.auth)
		}),
	)

	group.GET("/resource", okHandler, "resource", 1)
	group.Export("api", 123)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if counts.auth != 1 {
		t.Errorf("auth middleware count = %d, want %d", counts.auth, 1)
	}
}

type middlewareCounts struct {
	auth          int
	authorization int
	sameApp       int
	custom        int
}

func countingMiddleware(count *int) gin.HandlerFunc {
	return func(c *gin.Context) {
		(*count)++
		c.Next()
	}
}

func okHandler(c *gin.Context) {
	c.Status(http.StatusOK)
}
