// Package jsonendpoint provides optional typed JSON handlers whose runtime
// behavior and routekit Contract come from the same configuration.
package jsonendpoint

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
)

const defaultBodyLimit int64 = 1 << 20

var (
	// ErrResponseAlreadyWritten is recorded on the Gin context in strict mode
	// when a typed handler writes its own response.
	ErrResponseAlreadyWritten = errors.New("typed handler wrote directly to the response writer")
	// ErrNilResponse is recorded when a nil slice or map cannot match its
	// non-nullable response schema.
	ErrNilResponse      = errors.New("typed handler returned a nil JSON response")
	jsonMarshalerType   = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textMarshalerType   = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	timeType            = reflect.TypeOf(time.Time{})
	rawMessageType      = reflect.TypeOf(json.RawMessage{})
	jsonNumberType      = reflect.TypeOf(json.Number(""))
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
		if isNilJSONValue(response) {
			_ = context.Error(ErrNilResponse)
			handlerErrors.respond(context)
			return
		}
		if err := validateJSONResponseValue(reflect.ValueOf(response), responseType, false, map[visit]bool{}); err != nil {
			_ = context.Error(fmt.Errorf("validate typed response: %w", err))
			handlerErrors.respond(context)
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
	validateRequestType(requestType)
	validateResponseType(responseType)
	if err := validateJSONContractType(requestType, true, map[reflect.Type]bool{}); err != nil {
		panic("jsonendpoint: request type: " + err.Error())
	}
	if err := validateJSONContractType(responseType, false, map[reflect.Type]bool{}); err != nil {
		panic("jsonendpoint: response type: " + err.Error())
	}
	for _, validator := range config.validators {
		if validator.typ != requestType {
			panic(fmt.Sprintf("jsonendpoint: validator type %s does not match request type %s", validator.typ, requestType))
		}
	}

}

func validateRequestType(requestType reflect.Type) {
	switch requestType.Kind() {
	case reflect.Struct, reflect.Slice:
		return
	case reflect.Pointer:
		if requestType.Elem().Kind() != reflect.Struct {
			panic(fmt.Sprintf("jsonendpoint: request type %s must point to a struct", requestType))
		}
	case reflect.Map:
		if requestType.Key().Kind() != reflect.String {
			panic(fmt.Sprintf("jsonendpoint: request map type %s must have string keys", requestType))
		}
	default:
		panic(fmt.Sprintf("jsonendpoint: unsupported request type %s", requestType))
	}
}

func validateResponseType(responseType reflect.Type) {
	switch responseType.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Func, reflect.Chan, reflect.Complex64, reflect.Complex128, reflect.UnsafePointer:
		panic(fmt.Sprintf("jsonendpoint: unsupported response type %s", responseType))
	case reflect.Map:
		if responseType.Key().Kind() != reflect.String {
			panic(fmt.Sprintf("jsonendpoint: response map type %s must have string keys", responseType))
		}
	}
}

func validateJSONContractType(typ reflect.Type, request bool, stack map[reflect.Type]bool) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == timeType {
		return nil
	}
	if typ == rawMessageType {
		return errors.New("json.RawMessage does not have a fixed reflected schema")
	}
	if typ == jsonNumberType {
		return errors.New("json.Number is encoded as a number but reflected as a string")
	}
	if typ.Name() == "UUID" && typ.PkgPath() != "" && typ.Kind() != reflect.String {
		return fmt.Errorf("type %s is reflected as string/uuid but has kind %s", typ, typ.Kind())
	}
	if stack[typ] {
		if typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
			return fmt.Errorf("recursive container type %s is not supported", typ)
		}
		return nil
	}
	if hasCustomJSONRepresentation(typ, request) {
		return fmt.Errorf("type %s has a custom JSON representation", typ)
	}

	stack[typ] = true
	defer delete(stack, typ)
	switch typ.Kind() {
	case reflect.Interface:
		return fmt.Errorf("interface type %s does not have a fixed schema", typ)
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return nil
	case reflect.Slice, reflect.Array:
		if typ.Elem().Kind() == reflect.Uint8 {
			return fmt.Errorf("byte sequence type %s is encoded as a string", typ)
		}
		return validateJSONContractType(typ.Elem(), request, stack)
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return fmt.Errorf("map type %s must have string keys", typ)
		}
		return validateJSONContractType(typ.Elem(), request, stack)
	case reflect.Struct:
		jsonNames := map[string]string{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Anonymous && !field.IsExported() {
				return fmt.Errorf("unexported anonymous field %s.%s is not supported", typ, field.Name)
			}
			if !field.IsExported() {
				continue
			}
			jsonName, jsonOptions := splitJSONTag(field.Tag.Get("json"))
			if jsonName == "-" {
				continue
			}
			if field.Anonymous && jsonName == "" {
				return fmt.Errorf("anonymous field %s.%s requires an explicit json tag", typ, field.Name)
			}
			if jsonName == "" {
				jsonName = field.Name
			}
			if existing, ok := jsonNames[jsonName]; ok {
				return fmt.Errorf("fields %s.%s and %s.%s use the same JSON name %q", typ, existing, typ, field.Name, jsonName)
			}
			jsonNames[jsonName] = field.Name
			if jsonOptions["string"] {
				return fmt.Errorf("field %s.%s uses unsupported json string encoding", typ, field.Name)
			}
			if err := validateJSONContractType(field.Type, request, stack); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("kind %s in type %s is not supported by schema reflection", typ.Kind(), typ)
	}
}

