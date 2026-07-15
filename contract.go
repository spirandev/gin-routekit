package routekit

type Contract struct {
	Profiles    []string
	Parameters  []DocParam
	RequestBody *DocBody
	Responses   []DocResponse
}

type ContractOption func(*contractOptions)

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
}

func WithOptionalRequestBody() ContractOption {
	return func(options *contractOptions) {
		options.requestRequired = false
	}
}

func WithRequestContentType(contentType string) ContractOption {
	return func(options *contractOptions) {
		options.requestContentType = contentType
	}
}

func WithResponseContentType(contentType string) ContractOption {
	return func(options *contractOptions) {
		options.responseContentType = contentType
	}
}

func WithRequestExample(example any) ContractOption {
	return func(options *contractOptions) {
		options.requestExample = example
	}
}

func WithResponseExample(example any) ContractOption {
	return func(options *contractOptions) {
		options.responseExample = example
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

func JSONContractOf[Request, Response any](status int, description string, opts ...ContractOption) Contract {
	options := contractOptions{requestRequired: true}
	for _, opt := range opts {
		opt(&options)
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

func DescribeJSON[Request, Response any](route *RouteConfig, status int, description string, options ...ContractOption) *RouteConfig {
	return route.Contract(JSONContractOf[Request, Response](status, description, options...))
}
