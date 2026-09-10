package routekit_test

import (
	"testing"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
)

func TestDocsPortalPublicAPICompiles(t *testing.T) {
	var _ func(*routekit.AppRouter, *gin.Engine, routekit.DocsPortalConfig) error = (*routekit.AppRouter).RegisterDocsPortal
	_ = routekit.DocsPortalConfig{Path: "/docs", Index: "index.html"}
}
