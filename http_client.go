package routekit

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const defaultHTTPClientBaseURLVariable = "baseUrl"

type HTTPClientConfig struct {
	BaseURLVariable string
	BaseURL         string
}

func MarshalHTTPClient(document *OpenAPIDocument, config HTTPClientConfig) ([]byte, error) {
	if document == nil {
		return nil, errors.New("OpenAPI document must not be nil")
	}

	baseURLVariable, err := resolveHTTPClientBaseURLVariable(config.BaseURLVariable)
	if err != nil {
		return nil, err
	}
	baseURL, err := resolveHTTPClientBaseURL(document, config.BaseURL)
	if err != nil {
		return nil, err
	}

	renderer := httpClientRenderer{
		document:        document,
		baseURLVariable: baseURLVariable,
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "@%s = %s\n", baseURLVariable, baseURL)

	paths := make([]string, 0, len(document.Paths))
	for path := range document.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		item := document.Paths[path]
		for _, operation := range orderedHTTPClientOperations(item) {
			if operation.operation == nil {
				continue
			}
			if err := renderer.renderOperation(&builder, path, operation.method, operation.operation); err != nil {
				return nil, err
			}
		}
	}

	return []byte(builder.String()), nil
}

func (ar *AppRouter) BuildHTTPClient(openAPIConfig OpenAPIConfig, httpClientConfig HTTPClientConfig) ([]byte, error) {
	document, err := ar.BuildOpenAPI(openAPIConfig)
	if err != nil {
		return nil, err
	}
	return MarshalHTTPClient(document, httpClientConfig)
}

type httpClientRenderer struct {
	document        *OpenAPIDocument
	baseURLVariable string
}

type httpClientOperation struct {
	method    string
	operation *OpenAPIOperation
}

type httpClientParam struct {
	name        string
	value       string
	placeholder bool
}

type httpClientHeader struct {
	name  string
	value string
}

func orderedHTTPClientOperations(item OpenAPIPathItem) []httpClientOperation {
	return []httpClientOperation{
		{method: "GET", operation: item.Get},
		{method: "POST", operation: item.Post},
		{method: "PUT", operation: item.Put},
		{method: "PATCH", operation: item.Patch},
		{method: "DELETE", operation: item.Delete},
		{method: "HEAD", operation: item.Head},
		{method: "OPTIONS", operation: item.Options},
		{method: "TRACE", operation: item.Trace},
	}
}

func (renderer httpClientRenderer) renderOperation(builder *strings.Builder, path, method string, operation *OpenAPIOperation) error {
	queryParams, headers := renderer.operationParams(operation)
	comments := renderer.applySecurity(operation, &queryParams, &headers)
	mediaType, media, hasBody := selectHTTPClientMediaType(operation.RequestBody)

	sortHTTPClientQueryParams(queryParams)
	sortHTTPClientHeaders(headers)

	requestPath := renderer.renderPath(path, operation)
	requestURL := "{{" + renderer.baseURLVariable + "}}" + requestPath + renderHTTPClientQuery(queryParams)
	title := operation.Summary
	if title == "" {
		title = operation.OperationID
	}
	if title == "" {
		title = method + " " + path
	}
	requestName := sanitizeHTTPClientIdentifier(operation.OperationID, strings.ToLower(method)+"_request")

	builder.WriteString("\n### ")
	builder.WriteString(safeHTTPClientComment(title))
	builder.WriteByte('\n')
	builder.WriteString("# @name ")
	builder.WriteString(requestName)
	builder.WriteByte('\n')
	builder.WriteString(method)
	builder.WriteByte(' ')
	builder.WriteString(requestURL)
	builder.WriteByte('\n')
	builder.WriteString("Accept: application/json\n")
	for _, header := range headers {
		builder.WriteString(header.name)
		builder.WriteString(": ")
		builder.WriteString(header.value)
		builder.WriteByte('\n')
	}
	if hasBody {
		builder.WriteString("Content-Type: ")
		builder.WriteString(mediaType)
		builder.WriteByte('\n')
	}
	for _, comment := range comments {
		builder.WriteString("# ")
		builder.WriteString(safeHTTPClientComment(comment))
		builder.WriteByte('\n')
	}
	if !hasBody {
		return nil
	}

	if isJSONHTTPClientMediaType(mediaType) {
		payload, err := renderer.renderJSONBody(media)
		if err != nil {
			return err
		}
		builder.WriteByte('\n')
		builder.Write(payload)
		builder.WriteByte('\n')
		return nil
	}

	body, hasGeneratedBody, err := renderNonJSONHTTPClientBody(media)
	if err != nil {
		return err
	}
	if hasGeneratedBody {
		builder.WriteByte('\n')
		builder.WriteString(body)
		builder.WriteByte('\n')
		return nil
	}
	builder.WriteString("\n# No example available for ")
	builder.WriteString(safeHTTPClientComment(mediaType))
	builder.WriteString(" body\n")
	return nil
}

