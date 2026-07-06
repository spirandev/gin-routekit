package routekit

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
)

func (ar *AppRouter) RegisterOpenAPI(engine *gin.Engine, config OpenAPIConfig) error {
	if !ar.routesRegistered {
		return errRegisterRoutesRequired
	}
	if len(ar.routes) == 0 {
		return errRegisterRoutesRequired
	}

	doc, err := BuildOpenAPI(ar.routes, config)
	if err != nil {
		return err
	}

	payload, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	ar.openAPICache = payload

	jsonPath := config.JSONPath
	if jsonPath == "" {
		jsonPath = "/openapi.json"
	}

	engine.GET(jsonPath, serveOpenAPI(ar.openAPICache))
	return nil
}

func serveOpenAPI(payload []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "application/json")
		c.Header("Cache-Control", "no-store")
		_, _ = c.Writer.Write(payload)
	}
}
