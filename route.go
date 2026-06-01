package routekit

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

var ValidMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodHead:    {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodPatch:   {},
	http.MethodDelete:  {},
	http.MethodConnect: {},
	http.MethodOptions: {},
	http.MethodTrace:   {},
}

type Route struct {
	Path          string
	Handlers      []Handler
	Middleware    []gin.HandlerFunc
	Definition    string
	Group         string
	ApplicationID int64
}

type Handler struct {
	Handler          gin.HandlerFunc
	Middleware       []gin.HandlerFunc
	Method           string
	Path             string
	Definition       string
	RouteId          int32
	RelativePath     string
	IsAuthentication *bool
	IsAuthorization  *bool
	IsBasic          *bool
}

type RouteRegistrar interface {
	Register(engine *gin.Engine) Route
}

func boolPtr(value bool) *bool {
	return &value
}