func (renderer httpClientRenderer) operationParams(operation *OpenAPIOperation) ([]httpClientParam, []httpClientHeader) {
	queryParams := []httpClientParam{}
	headers := []httpClientHeader{}
	for _, param := range operation.Parameters {
		switch param.In {
		case string(DocParamInQuery):
			queryParams = append(queryParams, httpClientParam{name: param.Name, value: renderer.parameterValue(param, "query"), placeholder: param.Example == nil})
		case string(DocParamInHeader):
			headers = append(headers, httpClientHeader{name: param.Name, value: renderer.parameterValue(param, "header")})
		}
	}
	return queryParams, headers
}

func (renderer httpClientRenderer) renderPath(path string, operation *OpenAPIOperation) string {
	pathParams := map[string]OpenAPIParameter{}
	for _, param := range operation.Parameters {
		if param.In == string(DocParamInPath) {
			pathParams[param.Name] = param
		}
	}

	var builder strings.Builder
	for index := 0; index < len(path); {
		if path[index] != '{' {
			builder.WriteByte(path[index])
			index++
			continue
		}
		end := strings.IndexByte(path[index+1:], '}')
		if end < 0 {
			builder.WriteByte(path[index])
			index++
			continue
		}
		name := path[index+1 : index+1+end]
		param, ok := pathParams[name]
		if !ok || param.Example == nil {
			builder.WriteString("{{")
			builder.WriteString(httpClientVariableName("path", name))
			builder.WriteString("}}")
		} else {
			builder.WriteString(url.PathEscape(serializedHTTPClientValue(param.Example)))
		}
		index += end + 2
	}
	return builder.String()
}

func (renderer httpClientRenderer) parameterValue(param OpenAPIParameter, location string) string {
	if param.Example == nil {
		return "{{" + httpClientVariableName(location, param.Name) + "}}"
	}
	if location == "header" {
		return headerHTTPClientValue(param.Example)
	}
	return serializedHTTPClientValue(param.Example)
}

