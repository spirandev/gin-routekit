package routekit

import (
	"reflect"

	"github.com/gin-gonic/gin"
)

func cloneRoutes(routes []Route) []Route {
	if routes == nil {
		return nil
	}
	cloned := make([]Route, len(routes))
	for i, route := range routes {
		cloned[i] = Route{
			Path:                    route.Path,
			Definition:              route.Definition,
			Group:                   route.Group,
			ApplicationID:           route.ApplicationID,
			Handlers:                cloneHandlers(route.Handlers),
			Middleware:              append([]gin.HandlerFunc(nil), route.Middleware...),
			DocumentationDefaults:   cloneDocumentationDefaults(route.DocumentationDefaults),
			GroupMiddlewareMetadata: cloneMiddlewareMetadataSlice(route.GroupMiddlewareMetadata),
		}
	}
	return cloned
}

func cloneHandlers(handlers []Handler) []Handler {
	if handlers == nil {
		return nil
	}
	cloned := make([]Handler, len(handlers))
	for i, h := range handlers {
		scopes := append([]string(nil), h.Scopes...)
		if h.Scopes != nil && scopes == nil {
			scopes = []string{}
		}
		cloned[i] = Handler{
			Handler:                   h.Handler,
			Middleware:                append([]gin.HandlerFunc(nil), h.Middleware...),
			Method:                    h.Method,
			Path:                      h.Path,
			Definition:                h.Definition,
			RouteId:                   h.RouteId,
			RelativePath:              h.RelativePath,
			IsAuthentication:          cloneBoolPtr(h.IsAuthentication),
			IsAuthorization:           cloneBoolPtr(h.IsAuthorization),
			RequiresClientContext:     cloneBoolPtr(h.RequiresClientContext),
			IsBasic:                   cloneBoolPtr(h.IsBasic),
			IsM2M:                     cloneBoolPtr(h.IsM2M),
			IsSameApplicationRequired: cloneBoolPtr(h.IsSameApplicationRequired),
			IsIntegration:             cloneBoolPtr(h.IsIntegration),
			Scopes:                    scopes,
			Doc:                       cloneDocConfig(h.Doc),
			Contract:                  cloneContract(h.Contract),
			DocRemovals:               cloneDocRemovals(h.DocRemovals),
			MiddlewareMetadata:        cloneMiddlewareMetadataSlice(h.MiddlewareMetadata),
		}
	}
	return cloned
}

