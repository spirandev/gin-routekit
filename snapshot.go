package routekit

import "github.com/gin-gonic/gin"

func cloneRoutes(routes []Route) []Route {
	if routes == nil {
		return nil
	}
	cloned := make([]Route, len(routes))
	for i, route := range routes {
		cloned[i] = Route{
			Path:          route.Path,
			Definition:    route.Definition,
			Group:         route.Group,
			ApplicationID: route.ApplicationID,
			Handlers:      cloneHandlers(route.Handlers),
			Middleware:    append([]gin.HandlerFunc(nil), route.Middleware...),
		}
	}
	return cloned
}

func cloneHandlers(handlers []Handler) []Handler {
	if handlers == nil {
		return nil
	}
	cloned := make([]Handler, len(handlers))
	for i, h := range handlers {
		cloned[i] = Handler{
			Handler:                   h.Handler,
			Middleware:                append([]gin.HandlerFunc(nil), h.Middleware...),
			Method:                    h.Method,
			Path:                      h.Path,
			Definition:                h.Definition,
			RouteId:                   h.RouteId,
			RelativePath:              h.RelativePath,
			IsAuthentication:          h.IsAuthentication,
			IsAuthorization:           h.IsAuthorization,
			RequiresClientContext:     h.RequiresClientContext,
			IsBasic:                   h.IsBasic,
			IsM2M:                     h.IsM2M,
			IsSameApplicationRequired: h.IsSameApplicationRequired,
			IsIntegration:             h.IsIntegration,
			Scopes:                    append([]string(nil), h.Scopes...),
			Doc:                       cloneDocConfig(h.Doc),
		}
	}
	return cloned
}

func cloneDocConfig(doc *DocConfig) *DocConfig {
	if doc == nil {
		return nil
	}
	enabledCopy := doc.Enabled
	var enabled *bool
	if enabledCopy != nil {
		v := *enabledCopy
		enabled = &v
	}

	headers := make([]DocParam, len(doc.Headers))
	copy(headers, doc.Headers)
	pathParams := make([]DocParam, len(doc.PathParams))
	copy(pathParams, doc.PathParams)
	queryParams := make([]DocParam, len(doc.QueryParams))
	copy(queryParams, doc.QueryParams)

	cloned := &DocConfig{
		Enabled:     enabled,
		Summary:     doc.Summary,
		Description: doc.Description,
		Tags:        append([]string(nil), doc.Tags...),
		OperationID: doc.OperationID,
		Profiles:    append([]string(nil), doc.Profiles...),
		Headers:     headers,
		PathParams:  pathParams,
		QueryParams: queryParams,
	}

	if doc.RequestBody != nil {
		cloned.RequestBody = &DocBody{
			Description: doc.RequestBody.Description,
			Required:    doc.RequestBody.Required,
			Schema:      doc.RequestBody.Schema,
			ContentType: doc.RequestBody.ContentType,
			Example:     doc.RequestBody.Example,
		}
	}

	cloned.Responses = make([]DocResponse, len(doc.Responses))
	copy(cloned.Responses, doc.Responses)

	return cloned
}