func (renderer httpClientRenderer) applySecurity(operation *OpenAPIOperation, queryParams *[]httpClientParam, headers *[]httpClientHeader) []string {
	if len(operation.Security) == 0 || len(operation.Security[0]) == 0 {
		return nil
	}
	securitySchemes := map[string]*OpenAPISecurityScheme{}
	if renderer.document.Components != nil && renderer.document.Components.SecuritySchemes != nil {
		securitySchemes = renderer.document.Components.SecuritySchemes
	}

	requirement := operation.Security[0]
	schemeNames := make([]string, 0, len(requirement))
	for name := range requirement {
		schemeNames = append(schemeNames, name)
	}
	sort.Strings(schemeNames)

	querySeen := httpClientQueryNames(*queryParams)
	headerSeen := httpClientHeaderNames(*headers)
	comments := []string{}
	for _, schemeName := range schemeNames {
		scheme := securitySchemes[schemeName]
		if scheme == nil {
			comments = append(comments, fmt.Sprintf("Security scheme %s is not available", schemeName))
			continue
		}
		switch strings.ToLower(scheme.Type) {
		case "http":
			if strings.EqualFold(scheme.Scheme, "bearer") {
				if !headerSeen["authorization"] {
					*headers = append(*headers, httpClientHeader{name: "Authorization", value: "Bearer {{" + httpClientVariableComponent(schemeName, "Auth") + "Token}}"})
					headerSeen["authorization"] = true
				}
				continue
			}
			comments = append(comments, fmt.Sprintf("Security scheme %s uses unsupported HTTP scheme %s", schemeName, scheme.Scheme))
		case "apikey":
			switch strings.ToLower(scheme.In) {
			case "header":
				if scheme.Name == "" {
					comments = append(comments, fmt.Sprintf("Security scheme %s has no header name", schemeName))
					continue
				}
				key := strings.ToLower(scheme.Name)
				if !headerSeen[key] {
					*headers = append(*headers, httpClientHeader{name: scheme.Name, value: "{{" + httpClientVariableComponent(schemeName, "ApiKey") + "ApiKey}}"})
					headerSeen[key] = true
				}
			case "query":
				if scheme.Name == "" {
					comments = append(comments, fmt.Sprintf("Security scheme %s has no query name", schemeName))
					continue
				}
				if !querySeen[scheme.Name] {
					*queryParams = append(*queryParams, httpClientParam{name: scheme.Name, value: "{{" + httpClientVariableComponent(schemeName, "ApiKey") + "ApiKey}}", placeholder: true})
					querySeen[scheme.Name] = true
				}
			default:
				comments = append(comments, fmt.Sprintf("Security scheme %s uses unsupported API key location %s", schemeName, scheme.In))
			}
		case "oauth2", "openidconnect":
			comments = append(comments, fmt.Sprintf("Security scheme %s is not supported by HTTP client generation", schemeName))
		default:
			comments = append(comments, fmt.Sprintf("Security scheme %s has unsupported type %s", schemeName, scheme.Type))
		}
	}
	return comments
}

func (renderer httpClientRenderer) renderJSONBody(media OpenAPIMediaType) ([]byte, error) {
	example := media.Example
	if example == nil {
		example = firstHTTPClientExample(media)
	}
	if example != nil {
		payload, err := json.MarshalIndent(example, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal HTTP client JSON example: %w", err)
		}
		return payload, nil
	}
	placeholder := renderer.schemaPlaceholder(media.Schema, map[string]bool{})
	payload, err := json.MarshalIndent(placeholder, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal HTTP client JSON body: %w", err)
	}
	return payload, nil
}

// firstHTTPClientExample returns the value of the alphabetically first named
// example so generated clients keep a usable body sample when the media type
// only declares examples. Examples that reference externalValue are skipped.
func firstHTTPClientExample(media OpenAPIMediaType) any {
	if len(media.Examples) == 0 {
		return nil
	}
	names := make([]string, 0, len(media.Examples))
	for name := range media.Examples {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if value := media.Examples[name].Value; value != nil {
			return value
		}
	}
	return nil
}

func (renderer httpClientRenderer) schemaPlaceholder(schema *OpenAPISchema, seen map[string]bool) any {
	if schema == nil {
		return map[string]any{}
	}
	if schema.Ref != "" {
		const prefix = "#/components/schemas/"
		if !strings.HasPrefix(schema.Ref, prefix) || seen[schema.Ref] || renderer.document.Components == nil || renderer.document.Components.Schemas == nil {
			return map[string]any{}
		}
		component := renderer.document.Components.Schemas[strings.TrimPrefix(schema.Ref, prefix)]
		if component == nil {
			return map[string]any{}
		}
		seen[schema.Ref] = true
		value := renderer.schemaPlaceholder(component, seen)
		delete(seen, schema.Ref)
		return value
	}
	if len(schema.AnyOf) > 0 {
		for index := range schema.AnyOf {
			candidate := schema.AnyOf[index]
			if candidate.Type == "null" {
				continue
			}
			return renderer.schemaPlaceholder(&candidate, seen)
		}
		return nil
	}
	if schema.Type == "" {
		if len(schema.Properties) > 0 || schema.AdditionalProperties != nil {
			return renderer.objectSchemaPlaceholder(schema, seen)
		}
		if schema.Items != nil {
			return []any{renderer.schemaPlaceholder(schema.Items, seen)}
		}
		return map[string]any{}
	}
	switch schema.Type {
	case "object":
		return renderer.objectSchemaPlaceholder(schema, seen)
	case "array":
		return []any{renderer.schemaPlaceholder(schema.Items, seen)}
	case "string":
		return ""
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "null":
		return nil
	default:
		return map[string]any{}
	}
}