func hasCustomJSONRepresentation(typ reflect.Type, request bool) bool {
	interfaces := []reflect.Type{jsonMarshalerType, textMarshalerType}
	if request {
		interfaces = []reflect.Type{jsonUnmarshalerType, textUnmarshalerType}
	}
	for _, iface := range interfaces {
		if typ.Implements(iface) || (typ.Kind() != reflect.Pointer && reflect.PointerTo(typ).Implements(iface)) {
			return true
		}
	}
	return false
}

func splitJSONTag(raw string) (string, map[string]bool) {
	parts := strings.Split(raw, ",")
	options := map[string]bool{}
	for _, option := range parts[1:] {
		options[strings.TrimSpace(option)] = true
	}
	return parts[0], options
}

func bindRequest[Request any](context *gin.Context, config config) (Request, error) {
	request := newRequest[Request]()
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
	if bytes.Equal(trimmed, []byte("null")) {
		return request, errors.New("JSON null is not a valid request body")
	}
	if !json.Valid(trimmed) {
		return request, errors.New("request body must contain exactly one valid JSON value")
	}
	var raw any
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return request, err
	}
	if err := validateJSONRequestValue(raw, reflect.TypeOf((*Request)(nil)).Elem(), false); err != nil {
		return request, err
	}
	if !isJSONContentType(context.GetHeader("Content-Type")) {
		return request, errors.New("request Content-Type must be application/json")
	}
	if err := context.ShouldBindJSON(bindingTarget(&request)); err != nil {
		return request, err
	}
	return validateRequest(context, request, config.validators)
}

func newRequest[Request any]() Request {
	var request Request
	typ := reflect.TypeOf((*Request)(nil)).Elem()
	if typ.Kind() != reflect.Pointer {
		return request
	}
	value := reflect.New(typ.Elem())
	if value.Type() != typ {
		value = value.Convert(typ)
	}
	reflect.ValueOf(&request).Elem().Set(value)
	return request
}

