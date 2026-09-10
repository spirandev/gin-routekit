package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
)

type createInstanceRequest struct {
	Name string `json:"name" binding:"required"`
	Size string `json:"size" binding:"required,oneof=small medium large"`
}

type instanceResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   string `json:"size"`
	Status string `json:"status"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type registrarFunc func(engine *gin.Engine) routekit.Route

func (f registrarFunc) Register(engine *gin.Engine) routekit.Route {
	return f(engine)
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())

	v1 := registrarFunc(func(engine *gin.Engine) routekit.Route {
		group := routekit.NewRouterGroup(engine, "/api/v1/instances",
			routekit.WithDocumentation(),
			routekit.WithDeprecatedEndpoints(),
			routekit.WithDefaultResponse(500, "Internal Server Error", routekit.SchemaOf[errorResponse]()),
		)
		group.GET("", listInstancesV1, "List instances (v1)", 10).
			Section("Instances", "V1").
			Response(200, "OK", routekit.SchemaOf[[]instanceResponse]())
		group.POST("", createInstanceV1, "Create instance (v1)", 11).
			Section("Instances", "V1").
			Body(routekit.SchemaOf[createInstanceRequest]()).
			Response(201, "Created", routekit.SchemaOf[instanceResponse]())
		return group.Export("instances-v1", 1)
	})

	v2 := registrarFunc(func(engine *gin.Engine) routekit.Route {
		group := routekit.NewRouterGroup(engine, "/api/v2/instances",
			routekit.WithDocumentation(),
			routekit.WithDefaultResponse(500, "Internal Server Error", routekit.SchemaOf[errorResponse]()),
		)
		group.GET("", listInstancesV2, "List instances", 20).
			Section("Instances", "V2").
			Response(200, "OK", routekit.SchemaOf[[]instanceResponse]())
		group.POST("", createInstanceV2, "Create instance", 21).
			Section("Instances", "V2").
			Body(routekit.SchemaOf[createInstanceRequest]()).
			Response(201, "Created", routekit.SchemaOf[instanceResponse]())
		return group.Export("instances-v2", 1)
	})

	app := routekit.NewAppRouterFromRegistrars([]routekit.RouteRegistrar{v1, v2}, nil)
	if err := app.RegisterRoutes(engine); err != nil {
		log.Fatalf("RegisterRoutes: %v", err)
	}

	config := routekit.OpenAPIConfig{
		Title:             "Instances API",
		Version:           "2.0.0",
		Description:       "Demo API served by gin-routekit with a self-hosted docs portal",
		BasePath:          "/",
		PathMode:          routekit.FullRegisteredPaths,
		DocumentationMode: routekit.DocumentAll,
	}
	if err := app.RegisterOpenAPI(engine, config); err != nil {
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

var (
	instancesV1 = map[string]instanceResponse{}
	instancesV2 = map[string]instanceResponse{}
	instanceSeq = 0
)

func listInstancesV1(c *gin.Context) {
	c.JSON(http.StatusOK, instancesSnapshot(instancesV1))
}

func createInstanceV1(c *gin.Context) {
	instance, ok := bindInstance(c, "v1")
	if !ok {
		return
	}
	instancesV1[instance.ID] = instance
	c.JSON(http.StatusCreated, instance)
}

func listInstancesV2(c *gin.Context) {
	c.JSON(http.StatusOK, instancesSnapshot(instancesV2))
}

func createInstanceV2(c *gin.Context) {
	instance, ok := bindInstance(c, "v2")
	if !ok {
		return
	}
	instancesV2[instance.ID] = instance
	c.JSON(http.StatusCreated, instance)
}

func bindInstance(c *gin.Context, version string) (instanceResponse, bool) {
	var request createInstanceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Code: "invalid_request", Message: err.Error()})
		return instanceResponse{}, false
	}
	instanceSeq++
	return instanceResponse{
		ID:     fmt.Sprintf("inst-%s-%d", version, instanceSeq),
		Name:   request.Name,
		Size:   request.Size,
		Status: "provisioning",
	}, true
}

func instancesSnapshot(source map[string]instanceResponse) []instanceResponse {
	instances := make([]instanceResponse, 0, len(source))
	for _, instance := range source {
		instances = append(instances, instance)
	}
	return instances
}
