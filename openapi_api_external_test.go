package routekit_test

import (
	"net/http"
	"testing"

	routekit "github.com/spirandev/gin-routekit"
)

type compileRequest struct {
	Name string `json:"name"`
}
type compileResponse struct {
	ID string `json:"id"`
}

func TestOpenAPIPublicAPICompiles(t *testing.T) {
	var _ func([]routekit.Route, routekit.OpenAPIConfig) (routekit.DiagnosticReport, error) = routekit.ValidateOpenAPI
	var _ func([]routekit.Route, routekit.OpenAPIConfig) (*routekit.OpenAPIDocument, error) = routekit.BuildOpenAPI
	var _ func(*routekit.OpenAPIDocument) ([]byte, error) = routekit.MarshalOpenAPI
	var _ func(*routekit.OpenAPIDocument, routekit.HTTPClientConfig) ([]byte, error) = routekit.MarshalHTTPClient
	var _ func(*routekit.AppRouter, routekit.OpenAPIConfig) (routekit.DiagnosticReport, error) = (*routekit.AppRouter).ValidateOpenAPI
	var _ func(*routekit.AppRouter, routekit.OpenAPIConfig) (*routekit.OpenAPIDocument, error) = (*routekit.AppRouter).BuildOpenAPI
	var _ func(*routekit.AppRouter, routekit.OpenAPIConfig, routekit.HTTPClientConfig) ([]byte, error) = (*routekit.AppRouter).BuildHTTPClient
	_ = routekit.HTTPClientConfig{BaseURLVariable: "baseUrl", BaseURL: "https://api.example.com"}

	requestContract := routekit.JSONRequestContractOf[compileRequest, compileResponse](http.StatusCreated, "Created",
		routekit.WithRequestExamples(routekit.NamedExample{Name: "minimal", Summary: "Minimal", Value: compileRequest{Name: "demo"}}))
	responseContract := routekit.JSONResponseContractOf[compileResponse](http.StatusOK, "OK")
	emptyContract := routekit.EmptyJSONResponseContract(http.StatusNoContent, "No Content")
	response := routekit.ResponseOf[compileResponse](http.StatusOK, "OK", routekit.WithResponseExample(compileResponse{ID: "1"}))
	namedResponse := routekit.ResponseOf[compileResponse](http.StatusOK, "OK",
		routekit.WithResponseExamples(
			routekit.NamedExample{Name: "created", Summary: "Created", Value: compileResponse{ID: "1"}},
			routekit.NamedExample{Name: "external", ExternalValue: "https://example.test/response.json"},
		))
	defaultResponse := routekit.DefaultResponseOf[compileResponse](http.StatusOK, "OK")
	defaults := routekit.WithJSONDefaults(defaultResponse)
	registrations := []routekit.SchemaRegistration{
		routekit.RegisterSchemaAs[compileRequest]("CompileRequest", routekit.ForSchemaRequest()),
		routekit.OverrideSchemaOf[compileResponse](routekit.OpenAPISchema{Type: "object"}, routekit.ForSchemaResponse()),
	}
	contribution := routekit.RequiresHeader("X-Request-ID", "Request identifier")
	rule := routekit.NewRouteSecurityRule(func(routekit.Route, routekit.Handler) bool { return true }, contribution)
	_ = []any{requestContract, responseContract, emptyContract, response, namedResponse, defaults, registrations, rule}
}