func bindingTarget[Request any](request *Request) any {
	typ := reflect.TypeOf((*Request)(nil)).Elem()
	if typ.Kind() == reflect.Pointer {
		return any(*request)
	}
	return request
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

func isNilJSONValue(value any) bool {
	reflected := reflect.ValueOf(value)
	return reflected.IsValid() && (reflected.Kind() == reflect.Map || reflected.Kind() == reflect.Slice) && reflected.IsNil()
}

func isNoBodyStatus(status int) bool {
	return status == http.StatusNoContent || status == http.StatusResetContent
}

type visit struct {
	typ reflect.Type
	ptr uintptr
}

func validateJSONResponseValue(value reflect.Value, typ reflect.Type, nullable bool, seen map[visit]bool) error {
	if !value.IsValid() {
		return nil
	}
	if typ.Kind() == reflect.Pointer {
		if value.IsNil() {
			if !nullable {
				return fmt.Errorf("nil pointer %s is not nullable at this schema location", typ)
			}
			return nil
		}
		key := visit{typ: typ, ptr: value.Pointer()}
		if seen[key] {
			return nil
		}
		seen[key] = true
		defer delete(seen, key)
		return validateJSONResponseValue(value.Elem(), typ.Elem(), nullable, seen)
	}
	if typ == timeType {
		return nil
	}
	switch typ.Kind() {
	case reflect.Slice:
		if value.IsNil() {
			return fmt.Errorf("nil slice %s is not nullable in the reflected schema", typ)
		}
		for i := 0; i < value.Len(); i++ {
			if err := validateJSONResponseValue(value.Index(i), typ.Elem(), false, seen); err != nil {
				return err
			}
		}
	case reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if err := validateJSONResponseValue(value.Index(i), typ.Elem(), false, seen); err != nil {
				return err
			}
		}
	case reflect.Map:
		if value.IsNil() {
			return fmt.Errorf("nil map %s is not nullable in the reflected schema", typ)
		}
		iterator := value.MapRange()
		for iterator.Next() {
			if err := validateJSONResponseValue(iterator.Value(), typ.Elem(), false, seen); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			jsonName, options := splitJSONTag(field.Tag.Get("json"))
			if jsonName == "-" {
				continue
			}
			fieldValue := value.Field(i)
			if options["omitempty"] && isEmptyJSONValue(fieldValue) {
				continue
			}
			if err := validateJSONResponseValue(fieldValue, field.Type, fieldSchemaAllowsNull(field.Type), seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateJSONRequestValue(value any, typ reflect.Type, nullable bool) error {
	if typ.Kind() == reflect.Pointer {
		if value == nil {
			if !nullable {
				return fmt.Errorf("null pointer %s is not nullable at this schema location", typ)
			}
			return nil
		}
		return validateJSONRequestValue(value, typ.Elem(), nullable)
	}
	if value == nil {
		if !nullable {
			return fmt.Errorf("null %s is not nullable at this schema location", typ)
		}
		return nil
	}
	switch typ.Kind() {
	case reflect.Slice, reflect.Array:
		items, ok := value.([]any)
		if !ok {
			return nil
		}
		for _, item := range items {
			if err := validateJSONRequestValue(item, typ.Elem(), false); err != nil {
				return err
			}
		}
	case reflect.Map:
		items, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		for _, item := range items {
			if err := validateJSONRequestValue(item, typ.Elem(), false); err != nil {
				return err
			}
		}
	case reflect.Struct:
		if typ == timeType {
			return nil
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			jsonName, _ := splitJSONTag(field.Tag.Get("json"))
			if jsonName == "-" {
				continue
			}
			if jsonName == "" {
				jsonName = field.Name
			}
			fieldValue, exists := object[jsonName]
			if !exists {
				continue
			}
			if err := validateJSONRequestValue(fieldValue, field.Type, fieldSchemaAllowsNull(field.Type)); err != nil {
				return err
			}
		}
	}
	return nil
}

func fieldSchemaAllowsNull(typ reflect.Type) bool {
	if typ.Kind() != reflect.Pointer {
		return false
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ == timeType || typ.Kind() != reflect.Struct
}

func isEmptyJSONValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return value.Len() == 0
	case reflect.Bool:
		return !value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return value.IsNil()
	}
	return false
}

func (policy defaultErrorPolicy) respond(context *gin.Context) {
	context.Header("Content-Type", "application/json; charset=utf-8")
	context.JSON(policy.status, ErrorResponse{Error: policy.message})
}

func (policy defaultErrorPolicy) describe() routekit.DocResponse {
	return routekit.DocResponse{
		Status:      policy.status,
		Description: policy.description,
		Schema:      routekit.SchemaOf[ErrorResponse](),
		ContentType: "application/json",
	}
}

func deriveContract[Request, Response any](config config, bindingErrors, handlerErrors defaultErrorPolicy) routekit.Contract {
	success := routekit.DocResponse{
		Status:      config.successStatus,
		Description: config.successDescription,
		ContentType: "application/json",
	}
	if !isNoBodyStatus(config.successStatus) {
		success.Schema = routekit.SchemaOf[Response]()
	}
	return routekit.Contract{
		RequestBody: &routekit.DocBody{
			Required:    config.bodyRequired,
			Schema:      routekit.SchemaOf[Request](),
			ContentType: "application/json",
		},
		Responses: []routekit.DocResponse{success, bindingErrors.describe(), handlerErrors.describe()},
	}
}
