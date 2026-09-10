package routekit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

func validateUIPath(name, path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("%s path must start with /", name)
	}
	if strings.ContainsAny(path, ":*") {
		return fmt.Errorf("%s path must not contain gin path parameters or wildcards", name)
	}
	return nil
}

func registerDocsPage(engine *gin.Engine, path, html string) error {
	if engine == nil {
		return errors.New("gin engine is required")
	}
	engine.GET(path, func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		_, _ = c.Writer.Write([]byte(html))
	})
	return nil
}

func isAbsolutePathOrURL(value string) bool {
	return strings.HasPrefix(value, "/") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}
