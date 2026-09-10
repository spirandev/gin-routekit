// Command export writes the OpenAPI document of the demo API to openapi.json
// at the example root. It builds from the same route snapshot as the runtime
// server (Option B) - the exported file only feeds the optional build-time
// pipeline (Option A) and is never a second source of truth.
package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"

	api "github.com/spirandev/gin-routekit/examples/docusaurus-portal/go/api"
)

func main() {
	engine := gin.New()
	app := routekit.NewAppRouterFromRegistrars(api.Registrars(), nil)
	if err := app.RegisterRoutes(engine); err != nil {
		log.Fatalf("RegisterRoutes: %v", err)
	}

	document, err := app.BuildOpenAPI(api.OpenAPIConfig())
	if err != nil {
		log.Fatalf("BuildOpenAPI: %v", err)
	}
	payload, err := routekit.MarshalOpenAPI(document)
	if err != nil {
		log.Fatalf("MarshalOpenAPI: %v", err)
	}
	if err := os.WriteFile("openapi.json", payload, 0o644); err != nil {
		log.Fatalf("write openapi.json: %v", err)
	}
	log.Printf("wrote openapi.json (%d bytes)", len(payload))
}
