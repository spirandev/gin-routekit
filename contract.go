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
}

type contractOptions struct {
	requestRequired     bool
	requestContentType  string
	responseContentType string
	requestExample      any
	responseExample     any
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
	if !options.withoutRequestBody {
		contract.RequestBody = &DocBody{
			Required:    options.requestRequired,
			Schema:      SchemaOf[Request](),
			ContentType: options.requestContentType,
			Example:     cloneAny(options.requestExample),
		}
	}

	response := DocResponse{
		Status:      status,
		Description: description,
		ContentType: options.responseContentType,
		Example:     cloneAny(options.responseExample),
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
		Example: cloneAny(options.responseExample),
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
	}
}

func DefaultResponseOf[Response any](status int, description string, options ...ResponseOption) JSONDefault {
	response := ResponseOf[Response](status, description, options...)
	return JSONDefault{
		Status: response.Status, Description: response.Description, Schema: response.Schema,
		ContentType: response.ContentType, Example: response.Example,
	}
}

func DescribeJSON[Request, Response any](route *RouteConfig, status int, description string, options ...ContractOption) *RouteConfig {
	return route.Contract(JSONRequestContractOf[Request, Response](status, description, options...))
}
