package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"

	api "github.com/spirandev/gin-routekit/examples/docusaurus-portal/go/api"
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())

	app := routekit.NewAppRouterFromRegistrars(api.Registrars(), nil)
	if err := app.RegisterRoutes(engine); err != nil {
		log.Fatalf("RegisterRoutes: %v", err)
	}
	if err := app.RegisterOpenAPI(engine, api.OpenAPIConfig()); err != nil {
		log.Fatalf("RegisterOpenAPI: %v", err)
	}
	if err := app.RegisterSwaggerUI(engine, routekit.SwaggerUIConfig{
		Path:  "/swagger",
		Title: "Instances API (Swagger UI)",
	}); err != nil {
		log.Fatalf("RegisterSwaggerUI: %v", err)
	}
	if err := app.RegisterStoplightUI(engine, routekit.StoplightUIConfig{
		Path:  "/stoplight",
		Title: "Instances API (Stoplight Elements)",
	}); err != nil {
		log.Fatalf("RegisterStoplightUI: %v", err)
	}
	if err := app.RegisterDocsPortal(engine, routekit.DocsPortalConfig{
		Assets:       portalAssets,
		AssetsPrefix: "portal-build",
	}); err != nil {
		log.Fatalf("RegisterDocsPortal: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("demo listening on http://localhost:%s", port)
	log.Printf("  docs portal    : http://localhost:%s/docs", port)
	log.Printf("  api reference  : http://localhost:%s/docs/api-reference", port)
	log.Printf("  endpoint map   : http://localhost:%s/docs/endpoints", port)
	log.Printf("  openapi.json   : http://localhost:%s/openapi.json", port)
	log.Printf("  stoplight UI   : http://localhost:%s/stoplight", port)
	log.Printf("  swagger UI     : http://localhost:%s/swagger", port)

	if err := engine.Run(":" + port); err != nil {
		log.Fatalf("run: %v", err)
	}
}
