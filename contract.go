package routekit

type Contract struct {
	Profiles         []string
	Parameters       []DocParam
	RequestBody      *DocBody
	Responses        []DocResponse
	validationIssues []string
}

type ContractOption func(*contractOptions)

// ResponseOption shares the response-related Contract options so content type
// and example configuration stays consistent across all constructors.
type ResponseOption = ContractOption

type JSONDefault struct {
	Status      int
	Description string
	Schema      any
	ContentType string
	Example     any
	Examples    []NamedExample
}

type contractOptions struct {
	requestRequired     bool
	requestContentType  string
	responseContentType string
	requestExample      any
	responseExample     any
	requestExamples     []NamedExample
	responseExamples    []NamedExample
	additionalResponses []DocResponse
	withoutRequestBody  bool
	withoutResponseBody bool
	profiles            []string
	parameters          []DocParam
	requestConfigured   bool
	responseConfigured  bool
}

func WithOptionalRequestBody() ContractOption {
	return func(options *contractOptions) {
		options.requestRequired = false
		options.requestConfigured = true
	}
}

func WithRequestContentType(contentType string) ContractOption {
	return func(options *contractOptions) {
		options.requestContentType = contentType
		options.requestConfigured = true
	}
}

func WithResponseContentType(contentType string) ContractOption {
	return func(options *contractOptions) {
		options.responseContentType = contentType
		options.responseConfigured = true
	}
}

func WithRequestExample(example any) ContractOption {
	return func(options *contractOptions) {
		options.requestExample = example
		options.requestConfigured = true
	}
}

func WithResponseExample(example any) ContractOption {
	return func(options *contractOptions) {
		options.responseExample = example
		options.responseConfigured = true
	}
}

// WithRequestExamples registers named request body examples; serialized as
// the OpenAPI examples map so Swagger UI offers a scenario dropdown. When
// both WithRequestExample and WithRequestExamples are used, the contract
// records a validation issue.
func WithRequestExamples(examples ...NamedExample) ContractOption {
	return func(options *contractOptions) {
		options.requestExamples = append(options.requestExamples, examples...)
		options.requestConfigured = true
	}
}

// WithResponseExamples registers named response examples; serialized as the
// OpenAPI examples map. When both WithResponseExample and WithResponseExamples
// are used, the contract records a validation issue.
func WithResponseExamples(examples ...NamedExample) ContractOption {
	return func(options *contractOptions) {
		options.responseExamples = append(options.responseExamples, examples...)
		options.responseConfigured = true
	}
}

func WithAdditionalResponse(response DocResponse) ContractOption {
	return func(options *contractOptions) {
		options.additionalResponses = append(options.additionalResponses, cloneDocResponse(response))
	}
}

func WithoutRequestBody() ContractOption {
	return func(options *contractOptions) {
		options.withoutRequestBody = true
	}
}

func WithoutResponseBody() ContractOption {
	return func(options *contractOptions) {
		options.withoutResponseBody = true
	}
}

func WithContractProfiles(names ...string) ContractOption {
	return func(options *contractOptions) {
		options.profiles = append(options.profiles, names...)
	}
}

func WithContractParameter(param DocParam) ContractOption {
	return func(options *contractOptions) {
		options.parameters = append(options.parameters, cloneDocParam(param))
	}
}