func (renderer httpClientRenderer) objectSchemaPlaceholder(schema *OpenAPISchema, seen map[string]bool) any {
	object := map[string]any{}
	propertyNames := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		propertyNames = append(propertyNames, name)
	}
	sort.Strings(propertyNames)
	for _, name := range propertyNames {
		property := schema.Properties[name]
		object[name] = renderer.schemaPlaceholder(&property, seen)
	}
	if len(object) == 0 && schema.AdditionalProperties != nil {
		object["additionalProperty"] = renderer.schemaPlaceholder(schema.AdditionalProperties, seen)
	}
	return object
}

func selectHTTPClientMediaType(body *OpenAPIRequestBody) (string, OpenAPIMediaType, bool) {
	if body == nil || len(body.Content) == 0 {
		return "", OpenAPIMediaType{}, false
	}
	if media, ok := body.Content["application/json"]; ok {
		return "application/json", media, true
	}
	mediaTypes := make([]string, 0, len(body.Content))
	for mediaType := range body.Content {
		mediaTypes = append(mediaTypes, mediaType)
	}
	sort.Strings(mediaTypes)
	mediaType := mediaTypes[0]
	return mediaType, body.Content[mediaType], true
}

func renderNonJSONHTTPClientBody(media OpenAPIMediaType) (string, bool, error) {
	example := media.Example
	if example == nil {
		example = firstHTTPClientExample(media)
	}
	if example == nil {
		return "", false, nil
	}
	if value, ok := example.(string); ok {
		return value, true, nil
	}
	payload, err := json.MarshalIndent(example, "", "  ")
	if err != nil {
		return "", false, fmt.Errorf("marshal HTTP client body example: %w", err)
	}
	return string(payload), true, nil
}

func resolveHTTPClientBaseURLVariable(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultHTTPClientBaseURLVariable
	}
	if !validHTTPClientIdentifier(name) {
		return "", errors.New("HTTP client base URL variable is invalid")
	}
	return name, nil
}

func resolveHTTPClientBaseURL(document *OpenAPIDocument, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return validateHTTPClientBaseURL(configured)
	}
	for _, server := range document.Servers {
		candidate := strings.TrimSpace(server.URL)
		if isAbsoluteHTTPClientBaseURL(candidate) {
			return validateHTTPClientBaseURL(candidate)
		}
	}
	return "", errors.New("HTTP client base URL must be absolute HTTP(S)")
}

func validateHTTPClientBaseURL(baseURL string) (string, error) {
	if baseURL == "" || containsLineBreak(baseURL) {
		return "", errors.New("HTTP client base URL is invalid")
	}
	if !isAbsoluteHTTPClientBaseURL(baseURL) {
		return "", errors.New("HTTP client base URL must be absolute HTTP(S)")
	}
	return strings.TrimRight(baseURL, "/"), nil
}

func isAbsoluteHTTPClientBaseURL(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func renderHTTPClientQuery(params []httpClientParam) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0, len(params))
	for _, param := range params {
		value := param.value
		if !param.placeholder {
			value = url.QueryEscape(value)
		}
		parts = append(parts, url.QueryEscape(param.name)+"="+value)
	}
	return "?" + strings.Join(parts, "&")
}

