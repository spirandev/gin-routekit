package routekit

import "github.com/gin-gonic/gin"

type GroupOptions struct {
	AuthMiddlewareFactory     func() gin.HandlerFunc
	AuthorizationMiddleware   gin.HandlerFunc
	SameApplicationMiddleware gin.HandlerFunc
	RouteContextKeys          RouteContextKeys
	documentationDefaults     DocumentationDefaults
}

type RouteContextKeys struct {
	RouteID       string
	ApplicationID string
}

type GroupOption func(*GroupOptions)

func WithAuthMiddlewareFactory(factory func() gin.HandlerFunc) GroupOption {
	return func(options *GroupOptions) {
		options.AuthMiddlewareFactory = factory
	}
}

func WithAuthorizationMiddleware(middleware gin.HandlerFunc) GroupOption {
	return func(options *GroupOptions) {
		options.AuthorizationMiddleware = middleware
	}
}

func WithSameApplicationMiddleware(middleware gin.HandlerFunc) GroupOption {
	return func(options *GroupOptions) {
		options.SameApplicationMiddleware = middleware
	}
}

func WithRouteContextKeys(routeIDKey, applicationIDKey string) GroupOption {
	return func(options *GroupOptions) {
		options.RouteContextKeys = RouteContextKeys{
			RouteID:       routeIDKey,
			ApplicationID: applicationIDKey,
		}
	}
}

func WithDocumentation() GroupOption {
	enabled := true
	return func(options *GroupOptions) {
		options.documentationDefaults.Enabled = &enabled
	}
}

func WithDocProfiles(names ...string) GroupOption {
	copied := append([]string(nil), names...)
	return func(options *GroupOptions) {
		options.documentationDefaults.Profiles = append(options.documentationDefaults.Profiles, copied...)
	}
}

func WithDefaultHeader(param DocParam) GroupOption {
	param = cloneDocParam(param)
	param.In = DocParamInHeader
	return func(options *GroupOptions) {
		options.documentationDefaults.Headers = append(options.documentationDefaults.Headers, param)
	}
}

func WithDefaultQuery(param DocParam) GroupOption {
	param = cloneDocParam(param)
	param.In = DocParamInQuery
	return func(options *GroupOptions) {
		options.documentationDefaults.QueryParams = append(options.documentationDefaults.QueryParams, param)
	}
}

func WithDefaultPathParam(param DocParam) GroupOption {
	param = cloneDocParam(param)
	param.In = DocParamInPath
	return func(options *GroupOptions) {
		options.documentationDefaults.PathParams = append(options.documentationDefaults.PathParams, param)
	}
}

func WithDefaultResponse(status int, description string, schema any) GroupOption {
	return WithDefaultResponseWith(status, description, "", schema)
}

func WithDefaultResponseWith(status int, description, contentType string, schema any) GroupOption {
	response := DocResponse{Status: status, Description: description, ContentType: contentType, Schema: schema}
	return func(options *GroupOptions) {
		options.documentationDefaults.Responses = append(options.documentationDefaults.Responses, cloneDocResponse(response))
	}
}

func WithDefaultContentTypes(request, response string) GroupOption {
	return func(options *GroupOptions) {
		options.documentationDefaults.RequestContentType = request
		options.documentationDefaults.ResponseContentType = response
	}
}

func defaultGroupOptions() GroupOptions {
	return GroupOptions{
		RouteContextKeys: RouteContextKeys{
			RouteID:       "RouteID",
			ApplicationID: "ApplicationID",
		},
	}
}