func JSONRequestContractOf[Request, Response any](status int, description string, opts ...ContractOption) Contract {
	options := contractOptions{requestRequired: true}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	contract := Contract{
		Profiles:   append([]string(nil), options.profiles...),
		Parameters: cloneDocParams(options.parameters),
	}
	if options.requestExample != nil && len(options.requestExamples) > 0 {
		contract.validationIssues = append(contract.validationIssues, "request body declares both WithRequestExample and WithRequestExamples; remove one")
	}
	if options.responseExample != nil && len(options.responseExamples) > 0 {
		contract.validationIssues = append(contract.validationIssues, "response declares both WithResponseExample and WithResponseExamples; remove one")
	}
	if !options.withoutRequestBody {
		contract.RequestBody = &DocBody{
			Required:    options.requestRequired,
			Schema:      SchemaOf[Request](),
			ContentType: options.requestContentType,
			Example:     cloneAny(options.requestExample),
			Examples:    cloneNamedExamples(options.requestExamples),
		}
	}

	response := DocResponse{
		Status:      status,
		Description: description,
		ContentType: options.responseContentType,
		Example:     cloneAny(options.responseExample),
		Examples:    cloneNamedExamples(options.responseExamples),
	}
	if !options.withoutResponseBody {
		response.Schema = SchemaOf[Response]()
	}
	contract.Responses = append(contract.Responses, response)
	contract.Responses = append(contract.Responses, cloneDocResponses(options.additionalResponses)...)
	return contract
}

func JSONResponseContractOf[Response any](status int, description string, opts ...ContractOption) Contract {
	options := contractOptions{requestRequired: true, withoutRequestBody: true}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	contract := contractWithoutRequest[Response](status, description, options, false)
	if options.requestConfigured {
		contract.validationIssues = append(contract.validationIssues, "response-only contract received request-specific options")
	}
	if options.responseExample != nil && len(options.responseExamples) > 0 {
		contract.validationIssues = append(contract.validationIssues, "response declares both WithResponseExample and WithResponseExamples; remove one")
	}
	return contract
}

func EmptyJSONResponseContract(status int, description string, opts ...ContractOption) Contract {
	options := contractOptions{requestRequired: true, withoutRequestBody: true, withoutResponseBody: true}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	contract := contractWithoutRequest[struct{}](status, description, options, true)
	if options.requestConfigured {
		contract.validationIssues = append(contract.validationIssues, "empty response contract received request-specific options")
	}
	if options.responseConfigured {
		contract.validationIssues = append(contract.validationIssues, "empty response contract received body-specific response options")
	}
	return contract
}

func contractWithoutRequest[Response any](status int, description string, options contractOptions, forceEmpty bool) Contract {
	contract := Contract{
		Profiles:   append([]string(nil), options.profiles...),
		Parameters: cloneDocParams(options.parameters),
	}
	response := DocResponse{
		Status: status, Description: description, ContentType: options.responseContentType,
		Example: cloneAny(options.responseExample), Examples: cloneNamedExamples(options.responseExamples),
	}
	if !forceEmpty && !options.withoutResponseBody {
		response.Schema = SchemaOf[Response]()
	}
	contract.Responses = append(contract.Responses, response)
	contract.Responses = append(contract.Responses, cloneDocResponses(options.additionalResponses)...)
	return contract
}

// JSONContractOf is retained as a compatibility alias for the request/response
// constructor.
// Deprecated: use JSONRequestContractOf.
func JSONContractOf[Request, Response any](status int, description string, opts ...ContractOption) Contract {
	return JSONRequestContractOf[Request, Response](status, description, opts...)
}

func ResponseOf[Response any](status int, description string, options ...ResponseOption) DocResponse {
	settings := contractOptions{}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}
	contentType := settings.responseContentType
	if contentType == "" {
		contentType = "application/json"
	}
	return DocResponse{
		Status: status, Description: description, Schema: SchemaOf[Response](),
		ContentType: contentType, Example: cloneAny(settings.responseExample),
		Examples: cloneNamedExamples(settings.responseExamples),
	}
}

func DefaultResponseOf[Response any](status int, description string, options ...ResponseOption) JSONDefault {
	response := ResponseOf[Response](status, description, options...)
	return JSONDefault{
		Status: response.Status, Description: response.Description, Schema: response.Schema,
		ContentType: response.ContentType, Example: response.Example, Examples: response.Examples,
	}
}

func DescribeJSON[Request, Response any](route *RouteConfig, status int, description string, options ...ContractOption) *RouteConfig {
	return route.Contract(JSONRequestContractOf[Request, Response](status, description, options...))
}
