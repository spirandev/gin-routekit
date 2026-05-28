package routekit

import "github.com/gin-gonic/gin"

type GroupOptions struct {
	AuthMiddlewareFactory   func() gin.HandlerFunc
	AuthorizationMiddleware gin.HandlerFunc
	RouteContextKeys        RouteContextKeys
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

func WithRouteContextKeys(routeIDKey, applicationIDKey string) GroupOption {
	return func(options *GroupOptions) {
		options.RouteContextKeys = RouteContextKeys{
			RouteID:       routeIDKey,
			ApplicationID: applicationIDKey,
		}
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
