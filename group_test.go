package routekit

import (
	"net/http"
	"net/http/httptest"
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
