package routekit

import "reflect"

type DocParamIn string

const (
	DocParamInHeader DocParamIn = "header"
	DocParamInPath   DocParamIn = "path"
	DocParamInQuery  DocParamIn = "query"
)

type DocConfig struct {
	Enabled     *bool
	Summary     string
	Description string
	Tags        []string
	OperationID string
	Profiles    []string

	Headers     []DocParam
	PathParams  []DocParam
	QueryParams []DocParam

	RequestBody *DocBody
	Responses   []DocResponse
}

type DocParam struct {
	Name        string
	In          DocParamIn
	Type        string
	Required    bool
	Description string
	Example     any
}

type DocBody struct {
	Description string
	Required    bool
	Schema      any
	ContentType string
	Example     any
}

type DocResponse struct {
	Status      int
	Description string
	Schema      any
	ContentType string
	Example     any
}

type DocProfile struct {
	Description string
	Headers     []DocParam
	QueryParams []DocParam
	PathParams  []DocParam
	Security    []string
}

type SchemaDescriptor struct {
	Type    reflect.Type
	Example any
}

func SchemaOf[T any]() SchemaDescriptor {
	return SchemaDescriptor{Type: reflect.TypeOf((*T)(nil)).Elem()}
}

func SchemaWithExample[T any](example T) SchemaDescriptor {
	return SchemaDescriptor{
		Type:    reflect.TypeOf((*T)(nil)).Elem(),
		Example: example,
	}
}

type DocumentationDefaults struct {
	Enabled             *bool
	Profiles            []string
	Headers             []DocParam
	PathParams          []DocParam
	QueryParams         []DocParam
	Responses           []DocResponse
	RequestContentType  string
	ResponseContentType string
}

type MiddlewareMetadata struct {
	Profiles   []string
	Parameters []DocParam
	Responses  []DocResponse
	Security   []OpenAPISecurityRequirement
}

type docRemovals struct {
	Profiles   []string
	Responses  []int
	Parameters []paramTombstone
}

type paramTombstone struct {
	In   DocParamIn
	Name string
}
