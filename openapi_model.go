package routekit

type OpenAPIDocument struct {
	OpenAPI           string                    `json:"openapi"`
	JSONSchemaDialect string                    `json:"jsonSchemaDialect,omitempty"`
	Info              OpenAPIInfo               `json:"info"`
	Servers           []OpenAPIServer           `json:"servers,omitempty"`
	Paths             OpenAPIPaths              `json:"paths"`
	Components        *OpenAPIComponents        `json:"components,omitempty"`
	Tags              []OpenAPITag              `json:"tags,omitempty"`
	TagGroups         []OpenAPITagGroup         `json:"x-tagGroups,omitempty"`
	RoutekitProfiles  map[string]OpenAPIProfile `json:"x-routekit-profiles,omitempty"`
	RoutekitDocs      *OpenAPIDocsMetadata      `json:"x-routekit-docs,omitempty"`
}

type OpenAPITagGroup struct {
	Name        string   `json:"name"`
	Tags        []string `json:"tags"`
	Description string   `json:"description,omitempty"`
}

type OpenAPIDocsMetadata struct {
	Sections map[string][]string `json:"sections,omitempty"`
}

type OpenAPIProfile struct {
	Description string `json:"description,omitempty"`
}

type OpenAPIInfo struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

type OpenAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type OpenAPITag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type OpenAPIPaths map[string]OpenAPIPathItem

type OpenAPIPathItem struct {
	Get     *OpenAPIOperation `json:"get,omitempty"`
	Post    *OpenAPIOperation `json:"post,omitempty"`
	Put     *OpenAPIOperation `json:"put,omitempty"`
	Patch   *OpenAPIOperation `json:"patch,omitempty"`
	Delete  *OpenAPIOperation `json:"delete,omitempty"`
	Head    *OpenAPIOperation `json:"head,omitempty"`
	Options *OpenAPIOperation `json:"options,omitempty"`
	Trace   *OpenAPIOperation `json:"trace,omitempty"`
}

type OpenAPIOperation struct {
	Tags        []string                     `json:"tags,omitempty"`
	Summary     string                       `json:"summary,omitempty"`
	Description string                       `json:"description,omitempty"`
	OperationID string                       `json:"operationId,omitempty"`
	Parameters  []OpenAPIParameter           `json:"parameters,omitempty"`
	RequestBody *OpenAPIRequestBody          `json:"requestBody,omitempty"`
	Responses   OpenAPIResponses             `json:"responses"`
	Security    []OpenAPISecurityRequirement `json:"security,omitempty"`
	Deprecated  bool                         `json:"deprecated,omitempty"`

	paramConflict    []paramKey       `json:"-"`
	responseConflict []string         `json:"-"`
	schemaReflector  *schemaReflector `json:"-"`
}

type OpenAPIResponses map[string]OpenAPIResponse

type OpenAPIResponse struct {
	Description string                      `json:"description"`
	Content     map[string]OpenAPIMediaType `json:"content,omitempty"`
}

type OpenAPIParameter struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Schema      *OpenAPISchema `json:"schema,omitempty"`
	Example     any            `json:"example,omitempty"`
}

type OpenAPIRequestBody struct {
	Description string                      `json:"description,omitempty"`
	Required    bool                        `json:"required,omitempty"`
	Content     map[string]OpenAPIMediaType `json:"content"`
}

type OpenAPIMediaType struct {
	Schema   *OpenAPISchema            `json:"schema,omitempty"`
	Example  any                       `json:"example,omitempty"`
	Examples map[string]OpenAPIExample `json:"examples,omitempty"`
}

// OpenAPIExample mirrors the OpenAPI Example Object. Exactly one of Value or
// ExternalValue must be set, which the document builder validates.
type OpenAPIExample struct {
	Summary       string `json:"summary,omitempty"`
	Description   string `json:"description,omitempty"`
	Value         any    `json:"value,omitempty"`
	ExternalValue string `json:"externalValue,omitempty"`
}

