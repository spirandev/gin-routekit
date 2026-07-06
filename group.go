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
		IsIntegration:             boolPtr(false),
		Scopes:                    []string{},
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

func (rc *RouteConfig) IntegrationRoute() *RouteConfig {
	rc.group.definitions[rc.index].IsIntegration = boolPtr(true)
	return rc
}

func (rc *RouteConfig) Scopes(scopes ...string) *RouteConfig {
	rc.group.definitions[rc.index].Scopes = append([]string{}, scopes...)
	return rc
}

func (rc *RouteConfig) Use(middleware ...gin.HandlerFunc) *RouteConfig {
	rc.group.definitions[rc.index].Middleware = append(rc.group.definitions[rc.index].Middleware, middleware...)
	return rc
}

func (rc *RouteConfig) ensureDoc() *DocConfig {
	def := &rc.group.definitions[rc.index]
	if def.Doc == nil {
		def.Doc = &DocConfig{}
	}
	return def.Doc
}

func (rc *RouteConfig) Document() *RouteConfig {
	doc := rc.ensureDoc()
	enabled := true
	doc.Enabled = &enabled
	return rc
}

func (rc *RouteConfig) HideFromDocs() *RouteConfig {
	doc := rc.ensureDoc()
	enabled := false
	doc.Enabled = &enabled
	return rc
}

func (rc *RouteConfig) Summary(value string) *RouteConfig {
	rc.ensureDoc().Summary = value
	return rc
}

func (rc *RouteConfig) Description(value string) *RouteConfig {
	rc.ensureDoc().Description = value
	return rc
}

func (rc *RouteConfig) OperationID(value string) *RouteConfig {
	rc.ensureDoc().OperationID = value
	return rc
}

func (rc *RouteConfig) Tags(tags ...string) *RouteConfig {
	doc := rc.ensureDoc()
	doc.Tags = append([]string{}, tags...)
	return rc
}

func (rc *RouteConfig) DocProfile(names ...string) *RouteConfig {
	doc := rc.ensureDoc()
	doc.Profiles = append(doc.Profiles, names...)
	return rc
}

func (rc *RouteConfig) Header(name, typ string, required bool, description string) *RouteConfig {
	doc := rc.ensureDoc()
	doc.Headers = append(doc.Headers, DocParam{
		Name:        name,
		In:          DocParamInHeader,
		Type:        typ,
		Required:    required,
		Description: description,
	})
	return rc
}

func (rc *RouteConfig) Query(name, typ string, required bool, description string) *RouteConfig {
	doc := rc.ensureDoc()
	doc.QueryParams = append(doc.QueryParams, DocParam{
		Name:        name,
		In:          DocParamInQuery,
		Type:        typ,
		Required:    required,
		Description: description,
	})
	return rc
}

func (rc *RouteConfig) PathParam(name, typ string, required bool, description string) *RouteConfig {
	doc := rc.ensureDoc()
	doc.PathParams = append(doc.PathParams, DocParam{
		Name:        name,
		In:          DocParamInPath,
		Type:        typ,
		Required:    required,
		Description: description,
	})
	return rc
}

func (rc *RouteConfig) Body(schema any) *RouteConfig {
	rc.ensureDoc().RequestBody = &DocBody{Schema: schema, Required: true}
	return rc
}

func (rc *RouteConfig) BodyWith(description string, required bool, schema any) *RouteConfig {
	rc.ensureDoc().RequestBody = &DocBody{
		Description: description,
		Required:    required,
		Schema:      schema,
	}
	return rc
}

func (rc *RouteConfig) Response(status int, description string, schema any) *RouteConfig {
	doc := rc.ensureDoc()
	doc.Responses = append(doc.Responses, DocResponse{
		Status:      status,
		Description: description,
		Schema:      schema,
	})
	return rc
}

func (rc *RouteConfig) ResponseWith(status int, description string, contentType string, schema any) *RouteConfig {
	doc := rc.ensureDoc()
	doc.Responses = append(doc.Responses, DocResponse{
		Status:      status,
		Description: description,
		ContentType: contentType,
		Schema:      schema,
	})
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
