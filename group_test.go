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

func TestLegacyHTTPRegistrationCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := NewRouterGroup(engine, "/api")

	group.GET("/get", okHandler, "get", 1)
	group.HEAD("/head", okHandler, "head", 2)
	group.POST("/post", okHandler, "post", 3)
	group.PUT("/put", okHandler, "put", 4)
	group.PATCH("/patch", okHandler, "patch", 5)
	group.DELETE("/delete", okHandler, "delete", 6)
	group.OPTIONS("/options", okHandler, "options", 7)
	group.Handle(http.MethodTrace, "/trace", okHandler, "trace", 8)

	route := group.Export("api", 123)
	wantMethods := []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
		http.MethodTrace,
	}
	if len(route.Handlers) != len(wantMethods) {
		t.Fatalf("handlers = %d, want %d", len(route.Handlers), len(wantMethods))
	}
	for i, want := range wantMethods {
		if got := route.Handlers[i].Method; got != want {
			t.Errorf("handler %d method = %q, want %q", i, got, want)
		}
		if got := route.Handlers[i].RouteId; got != int32(i+1) {
			t.Errorf("handler %d route ID = %d, want %d", i, got, i+1)
		}
	}
}

func TestLegacyRouteFluentsCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := NewRouterGroup(engine, "/api")
	middleware := countingMiddleware(new(int))

	group.GET("/resource", okHandler, "resource", 1).
		NoAuthz().
		RequireClientContext().
		BasicRoute().
		M2MRoute().
		AllowAnySessionApp().
		IntegrationRoute().
		Scopes("scope:a", "scope:b").
		Use(middleware)
	group.GET("/no-client", okHandler, "no client", 2).
		RequireClientContext().
		NoClientContext().
		Public()

	route := group.Export("api", 123)
	got := route.Handlers[0]
	if got.IsAuthentication == nil || !*got.IsAuthentication {
		t.Error("authenticated fluent route lost authentication metadata")
	}
	if got.IsAuthorization == nil || *got.IsAuthorization {
		t.Error("NoAuthz did not disable authorization")
	}
	if got.RequiresClientContext == nil || !*got.RequiresClientContext {
		t.Error("RequireClientContext did not set metadata")
	}
	if got.IsBasic == nil || !*got.IsBasic || got.IsM2M == nil || !*got.IsM2M {
		t.Error("BasicRoute or M2MRoute metadata was not preserved")
	}
	if got.IsSameApplicationRequired == nil || *got.IsSameApplicationRequired {
		t.Error("AllowAnySessionApp did not disable same-application metadata")
	}
	if got.IsIntegration == nil || !*got.IsIntegration {
		t.Error("IntegrationRoute did not set metadata")
	}
	if !slices.Equal(got.Scopes, []string{"scope:a", "scope:b"}) {
		t.Errorf("scopes = %v", got.Scopes)
	}
	if len(got.Middleware) != 1 {
		t.Errorf("route middleware count = %d, want 1", len(got.Middleware))
	}

	noClient := route.Handlers[1]
	if noClient.RequiresClientContext == nil || *noClient.RequiresClientContext {
		t.Error("NoClientContext did not disable client context")
	}
	if noClient.IsAuthentication == nil || *noClient.IsAuthentication || noClient.IsAuthorization == nil || *noClient.IsAuthorization {
		t.Error("Public did not disable authentication and authorization")
	}
}

func TestLegacyMiddlewareAndRouteContextOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	order := []string{}
	appendMiddleware := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) {
			order = append(order, name)
			c.Next()
		}
	}
	group := NewRouterGroup(
		engine,
		"/api",
		WithRouteContextKeys("route-id", "app-id"),
		WithAuthMiddlewareFactory(func() gin.HandlerFunc { return appendMiddleware("auth") }),
		WithSameApplicationMiddleware(appendMiddleware("same-app")),
		WithAuthorizationMiddleware(appendMiddleware("authorization")),
	)
	group.Use(func(c *gin.Context) {
		routeID, routeIDExists := c.Get("route-id")
		if !routeIDExists || routeID != int32(7) || c.GetInt64("app-id") != 99 {
			t.Errorf("route context = (%v, %d), want (7, 99)", routeID, c.GetInt64("app-id"))
		}
		order = append(order, "group")
		c.Next()
	})
	group.GET("/resource", func(c *gin.Context) {
		order = append(order, "handler")
		c.Status(http.StatusOK)
	}, "resource", 7).Use(appendMiddleware("route"))
	group.Export("api", 99)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	want := []string{"group", "auth", "same-app", "authorization", "route", "handler"}
	if !slices.Equal(order, want) {
		t.Errorf("middleware order = %v, want %v", order, want)
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
