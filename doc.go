package routekit

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
