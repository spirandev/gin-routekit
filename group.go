package routekit

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type RouteConfig struct {
	group *RouterGroup
	index int
}

type RouterGroup struct {
	ginGroup    *gin.RouterGroup
	basePath    string
	definitions []Handler
	middlewares []gin.HandlerFunc
	options     GroupOptions
}

func NewRouterGroup(engine *gin.Engine, path string, options ...GroupOption) *RouterGroup {
	groupOptions := defaultGroupOptions()
	for _, option := range options {
		option(&groupOptions)
	}

	return &RouterGroup{
		ginGroup:    engine.Group(path),
		basePath:    path,
		definitions: []Handler{},
		middlewares: []gin.HandlerFunc{},
		options:     groupOptions,
	}
}

func (rg *RouterGroup) Use(middleware ...gin.HandlerFunc) {
	rg.middlewares = append(rg.middlewares, middleware...)
}

func (rg *RouterGroup) GET(relativePath string, handler gin.HandlerFunc, description string, routeID int32) *RouteConfig {
	return rg.addRoute(http.MethodGet, relativePath, handler, description, routeID)
}

func (rg *RouterGroup) HEAD(relativePath string, handler gin.HandlerFunc, description string, routeID int32) *RouteConfig {
	return rg.addRoute(http.MethodHead, relativePath, handler, description, routeID)
}

func (rg *RouterGroup) POST(relativePath string, handler gin.HandlerFunc, description string, routeID int32) *RouteConfig {
	return rg.addRoute(http.MethodPost, relativePath, handler, description, routeID)
}

func (rg *RouterGroup) PUT(relativePath string, handler gin.HandlerFunc, description string, routeID int32) *RouteConfig {
	return rg.addRoute(http.MethodPut, relativePath, handler, description, routeID)
}

func (rg *RouterGroup) PATCH(relativePath string, handler gin.HandlerFunc, description string, routeID int32) *RouteConfig {
	return rg.addRoute(http.MethodPatch, relativePath, handler, description, routeID)
}

func (rg *RouterGroup) DELETE(relativePath string, handler gin.HandlerFunc, description string, routeID int32) *RouteConfig {
	return rg.addRoute(http.MethodDelete, relativePath, handler, description, routeID)
}

func (rg *RouterGroup) OPTIONS(relativePath string, handler gin.HandlerFunc, description string, routeID int32) *RouteConfig {
	return rg.addRoute(http.MethodOptions, relativePath, handler, description, routeID)
}

func (rg *RouterGroup) Handle(method, relativePath string, handler gin.HandlerFunc, description string, routeID int32) *RouteConfig {
	return rg.addRoute(method, relativePath, handler, description, routeID)
}

func (rg *RouterGroup) addRoute(method, path string, handler gin.HandlerFunc, description string, routeID int32) *RouteConfig {
	def := Handler{
		Method:                    method,
		Path:                      path,
		Definition:                description,
		Handler:                   handler,
		RouteId:                   routeID,
		RelativePath:              path,
		IsAuthentication:          boolPtr(true),
		IsAuthorization:           boolPtr(true),
		IsBasic:                   boolPtr(false),
		IsM2M:                     boolPtr(false),
		IsSameApplicationRequired: boolPtr(true),
		RequiresClientContext:     boolPtr(false),
	}

	rg.definitions = append(rg.definitions, def)
	return &RouteConfig{
		group: rg,
		index: len(rg.definitions) - 1,
	}
}

func (rc *RouteConfig) Public() *RouteConfig {
	rc.group.definitions[rc.index].IsAuthentication = boolPtr(false)
	rc.group.definitions[rc.index].IsAuthorization = boolPtr(false)
	return rc
}

func (rc *RouteConfig) NoAuthz() *RouteConfig {
	rc.group.definitions[rc.index].IsAuthorization = boolPtr(false)
	return rc
}

func (rc *RouteConfig) RequireClientContext() *RouteConfig {
	rc.group.definitions[rc.index].RequiresClientContext = boolPtr(true)
	return rc
}

func (rc *RouteConfig) NoClientContext() *RouteConfig {
	rc.group.definitions[rc.index].RequiresClientContext = boolPtr(false)
	return rc
}

func (rc *RouteConfig) BasicRoute() *RouteConfig {
	rc.group.definitions[rc.index].IsBasic = boolPtr(true)
	return rc
}

func (rc *RouteConfig) M2MRoute() *RouteConfig {
	rc.group.definitions[rc.index].IsM2M = boolPtr(true)
	return rc
}

func (rc *RouteConfig) AllowAnySessionApp() *RouteConfig {
	rc.group.definitions[rc.index].IsSameApplicationRequired = boolPtr(false)
	return rc
}

func (rc *RouteConfig) Use(middleware ...gin.HandlerFunc) *RouteConfig {
	rc.group.definitions[rc.index].Middleware = append(rc.group.definitions[rc.index].Middleware, middleware...)
	return rc
}

func (rg *RouterGroup) Export(groupName string, appID int64) Route {
	for _, def := range rg.definitions {
		middlewares := []gin.HandlerFunc{
			routeContextMiddleware(def.RouteId, appID, rg.options.RouteContextKeys),
		}

		middlewares = append(middlewares, rg.middlewares...)

		if def.IsAuthentication == nil || *def.IsAuthentication {
			if rg.options.AuthMiddlewareFactory != nil {
				middlewares = append(middlewares, rg.options.AuthMiddlewareFactory())
			}

			if rg.options.SameApplicationMiddleware != nil && (def.IsSameApplicationRequired == nil || *def.IsSameApplicationRequired) {
				middlewares = append(middlewares, rg.options.SameApplicationMiddleware)
			}

			if def.IsAuthorization == nil || *def.IsAuthorization {
				if rg.options.AuthorizationMiddleware != nil {
					middlewares = append(middlewares, rg.options.AuthorizationMiddleware)
				}
			}
		}

		middlewares = append(middlewares, def.Middleware...)
		middlewares = append(middlewares, def.Handler)

		rg.ginGroup.Handle(def.Method, def.Path, middlewares...)
	}

	return Route{
		Path:          rg.basePath,
		Handlers:      rg.definitions,
		Group:         groupName,
		ApplicationID: appID,
	}
}

func routeContextMiddleware(routeID int32, applicationID int64, keys RouteContextKeys) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(keys.RouteID, routeID)
		c.Set(keys.ApplicationID, applicationID)
		c.Next()
	}
}