type OpenAPISchema struct {
	Type                 string                   `json:"type,omitempty"`
	Format               string                   `json:"format,omitempty"`
	Description          string                   `json:"description,omitempty"`
	Deprecated           bool                     `json:"deprecated,omitempty"`
	AnyOf                []OpenAPISchema          `json:"anyOf,omitempty"`
	Items                *OpenAPISchema           `json:"items,omitempty"`
	Properties           map[string]OpenAPISchema `json:"properties,omitempty"`
	Required             []string                 `json:"required,omitempty"`
	Ref                  string                   `json:"$ref,omitempty"`
	AdditionalProperties *OpenAPISchema           `json:"additionalProperties,omitempty"`
}

type OpenAPIComponents struct {
	Schemas         map[string]*OpenAPISchema         `json:"schemas,omitempty"`
	SecuritySchemes map[string]*OpenAPISecurityScheme `json:"securitySchemes,omitempty"`
}

type OpenAPISecurityScheme struct {
	Type             string             `json:"type"`
	Description      string             `json:"description,omitempty"`
	Name             string             `json:"name,omitempty"`
	In               string             `json:"in,omitempty"`
	Scheme           string             `json:"scheme,omitempty"`
	BearerFormat     string             `json:"bearerFormat,omitempty"`
	Flows            *OpenAPIOAuthFlows `json:"flows,omitempty"`
	OpenIDConnectURL string             `json:"openIdConnectUrl,omitempty"`
}

type OpenAPIOAuthFlows struct {
	Implicit          *OpenAPIOAuthFlow `json:"implicit,omitempty"`
	Password          *OpenAPIOAuthFlow `json:"password,omitempty"`
	ClientCredentials *OpenAPIOAuthFlow `json:"clientCredentials,omitempty"`
	AuthorizationCode *OpenAPIOAuthFlow `json:"authorizationCode,omitempty"`
}

type OpenAPIOAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes"`
}

type OpenAPISecurityRequirement map[string][]string

type DocumentationMode string

type SchemaDirection string

const (
	SchemaRequest  SchemaDirection = "request"
	SchemaResponse SchemaDirection = "response"
)

type PathMode string

const (
	PathsRelativeToBase PathMode = "relative"
	FullRegisteredPaths PathMode = "full"
)

const (
	DocumentationModeUnspecified DocumentationMode = ""
	DocumentAll                  DocumentationMode = "all"
	DocumentOptIn                DocumentationMode = "opt-in"
)

type OpenAPIConfig struct {
	Title       string
	Version     string
	Description string
	Servers     []OpenAPIServer
	JSONPath    string
	BasePath    string
	PathMode    PathMode

	DocumentationMode DocumentationMode
	TagGroups         []OpenAPITagGroup
	Defaults          DocumentationDefaults

	// Deprecated: use DocumentationMode.
	EnabledByDefault    bool
	RouteDecorators     []RouteDocDecorator
	Profiles            map[string]DocProfile
	Components          OpenAPIComponents
	SchemaRegistrations []SchemaRegistration
	SecurityRules       []RouteSecurityRule
}

type OpenAPISchemaProvider interface {
	OpenAPISchema() OpenAPISchema
}

type DirectionalOpenAPISchemaProvider interface {
	OpenAPISchemaFor(direction SchemaDirection) OpenAPISchema
}

func BearerSecurityScheme(description string) *OpenAPISecurityScheme {
	return &OpenAPISecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  description,
	}
}

func APIKeyHeaderSecurityScheme(name, description string) *OpenAPISecurityScheme {
	return &OpenAPISecurityScheme{
		Type:        "apiKey",
		In:          "header",
		Name:        name,
		Description: description,
	}
}

func APIKeyQuerySecurityScheme(name, description string) *OpenAPISecurityScheme {
	return &OpenAPISecurityScheme{
		Type:        "apiKey",
		In:          "query",
		Name:        name,
		Description: description,
	}
}

type RouteDocDecorator func(ctx *RouteDocContext) error

type RouteDocContext struct {
	Route     Route
	Handler   Handler
	Operation *OpenAPIOperation
	Config    OpenAPIConfig
}