func cloneDocConfig(doc *DocConfig) *DocConfig {
	if doc == nil {
		return nil
	}
	enabledCopy := doc.Enabled
	var enabled *bool
	if enabledCopy != nil {
		v := *enabledCopy
		enabled = &v
	}

	cloned := &DocConfig{
		Enabled:     enabled,
		Deprecated:  doc.Deprecated,
		Summary:     doc.Summary,
		Description: doc.Description,
		Tags:        append([]string(nil), doc.Tags...),
		Section:     append([]string(nil), doc.Section...),
		OperationID: doc.OperationID,
		Profiles:    append([]string(nil), doc.Profiles...),
		Headers:     cloneDocParams(doc.Headers),
		PathParams:  cloneDocParams(doc.PathParams),
		QueryParams: cloneDocParams(doc.QueryParams),
	}

	if doc.RequestBody != nil {
		cloned.RequestBody = cloneDocBody(doc.RequestBody)
	}

	cloned.Responses = cloneDocResponses(doc.Responses)

	return cloned
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneDocParam(param DocParam) DocParam {
	param.Example = cloneAny(param.Example)
	return param
}

func cloneDocParams(params []DocParam) []DocParam {
	if params == nil {
		return nil
	}
	cloned := make([]DocParam, len(params))
	for i, param := range params {
		cloned[i] = cloneDocParam(param)
	}
	return cloned
}

func cloneDocBody(body *DocBody) *DocBody {
	if body == nil {
		return nil
	}
	return &DocBody{
		Description: body.Description,
		Required:    body.Required,
		Schema:      cloneSchemaInput(body.Schema),
		ContentType: body.ContentType,
		Example:     cloneAny(body.Example),
		Examples:    cloneNamedExamples(body.Examples),
	}
}

func cloneDocResponse(response DocResponse) DocResponse {
	response.Schema = cloneSchemaInput(response.Schema)
	response.Example = cloneAny(response.Example)
	response.Examples = cloneNamedExamples(response.Examples)
	return response
}

func cloneNamedExamples(examples []NamedExample) []NamedExample {
	if examples == nil {
		return nil
	}
	cloned := make([]NamedExample, len(examples))
	for i, example := range examples {
		cloned[i] = NamedExample{
			Name:          example.Name,
			Summary:       example.Summary,
			Description:   example.Description,
			Value:         cloneAny(example.Value),
			ExternalValue: example.ExternalValue,
		}
	}
	return cloned
}

func cloneDocResponses(responses []DocResponse) []DocResponse {
	if responses == nil {
		return nil
	}
	cloned := make([]DocResponse, len(responses))
	for i, response := range responses {
		cloned[i] = cloneDocResponse(response)
	}
	return cloned
}

func cloneContract(contract *Contract) *Contract {
	if contract == nil {
		return nil
	}
	return &Contract{
		Profiles:         append([]string(nil), contract.Profiles...),
		Parameters:       cloneDocParams(contract.Parameters),
		RequestBody:      cloneDocBody(contract.RequestBody),
		Responses:        cloneDocResponses(contract.Responses),
		validationIssues: append([]string(nil), contract.validationIssues...),
	}
}

func cloneDocumentationDefaults(defaults DocumentationDefaults) DocumentationDefaults {
	return DocumentationDefaults{
		Enabled:             cloneBoolPtr(defaults.Enabled),
		Deprecated:          defaults.Deprecated,
		Profiles:            append([]string(nil), defaults.Profiles...),
		Headers:             cloneDocParams(defaults.Headers),
		PathParams:          cloneDocParams(defaults.PathParams),
		QueryParams:         cloneDocParams(defaults.QueryParams),
		Responses:           cloneDocResponses(defaults.Responses),
		RequestContentType:  defaults.RequestContentType,
		ResponseContentType: defaults.ResponseContentType,
	}
}

func cloneTagGroups(groups []OpenAPITagGroup) []OpenAPITagGroup {
	if groups == nil {
		return nil
	}
	cloned := make([]OpenAPITagGroup, len(groups))
	for index, group := range groups {
		cloned[index] = OpenAPITagGroup{
			Name:        group.Name,
			Tags:        append([]string(nil), group.Tags...),
			Description: group.Description,
		}
	}
	return cloned
}

func cloneMiddlewareMetadata(metadata MiddlewareMetadata) MiddlewareMetadata {
	return MiddlewareMetadata{
		Profiles:   append([]string(nil), metadata.Profiles...),
		Parameters: cloneDocParams(metadata.Parameters),
		Responses:  cloneDocResponses(metadata.Responses),
		Security:   cloneSecurityRequirements(metadata.Security),
	}
}

func cloneMiddlewareMetadataSlice(metadata []MiddlewareMetadata) []MiddlewareMetadata {
	if metadata == nil {
		return nil
	}
	cloned := make([]MiddlewareMetadata, len(metadata))
	for i, item := range metadata {
		cloned[i] = cloneMiddlewareMetadata(item)
	}
	return cloned
}

func cloneSecurityRequirement(requirement OpenAPISecurityRequirement) OpenAPISecurityRequirement {
	if requirement == nil {
		return nil
	}
	cloned := OpenAPISecurityRequirement{}
	for name, scopes := range requirement {
		cloned[name] = append([]string{}, scopes...)
	}
	return cloned
}

func cloneOpenAPISecurityScheme(scheme *OpenAPISecurityScheme) *OpenAPISecurityScheme {
	if scheme == nil {
		return nil
	}
	cloned := *scheme
	if scheme.Flows != nil {
		cloned.Flows = &OpenAPIOAuthFlows{
			Implicit: cloneOpenAPIOAuthFlow(scheme.Flows.Implicit), Password: cloneOpenAPIOAuthFlow(scheme.Flows.Password),
			ClientCredentials: cloneOpenAPIOAuthFlow(scheme.Flows.ClientCredentials), AuthorizationCode: cloneOpenAPIOAuthFlow(scheme.Flows.AuthorizationCode),
		}
	}
	return &cloned
}

func cloneOpenAPIOAuthFlow(flow *OpenAPIOAuthFlow) *OpenAPIOAuthFlow {
	if flow == nil {
		return nil
	}
	cloned := *flow
	if flow.Scopes != nil {
		cloned.Scopes = map[string]string{}
		for name, description := range flow.Scopes {
			cloned.Scopes[name] = description
		}
	}
	return &cloned
}

func cloneSecurityRequirements(requirements []OpenAPISecurityRequirement) []OpenAPISecurityRequirement {
	if requirements == nil {
		return nil
	}
	cloned := make([]OpenAPISecurityRequirement, len(requirements))
	for i, requirement := range requirements {
		cloned[i] = cloneSecurityRequirement(requirement)
	}
	return cloned
}

func cloneDocRemovals(removals docRemovals) docRemovals {
	return docRemovals{
		Profiles:   append([]string(nil), removals.Profiles...),
		Responses:  append([]int(nil), removals.Responses...),
		Parameters: append([]paramTombstone(nil), removals.Parameters...),
	}
}

func cloneSchemaInput(schema any) any {
	switch v := schema.(type) {
	case *OpenAPISchema:
		return cloneOpenAPISchema(v)
	case OpenAPISchema:
		cloned := cloneOpenAPISchema(&v)
		if cloned == nil {
			return OpenAPISchema{}
		}
		return *cloned
	case SchemaDescriptor:
		v.Example = cloneAny(v.Example)
		return v
	case *SchemaDescriptor:
		if v == nil {
			return (*SchemaDescriptor)(nil)
		}
		cloned := *v
		cloned.Example = cloneAny(cloned.Example)
		return &cloned
	default:
		return schema
	}
}

func cloneOpenAPISchema(schema *OpenAPISchema) *OpenAPISchema {
	return cloneOpenAPISchemaSeen(schema, map[*OpenAPISchema]*OpenAPISchema{})
}

func cloneOpenAPISchemaSeen(schema *OpenAPISchema, seen map[*OpenAPISchema]*OpenAPISchema) *OpenAPISchema {
	if schema == nil {
		return nil
	}
	if cloned, exists := seen[schema]; exists {
		return cloned
	}
	cloned := *schema
	seen[schema] = &cloned
	if schema.AnyOf != nil {
		cloned.AnyOf = make([]OpenAPISchema, len(schema.AnyOf))
		for i := range schema.AnyOf {
			item := cloneOpenAPISchemaSeen(&schema.AnyOf[i], seen)
			if item != nil {
				cloned.AnyOf[i] = *item
			}
		}
	}
	cloned.Items = cloneOpenAPISchemaSeen(schema.Items, seen)
	cloned.Required = append([]string(nil), schema.Required...)
	if schema.Properties != nil {
		cloned.Properties = make(map[string]OpenAPISchema, len(schema.Properties))
		for name, property := range schema.Properties {
			propertyCopy := cloneOpenAPISchemaSeen(&property, seen)
			if propertyCopy != nil {
				cloned.Properties[name] = *propertyCopy
			}
		}
	}
	cloned.AdditionalProperties = cloneOpenAPISchemaSeen(schema.AdditionalProperties, seen)
	return &cloned
}

func cloneAny(value any) any {
	return cloneAnySeen(value, map[cloneAnyVisit]any{})
}

type cloneAnyVisit struct {
	kind    reflect.Kind
	typ     reflect.Type
	pointer uintptr
}

func cloneAnySeen(value any, seen map[cloneAnyVisit]any) any {
	switch v := value.(type) {
	case map[string]any:
		visit := cloneAnyVisit{kind: reflect.Map, typ: reflect.TypeOf(v), pointer: uintptr(reflect.ValueOf(v).UnsafePointer())}
		if cloned, exists := seen[visit]; exists {
			return cloned
		}
		cloned := make(map[string]any, len(v))
		seen[visit] = cloned
		for key, item := range v {
			cloned[key] = cloneAnySeen(item, seen)
		}
		return cloned
	case []any:
		visit := cloneAnyVisit{kind: reflect.Slice, typ: reflect.TypeOf(v), pointer: uintptr(reflect.ValueOf(v).UnsafePointer())}
		if cloned, exists := seen[visit]; exists {
			return cloned
		}
		cloned := make([]any, len(v))
		seen[visit] = cloned
		for i, item := range v {
			cloned[i] = cloneAnySeen(item, seen)
		}
		return cloned
	case []string:
		return append([]string(nil), v...)
	case map[string]string:
		cloned := make(map[string]string, len(v))
		for key, item := range v {
			cloned[key] = item
		}
		return cloned
	default:
		return value
	}
}
