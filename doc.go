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
	Deprecated  bool
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
	Deprecated          bool
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

type DocumentationContribution interface {
	applyDocumentation(*MiddlewareMetadata)
}

func (metadata MiddlewareMetadata) applyDocumentation(target *MiddlewareMetadata) {
	mergeMiddlewareMetadata(target, metadata)
}

type documentationContributionFunc func(*MiddlewareMetadata)

func (contribution documentationContributionFunc) applyDocumentation(target *MiddlewareMetadata) {
	contribution(target)
}

func mergeMiddlewareMetadata(target *MiddlewareMetadata, metadata MiddlewareMetadata) {
	metadata = cloneMiddlewareMetadata(metadata)
	target.Profiles = append(target.Profiles, metadata.Profiles...)
	target.Parameters = append(target.Parameters, metadata.Parameters...)
	target.Responses = append(target.Responses, metadata.Responses...)
	target.Security = append(target.Security, metadata.Security...)
}

func RequireSecurityScheme(name string, scopes ...string) DocumentationContribution {
	copiedScopes := append([]string(nil), scopes...)
	return documentationContributionFunc(func(target *MiddlewareMetadata) {
		target.Security = append(target.Security, OpenAPISecurityRequirement{name: append([]string(nil), copiedScopes...)})
	})
}

// RequireOperationHeader accepts either a DocParam or name, type, required,
// description arguments. RequiresHeader is its concise alias.
func RequireOperationHeader(arguments ...any) DocumentationContribution {
	parameter := contributionHeader(arguments...)
	return documentationContributionFunc(func(target *MiddlewareMetadata) {
		target.Parameters = append(target.Parameters, cloneDocParam(parameter))
	})
}

func RequiresHeader(arguments ...any) DocumentationContribution {
	return RequireOperationHeader(arguments...)
}

func contributionHeader(arguments ...any) DocParam {
	if len(arguments) == 1 {
		if parameter, ok := arguments[0].(DocParam); ok {
			parameter.In = DocParamInHeader
			return cloneDocParam(parameter)
		}
	}
	parameter := DocParam{In: DocParamInHeader, Type: "string"}
	if len(arguments) == 2 {
		parameter.Name, _ = arguments[0].(string)
		parameter.Description, _ = arguments[1].(string)
		parameter.Required = true
		return parameter
	}
	if len(arguments) > 0 {
		parameter.Name, _ = arguments[0].(string)
	}
	if len(arguments) > 1 {
		parameter.Type, _ = arguments[1].(string)
	}
	if len(arguments) > 2 {
		parameter.Required, _ = arguments[2].(bool)
	}
	if len(arguments) > 3 {
		parameter.Description, _ = arguments[3].(string)
	}
	return parameter
}

func RequiresSecurity(requirements ...OpenAPISecurityRequirement) DocumentationContribution {
	copied := cloneSecurityRequirements(requirements)
	return documentationContributionFunc(func(target *MiddlewareMetadata) {
		target.Security = append(target.Security, cloneSecurityRequirements(copied)...)
	})
}

func RespondsWith(responses ...DocResponse) DocumentationContribution {
	copied := cloneDocResponses(responses)
	return documentationContributionFunc(func(target *MiddlewareMetadata) {
		target.Responses = append(target.Responses, cloneDocResponses(copied)...)
	})
}

func UsesProfiles(names ...string) DocumentationContribution {
	copied := append([]string(nil), names...)
	return documentationContributionFunc(func(target *MiddlewareMetadata) {
		target.Profiles = append(target.Profiles, copied...)
	})
}

type RouteSecurityPredicate func(route Route, handler Handler) bool

type RouteSecurityRule struct {
	predicate     RouteSecurityPredicate
	contributions []DocumentationContribution
}

func NewRouteSecurityRule(predicate RouteSecurityPredicate, contributions ...DocumentationContribution) RouteSecurityRule {
	metadata := metadataFromContributions(contributions)
	return RouteSecurityRule{predicate: predicate, contributions: []DocumentationContribution{metadata}}
}

func WhenAuthenticated(contributions ...DocumentationContribution) RouteSecurityRule {
	return NewRouteSecurityRule(func(_ Route, handler Handler) bool {
		return handler.IsAuthentication == nil || *handler.IsAuthentication
	}, contributions...)
}

func WhenM2M(contributions ...DocumentationContribution) RouteSecurityRule {
	return NewRouteSecurityRule(func(_ Route, handler Handler) bool {
		return handler.IsM2M != nil && *handler.IsM2M
	}, contributions...)
}

func WhenIntegration(contributions ...DocumentationContribution) RouteSecurityRule {
	return NewRouteSecurityRule(func(_ Route, handler Handler) bool {
		return handler.IsIntegration != nil && *handler.IsIntegration
	}, contributions...)
}

func (rule RouteSecurityRule) metadata(route Route, handler Handler) (MiddlewareMetadata, bool) {
	if rule.predicate == nil || !rule.predicate(route, handler) {
		return MiddlewareMetadata{}, false
	}
	var metadata MiddlewareMetadata
	for _, contribution := range rule.contributions {
		if contribution != nil {
			contribution.applyDocumentation(&metadata)
		}
	}
	return cloneMiddlewareMetadata(metadata), true
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
