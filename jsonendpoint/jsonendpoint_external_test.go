package jsonendpoint_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
	"github.com/spirandev/gin-routekit/jsonendpoint"
)

type externalRequest struct {
	Name string `json:"name"`
}

type externalResponse struct {
	Name string `json:"name"`
}

type externalService struct{}

func (externalService) create(_ *gin.Context, request externalRequest) (externalResponse, error) {
	return externalResponse{Name: request.Name}, nil
}

func TestHandleInfersTypesFromFunctionAndMethodValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := routekit.NewRouterGroup(engine, "/api")

	functionRoute := jsonendpoint.Handle(group, http.MethodPost, "/function", func(_ *gin.Context, request externalRequest) (externalResponse, error) {
		return externalResponse{Name: request.Name}, nil
	}, "function", 1)
	methodRoute := jsonendpoint.Handle(group, http.MethodPost, "/method", externalService{}.create, "method", 2)

	functionRoute.Public().NoClientContext().Use(func(context *gin.Context) { context.Next() })
	methodRoute.NoAuthz().RequireClientContext().Scopes("write")
	route := group.Export("external", 1)
	if len(route.Handlers) != 2 {
		t.Fatalf("handlers = %d, want 2", len(route.Handlers))
	}
	if route.Handlers[0].IsAuthentication == nil || *route.Handlers[0].IsAuthentication {
		t.Fatal("returned RouteConfig did not preserve Public fluent")
	}
	if route.Handlers[1].RequiresClientContext == nil || !*route.Handlers[1].RequiresClientContext {
		t.Fatal("returned RouteConfig did not preserve RequireClientContext fluent")
	}
}
