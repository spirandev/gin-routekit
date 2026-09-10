// Package api holds the demo API definition shared by the runtime server
// (go/main.go) and the build-time OpenAPI exporter (export/main.go).
// Both paths build from the same route snapshot: one source of truth (ADR 0002).
package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
)

type CreateInstanceRequest struct {
	Name string `json:"name" binding:"required"`
	Size string `json:"size" binding:"required,oneof=small medium large"`
}

type InstanceResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   string `json:"size"`
	Status string `json:"status"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type registrarFunc func(engine *gin.Engine) routekit.Route

func (f registrarFunc) Register(engine *gin.Engine) routekit.Route {
	return f(engine)
}

// Registrars returns the route registrars of the demo API.
func Registrars() []routekit.RouteRegistrar {
	return []routekit.RouteRegistrar{
		registrarFunc(registerInstancesV1),
		registrarFunc(registerInstancesV2),
	}
}

// OpenAPIConfig returns the OpenAPI configuration of the demo API.
func OpenAPIConfig() routekit.OpenAPIConfig {
	return routekit.OpenAPIConfig{
		Title:             "Instances API",
		Version:           "2.0.0",
		Description:       "Demo API served by gin-routekit with a self-hosted docs portal",
		BasePath:          "/",
		PathMode:          routekit.FullRegisteredPaths,
		DocumentationMode: routekit.DocumentAll,
	}
}

func registerInstancesV1(engine *gin.Engine) routekit.Route {
	group := routekit.NewRouterGroup(engine, "/api/v1/instances",
		routekit.WithDocumentation(),
		routekit.WithDeprecatedEndpoints(),
		routekit.WithDefaultResponse(500, "Internal Server Error", routekit.SchemaOf[ErrorResponse]()),
	)
	group.GET("", listInstancesV1, "List instances (v1)", 10).
		Section("Instances", "V1").
		Response(200, "OK", routekit.SchemaOf[[]InstanceResponse]())
	group.POST("", createInstanceV1, "Create instance (v1)", 11).
		Section("Instances", "V1").
		Body(routekit.SchemaOf[CreateInstanceRequest]()).
		Response(201, "Created", routekit.SchemaOf[InstanceResponse]())
	return group.Export("instances-v1", 1)
}

func registerInstancesV2(engine *gin.Engine) routekit.Route {
	group := routekit.NewRouterGroup(engine, "/api/v2/instances",
		routekit.WithDocumentation(),
		routekit.WithDefaultResponse(500, "Internal Server Error", routekit.SchemaOf[ErrorResponse]()),
	)
	group.GET("", listInstancesV2, "List instances", 20).
		Section("Instances", "V2").
		Response(200, "OK", routekit.SchemaOf[[]InstanceResponse]())
	group.POST("", createInstanceV2, "Create instance", 21).
		Section("Instances", "V2").
		Body(routekit.SchemaOf[CreateInstanceRequest]()).
		Response(201, "Created", routekit.SchemaOf[InstanceResponse]())
	return group.Export("instances-v2", 1)
}

var (
	instancesV1 = map[string]InstanceResponse{}
	instancesV2 = map[string]InstanceResponse{}
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

func bindInstance(c *gin.Context, version string) (InstanceResponse, bool) {
	var request CreateInstanceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "invalid_request", Message: err.Error()})
		return InstanceResponse{}, false
	}
	instanceSeq++
	return InstanceResponse{
		ID:     fmt.Sprintf("inst-%s-%d", version, instanceSeq),
		Name:   request.Name,
		Size:   request.Size,
		Status: "provisioning",
	}, true
}

func instancesSnapshot(source map[string]InstanceResponse) []InstanceResponse {
	instances := make([]InstanceResponse, 0, len(source))
	for _, instance := range source {
		instances = append(instances, instance)
	}
	return instances
}
