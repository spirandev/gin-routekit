// Package jsonendpoint provides optional typed JSON handlers whose runtime
// behavior and routekit Contract come from the same configuration.
package jsonendpoint

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
)

const defaultBodyLimit int64 = 1 << 20

var (
	// ErrResponseAlreadyWritten is recorded on the Gin context in strict mode
	// when a typed handler writes its own response.
	ErrResponseAlreadyWritten = errors.New("typed handler wrote directly to the response writer")
	// ErrNilResponse is retained for source compatibility. Nil JSON values now
	// follow encoding/json and serialize as null.
	// Deprecated: nil JSON responses are valid.
	ErrNilResponse = errors.New("typed handler returned a nil JSON response")
)

type Handler[Request, Response any] func(*gin.Context, Request) (Response, error)

type Validator[Request any] func(*gin.Context, Request) error

type Option interface {
	apply(*config)
}

type optionFunc func(*config)

func (option optionFunc) apply(config *config) {
	option(config)
}

type validatorConfig struct {
	typ      reflect.Type
	validate func(*gin.Context, any) error
}

type config struct {
	successStatus      int
	successDescription string
	bodyRequired       bool
	bodyLimit          int64
	strictWriter       bool
	validators         []validatorConfig
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type defaultErrorPolicy struct {
	status      int
	description string
	message     string
}

func WithSuccess(status int, description string) Option {
	return optionFunc(func(config *config) {
		config.successStatus = status
		config.successDescription = description
	})
}

func WithOptionalBody() Option {
	return optionFunc(func(config *config) {
		config.bodyRequired = false
	})
}

func WithValidator[Request any](validator Validator[Request]) Option {
	return optionFunc(func(config *config) {
		if validator == nil {
			panic("jsonendpoint: validator must not be nil")
		}
		config.validators = append(config.validators, validatorConfig{
			typ: reflect.TypeOf((*Request)(nil)).Elem(),
			validate: func(context *gin.Context, value any) error {
				return validator(context, value.(Request))
			},
		})
	})
}

func WithBodyLimit(bytes int64) Option {
	return optionFunc(func(config *config) {
		config.bodyLimit = bytes
	})
}

func WithStrictWriter() Option {
	return optionFunc(func(config *config) {
		config.strictWriter = true
	})
}

func Handle[Request, Response any](
	group *routekit.RouterGroup,
	method string,
	path string,
	handler Handler[Request, Response],
	description string,
	routeID int32,
	options ...Option,
) *routekit.RouteConfig {
	if group == nil {
		panic("jsonendpoint: group must not be nil")
	}
	if handler == nil {
		panic("jsonendpoint: handler must not be nil")
	}

	config := defaultConfig()
	for _, option := range options {
		if option == nil {
			panic("jsonendpoint: option must not be nil")
		}
		option.apply(&config)
	}
	if config.successDescription == "" {
		config.successDescription = http.StatusText(config.successStatus)
	}

	requestType := reflect.TypeOf((*Request)(nil)).Elem()
	responseType := reflect.TypeOf((*Response)(nil)).Elem()
	validateConfig(config, method, requestType, responseType)

	bindingErrors := defaultErrorPolicy{
		status:      http.StatusBadRequest,
		description: "Bad Request",
		message:     "invalid request",
	}
	handlerErrors := defaultErrorPolicy{
		status:      http.StatusInternalServerError,
		description: "Internal Server Error",
		message:     "internal server error",
	}

	adapter := func(context *gin.Context) {
		request, err := bindRequest[Request](context, config)
		if err != nil {
			_ = context.Error(fmt.Errorf("bind typed request: %w", err))
			bindingErrors.respond(context)
			return
		}

		response, err := handler(context, request)
		if context.Writer.Written() {
			if err != nil {
				_ = context.Error(fmt.Errorf("run typed handler: %w", err))
			}
			if config.strictWriter {
				_ = context.Error(ErrResponseAlreadyWritten)
			}
			return
		}
		if err != nil {
			_ = context.Error(fmt.Errorf("run typed handler: %w", err))
			handlerErrors.respond(context)
			return
		}
		if isNoBodyStatus(config.successStatus) {
			context.Status(config.successStatus)
			return
		}
		context.Header("Content-Type", "application/json; charset=utf-8")
		payload, err := json.Marshal(response)
		if err != nil {
			_ = context.Error(fmt.Errorf("serialize typed response: %w", err))
			handlerErrors.respond(context)
			return
		}
		context.Data(config.successStatus, "application/json; charset=utf-8", payload)
	}

	contract := deriveContract[Request, Response](config, bindingErrors, handlerErrors)
	return group.Handle(method, path, adapter, description, routeID).Contract(contract)
}

func defaultConfig() config {
	return config{
		successStatus:      http.StatusOK,
		successDescription: "OK",
		bodyRequired:       true,
		bodyLimit:          defaultBodyLimit,
	}
}

func validateConfig(config config, method string, requestType, responseType reflect.Type) {
	if _, ok := routekit.ValidMethods[method]; !ok || method == http.MethodConnect || method == http.MethodTrace {
		panic(fmt.Sprintf("jsonendpoint: unsupported HTTP method %q", method))
	}
	if method == http.MethodHead {
		panic("jsonendpoint: HEAD cannot use a JSON response body")
	}
	if config.successStatus < 200 || config.successStatus > 299 {
		panic(fmt.Sprintf("jsonendpoint: success status %d must be between 200 and 299", config.successStatus))
	}
	if config.bodyLimit <= 0 || config.bodyLimit == math.MaxInt64 {
		panic("jsonendpoint: body limit must be between 1 and math.MaxInt64-1")
	}
	for _, validator := range config.validators {
		if validator.typ != requestType {
			panic(fmt.Sprintf("jsonendpoint: validator type %s does not match request type %s", validator.typ, requestType))
		}
	}

}

func bindRequest[Request any](context *gin.Context, config config) (Request, error) {
	var request Request
	body, err := readBody(context.Request, config.bodyLimit)
	if err != nil {
		return request, err
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		if config.bodyRequired {
			return request, errors.New("request body is required")
		}
		return validateRequest(context, request, config.validators)
	}
	if !json.Valid(trimmed) {
		return request, errors.New("request body must contain exactly one valid JSON value")
	}
	if !isJSONContentType(context.GetHeader("Content-Type")) {
		return request, errors.New("request Content-Type must be application/json")
	}
	if err := context.ShouldBindJSON(&request); err != nil {
		return request, err
	}
	return validateRequest(context, request, config.validators)
}

func validateRequest[Request any](context *gin.Context, request Request, validators []validatorConfig) (Request, error) {
	for _, validator := range validators {
		if err := validator.validate(context, request); err != nil {
			return request, err
		}
	}
	return request, nil
}

func readBody(request *http.Request, limit int64) ([]byte, error) {
	if request == nil || request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, limit+1))
	_ = request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("request body exceeds %d bytes", limit)
	}
	return body, nil
}

func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func isNoBodyStatus(status int) bool {
	return status == http.StatusNoContent || status == http.StatusResetContent
}

func (policy defaultErrorPolicy) respond(context *gin.Context) {
	context.Header("Content-Type", "application/json; charset=utf-8")
	context.JSON(policy.status, ErrorResponse{Error: policy.message})
}

func (policy defaultErrorPolicy) describe() routekit.DocResponse {
	return routekit.ResponseOf[ErrorResponse](policy.status, policy.description)
}

func deriveContract[Request, Response any](config config, bindingErrors, handlerErrors defaultErrorPolicy) routekit.Contract {
	options := []routekit.ContractOption{
		routekit.WithRequestContentType("application/json"),
		routekit.WithResponseContentType("application/json"),
		routekit.WithAdditionalResponse(bindingErrors.describe()),
		routekit.WithAdditionalResponse(handlerErrors.describe()),
	}
	if !config.bodyRequired {
		options = append(options, routekit.WithOptionalRequestBody())
	}
	if isNoBodyStatus(config.successStatus) {
		options = append(options, routekit.WithoutResponseBody())
	}
	return routekit.JSONRequestContractOf[Request, Response](config.successStatus, config.successDescription, options...)
}
