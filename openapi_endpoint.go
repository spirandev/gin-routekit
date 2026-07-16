package routekit

import (
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
)

func (ar *AppRouter) RegisterOpenAPI(engine *gin.Engine, config OpenAPIConfig) error {
	doc, err := ar.BuildOpenAPI(config)
	if err != nil {
		return err
	}

	payload, err := MarshalOpenAPI(doc)
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

func MarshalOpenAPI(document *OpenAPIDocument) ([]byte, error) {
	if document == nil {
		return nil, errors.New("OpenAPI document must not be nil")
	}
	return json.MarshalIndent(document, "", "  ")
}

func serveOpenAPI(payload []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "application/json")
		c.Header("Cache-Control", "no-store")
		_, _ = c.Writer.Write(payload)
	}
}