func sortHTTPClientQueryParams(params []httpClientParam) {
	sort.SliceStable(params, func(i, j int) bool {
		if params[i].name == params[j].name {
			return params[i].value < params[j].value
		}
		return params[i].name < params[j].name
	})
}

func sortHTTPClientHeaders(headers []httpClientHeader) {
	sort.SliceStable(headers, func(i, j int) bool {
		left := strings.ToLower(headers[i].name)
		right := strings.ToLower(headers[j].name)
		if left == right {
			return headers[i].name < headers[j].name
		}
		return left < right
	})
}

func httpClientQueryNames(params []httpClientParam) map[string]bool {
	seen := map[string]bool{}
	for _, param := range params {
		seen[param.name] = true
	}
	return seen
}

func httpClientHeaderNames(headers []httpClientHeader) map[string]bool {
	seen := map[string]bool{}
	for _, header := range headers {
		seen[strings.ToLower(header.name)] = true
	}
	return seen
}

func serializedHTTPClientValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(payload)
}

func headerHTTPClientValue(value any) string {
	serialized := serializedHTTPClientValue(value)
	if !containsLineBreak(serialized) {
		return serialized
	}
	payload, err := json.Marshal(serialized)
	if err != nil {
		return strings.NewReplacer("\r", " ", "\n", " ").Replace(serialized)
	}
	return string(payload)
}

func isJSONHTTPClientMediaType(mediaType string) bool {
	mediaType = strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0]))
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func safeHTTPClientComment(value string) string {
	value = strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
	return strings.TrimSpace(value)
}

func containsLineBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func validHTTPClientIdentifier(name string) bool {
	if name == "" || containsLineBreak(name) {
		return false
	}
	for index, r := range name {
		if index == 0 {
			if !isHTTPClientIdentifierStart(r) {
				return false
			}
			continue
		}
		if !isHTTPClientIdentifierPart(r) {
			return false
		}
	}
	return true
}

func sanitizeHTTPClientIdentifier(value, fallback string) string {
	value = strings.TrimSpace(value)
	if validHTTPClientIdentifier(value) {
		return value
	}
	var builder strings.Builder
	for _, r := range value {
		if builder.Len() == 0 {
			if isHTTPClientIdentifierStart(r) {
				builder.WriteRune(r)
			} else if isHTTPClientIdentifierPart(r) {
				builder.WriteByte('_')
				builder.WriteRune(r)
			}
			continue
		}
		if isHTTPClientIdentifierPart(r) {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	result := strings.Trim(builder.String(), "_")
	if validHTTPClientIdentifier(result) {
		return result
	}
	if validHTTPClientIdentifier(fallback) {
		return fallback
	}
	return "request"
}

func httpClientVariableName(prefix, value string) string {
	return prefix + httpClientVariableComponent(value, "Value")
}

func httpClientVariableComponent(value, fallback string) string {
	tokens := splitHTTPClientIdentifierTokens(value)
	if len(tokens) == 0 {
		return fallback
	}
	var builder strings.Builder
	for _, token := range tokens {
		builder.WriteString(upperFirstHTTPClientToken(token))
	}
	component := builder.String()
	if component == "" {
		return fallback
	}
	return component
}

func splitHTTPClientIdentifierTokens(value string) []string {
	tokens := []string{}
	var builder strings.Builder
	for _, r := range value {
		if isHTTPClientIdentifierPart(r) {
			builder.WriteRune(r)
			continue
		}
		if builder.Len() > 0 {
			tokens = append(tokens, builder.String())
			builder.Reset()
		}
	}
	if builder.Len() > 0 {
		tokens = append(tokens, builder.String())
	}
	return tokens
}

func upperFirstHTTPClientToken(token string) string {
	if token == "" {
		return token
	}
	first := token[0]
	if first >= 'a' && first <= 'z' {
		return string(first-'a'+'A') + token[1:]
	}
	return token
}

func isHTTPClientIdentifierStart(r rune) bool {
	return r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}

func isHTTPClientIdentifierPart(r rune) bool {
	return isHTTPClientIdentifierStart(r) || r >= '0' && r <= '9'
}
