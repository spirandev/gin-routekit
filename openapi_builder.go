package routekit

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
)

var errRegisterRoutesRequired = errors.New("RegisterRoutes must be called before OpenAPI operations")

const jsonSchemaDialectOAS31Base = "https://spec.openapis.org/oas/3.1/dialect/base"

func ValidateOpenAPI(routes []Route, config OpenAPIConfig) (DiagnosticReport, error) {
	_, report := buildOpenAPI(routes, config)
	if report.HasErrors() {
		return report, &DiagnosticsError{Report: report}
	}
	return report, nil
}

// BuildOpenAPI builds an OpenAPI 3.1 document from a route snapshot.
func BuildOpenAPI(routes []Route, config OpenAPIConfig) (*OpenAPIDocument, error) {
	document, report := buildOpenAPI(routes, config)
	if report.HasErrors() {
		return nil, &DiagnosticsError{Report: report}
	}
	return document, nil
}

func buildOpenAPI(routes []Route, config OpenAPIConfig) (*OpenAPIDocument, DiagnosticReport) {
	collector := &diagnosticCollector{}
	validateOpenAPIConfig(config, collector)
	reflector := newSchemaReflector(config, collector)
	document := &OpenAPIDocument{
		OpenAPI: "3.1.0", JSONSchemaDialect: jsonSchemaDialectOAS31Base,
		Info:  OpenAPIInfo{Title: config.Title, Description: config.Description, Version: config.Version},
		Paths: OpenAPIPaths{},
	}
	document.Servers = resolveServers(config, collector)

	operationIDs := map[string]bool{}
	tagNames := map[string]bool{}
	usedProfiles := map[string]bool{}
	usedSchemes := map[string]bool{}
	for _, route := range cloneRoutes(routes) {
		for _, handler := range route.Handlers {
			resolved, err := resolveRouteDocumentation(config, route, handler, collector)
			if err != nil {
				for _, resolutionError := range flattenErrors(err) {
					collector.route("documentation.resolve", DiagnosticError, route, handler, "documentation", resolutionError.Error())
				}
			}
			if resolved == nil {
				continue
			}
			if !resolved.Enabled {
				continue
			}
			methodValid := true
			if err := validateMethod(handler.Method); err != nil {
				collector.route("operation.method", DiagnosticError, route, handler, "method", err.Error())
				methodValid = false
			}

			reflector.setContext(route, handler)
			operation := buildOperation(resolved, reflector)
			if err := runDecorators(config, route, handler, operation); err != nil {
				collector.route("decorator.error", DiagnosticError, route, handler, "decorator", err.Error())
			}
			pathKey, pathOK := emittedOpenAPIPath(config, route, handler, collector)
			validateOperation(config, operation, pathKey, route, handler, collector)
			resolveOperationID(operation, route, handler.Method, handler.Path, operationIDs, collector, handler)
			if !contractDeclaresBehavior(handler.Contract) && !docDeclaresBehavior(handler.Doc) {
				collector.route("operation.defaults_only", DiagnosticWarning, route, handler, "documentation", "route is documented only by defaults and declares no Contract or DocConfig behavior")
			}
			if operation.RequestBody != nil && (handler.Method == http.MethodGet || handler.Method == http.MethodDelete) {
				collector.route("operation.body.convention", DiagnosticWarning, route, handler, "requestBody", fmt.Sprintf("%s operation declares an explicit request body", handler.Method))
			}
			for _, profile := range resolved.Profiles {
				usedProfiles[profile] = true
			}
			for _, requirement := range operation.Security {
				for scheme := range requirement {
					usedSchemes[scheme] = true
				}
			}
			for _, tag := range operation.Tags {
				if tag != "" {
					tagNames[tag] = true
				}
			}
			if !pathOK || !methodValid {
				continue
			}
			item := document.Paths[pathKey]
			if err := setMethod(&item, handler.Method, operation); err != nil {
				collector.route("operation.duplicate", DiagnosticError, route, handler, "paths", fmt.Sprintf("path %q: %v", pathKey, err))
				continue
			}
			document.Paths[pathKey] = item
		}
	}

	generatedSchemas := reflector.finalize()
	rewriteDocumentSchemaRefs(document, reflector.finalNames)
	document.Components = buildComponents(config, generatedSchemas, collector)
	validateComponentRefs(document, collector)
	for name := range config.Components.SecuritySchemes {
		if !usedSchemes[name] {
			collector.add(Diagnostic{Code: "security.scheme.unused", Severity: DiagnosticWarning, Location: "components.securitySchemes." + name, Message: fmt.Sprintf("security scheme %q is declared but never used", name)})
		}
	}
	document.Tags = sortedTags(tagNames)
	if len(usedProfiles) > 0 {
		document.RoutekitProfiles = map[string]OpenAPIProfile{}
		for name := range usedProfiles {
			if profile, exists := config.Profiles[name]; exists {
				document.RoutekitProfiles[name] = OpenAPIProfile{Description: profile.Description}
			}
		}
	}
	return document, collector.report()
}

func flattenErrors(err error) []error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var flattened []error
		for _, child := range joined.Unwrap() {
			flattened = append(flattened, flattenErrors(child)...)
		}
		return flattened
	}
	return []error{err}
}

func rewriteDocumentSchemaRefs(document *OpenAPIDocument, names map[string]string) {
	for path, item := range document.Paths {
		for _, operation := range pathItemOperations(item) {
			if operation == nil {
				continue
			}
			for index := range operation.Parameters {
				rewriteSchemaRefs(operation.Parameters[index].Schema, names)
			}
			if operation.RequestBody != nil {
				for media, content := range operation.RequestBody.Content {
					rewriteSchemaRefs(content.Schema, names)
					operation.RequestBody.Content[media] = content
				}
			}
			for status, response := range operation.Responses {
				for media, content := range response.Content {
					rewriteSchemaRefs(content.Schema, names)
					response.Content[media] = content
				}
				operation.Responses[status] = response
			}
		}
		document.Paths[path] = item
	}
}

func validateOpenAPIConfig(config OpenAPIConfig, collector *diagnosticCollector) {
	if config.Title == "" {
		collector.add(Diagnostic{Code: "config.title.required", Severity: DiagnosticError, Location: "config.title", Message: "OpenAPIConfig.Title is required"})
	}
	if config.Version == "" {
		collector.add(Diagnostic{Code: "config.version.required", Severity: DiagnosticError, Location: "config.version", Message: "OpenAPIConfig.Version is required"})
	}
	if err := validateDocumentationMode(config.DocumentationMode); err != nil {
		collector.add(Diagnostic{Code: "config.documentation_mode", Severity: DiagnosticError, Location: "config.documentationMode", Message: err.Error()})
	}
	if config.PathMode != PathsRelativeToBase && config.PathMode != FullRegisteredPaths {
		collector.add(Diagnostic{Code: "config.path_mode", Severity: DiagnosticError, Location: "config.pathMode", Message: fmt.Sprintf("OpenAPIConfig.PathMode must be %q or %q", PathsRelativeToBase, FullRegisteredPaths)})
	}
	if err := validateBasePath(config.BasePath); err != nil {
		collector.add(Diagnostic{Code: "config.base_path", Severity: DiagnosticError, Location: "config.basePath", Message: err.Error()})
	}
}

func validateDocumentationMode(mode DocumentationMode) error {
	switch mode {
	case DocumentationModeUnspecified, DocumentAll, DocumentOptIn:
		return nil
	default:
		return fmt.Errorf("unknown DocumentationMode %q", mode)
	}
}

func validateBasePath(basePath string) error {
	if basePath == "" {
		return errors.New("OpenAPIConfig.BasePath is required")
	}
	if !strings.HasPrefix(basePath, "/") {
		return fmt.Errorf("OpenAPIConfig.BasePath %q must be absolute", basePath)
	}
	if strings.ContainsAny(basePath, "?#") {
		return fmt.Errorf("OpenAPIConfig.BasePath %q must contain only a URL path", basePath)
	}
	if pathpkg.Clean(basePath) != basePath || basePath != "/" && strings.HasSuffix(basePath, "/") {
		return fmt.Errorf("OpenAPIConfig.BasePath %q must be normalized without a trailing slash", basePath)
	}
	return nil
}

func resolveServers(config OpenAPIConfig, collector *diagnosticCollector) []OpenAPIServer {
	servers := append([]OpenAPIServer(nil), config.Servers...)
	if len(servers) == 0 && config.PathMode == PathsRelativeToBase && validateBasePath(config.BasePath) == nil {
		return []OpenAPIServer{{URL: config.BasePath}}
	}
	if validateBasePath(config.BasePath) != nil {
		return servers
	}
	for index, server := range servers {
		parsed, err := url.Parse(server.URL)
		if err != nil || parsed.Path == "" && parsed.Host == "" {
			collector.add(Diagnostic{Code: "server.url", Severity: DiagnosticError, Location: fmt.Sprintf("config.servers[%d]", index), Message: fmt.Sprintf("server URL %q is invalid", server.URL)})
			continue
		}
		serverPath := parsed.EscapedPath()
		if serverPath == "" {
			serverPath = "/"
		}
		if config.PathMode == PathsRelativeToBase && !pathHasPrefix(serverPath, config.BasePath) {
			collector.add(Diagnostic{Code: "server.base_path.missing", Severity: DiagnosticError, Location: fmt.Sprintf("config.servers[%d]", index), Message: fmt.Sprintf("server URL %q does not contain base path %q", server.URL, config.BasePath)})
		}
		if config.PathMode == FullRegisteredPaths && config.BasePath != "/" && pathHasPrefix(serverPath, config.BasePath) {
			collector.add(Diagnostic{Code: "server.base_path.duplicate", Severity: DiagnosticError, Location: fmt.Sprintf("config.servers[%d]", index), Message: fmt.Sprintf("server URL %q repeats base path %q while paths are full", server.URL, config.BasePath)})
		}
	}
	return servers
}

func pathHasPrefix(value, prefix string) bool {
	if prefix == "/" {
		return strings.HasPrefix(value, "/")
	}
	return value == prefix || strings.HasPrefix(value, prefix+"/")
}

func emittedOpenAPIPath(config OpenAPIConfig, route Route, handler Handler, collector *diagnosticCollector) (string, bool) {
	registered := registeredRoutePath(route.Path, handler.RelativePath)
	if registered == "" {
		registered = "/"
	}
	if !strings.HasPrefix(registered, "/") || strings.ContainsAny(registered, "?#") || pathpkg.Clean(registered) != registered {
		collector.route("path.invalid", DiagnosticError, route, handler, "path", fmt.Sprintf("registered path %q must be absolute and normalized", registered))
		return ginPathToOpenAPI(registered), false
	}
	emitted := registered
	if config.PathMode == PathsRelativeToBase {
		if !pathHasPrefix(registered, config.BasePath) {
			collector.route("path.outside_base", DiagnosticError, route, handler, "path", fmt.Sprintf("registered path %q is outside base path %q", registered, config.BasePath))
			return ginPathToOpenAPI(registered), false
		}
		emitted = strings.TrimPrefix(registered, config.BasePath)
		if emitted == "" {
			emitted = "/"
		}
	}
	return ginPathToOpenAPI(emitted), true
}

func registeredRoutePath(base, relative string) string {
	if relative == "" {
		if base == "" {
			return "/"
		}
		return base
	}
	trailing := strings.HasSuffix(relative, "/")
	joined := pathpkg.Join(base, relative)
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	if trailing && joined != "/" {
		joined += "/"
	}
	return joined
}

func validateMethod(method string) error {
	if _, ok := ValidMethods[method]; !ok {
		return fmt.Errorf("unsupported HTTP method %q", method)
	}
	if method == http.MethodConnect {
		return errors.New("HTTP CONNECT is not representable in an OpenAPI PathItem")
	}
	return nil
}

func buildOperation(resolved *resolvedDocumentation, reflector *schemaReflector) *OpenAPIOperation {
	operation := &OpenAPIOperation{
		Tags: append([]string(nil), resolved.Tags...), Summary: resolved.Summary,
		Description: resolved.Description, OperationID: resolved.OperationID,
		Deprecated: resolved.Deprecated,
		Responses:  OpenAPIResponses{}, schemaReflector: reflector,
	}
	for _, parameter := range resolved.Parameters {
		operation.AddParameter(docParamToOpenAPIParameter(parameter))
	}
	if resolved.RequestBody != nil {
		operation.RequestBody = buildRequestBody(resolved.RequestBody, resolved.RequestContentType, reflector)
	}
	for _, response := range resolved.Responses {
		operation.Responses[statusLabel(response.Status)] = buildResponse(response, resolved.ResponseContentType, reflector)
	}
	for _, requirement := range resolved.Security {
		operation.AddSecurityRequirementFromMap(requirement)
	}
	return operation
}

func docParamToOpenAPIParameter(parameter DocParam) OpenAPIParameter {
	if parameter.Type == "" {
		parameter.Type = "string"
	}
	return OpenAPIParameter{Name: parameter.Name, In: string(parameter.In), Description: parameter.Description, Required: parameter.Required, Schema: &OpenAPISchema{Type: parameter.Type}, Example: cloneAny(parameter.Example)}
}

func buildRequestBody(body *DocBody, defaultContentType string, reflector *schemaReflector) *OpenAPIRequestBody {
	contentType := body.ContentType
	if contentType == "" {
		contentType = defaultContentType
	}
	schema, descriptorExample := reflector.schemaFromInput(body.Schema, SchemaRequest)
	content := map[string]OpenAPIMediaType{}
	if schema != nil {
		if contentType == "" {
			contentType = "application/json"
		}
		example := body.Example
		if example == nil {
			example = descriptorExample
		}
		content[contentType] = OpenAPIMediaType{Schema: schema, Example: cloneAny(example)}
	}
	return &OpenAPIRequestBody{Description: body.Description, Required: body.Required, Content: content}
}

func buildResponse(response DocResponse, defaultContentType string, reflector *schemaReflector) OpenAPIResponse {
	schema, descriptorExample := reflector.schemaFromInput(response.Schema, SchemaResponse)
	result := OpenAPIResponse{Description: response.Description}
	if schema == nil {
		return result
	}
	contentType := response.ContentType
	if contentType == "" {
		contentType = defaultContentType
	}
	if contentType == "" {
		contentType = "application/json"
	}
	example := response.Example
	if example == nil {
		example = descriptorExample
	}
	result.Content = map[string]OpenAPIMediaType{contentType: {Schema: schema, Example: cloneAny(example)}}
	return result
}

func runDecorators(config OpenAPIConfig, route Route, handler Handler, operation *OpenAPIOperation) error {
	for _, decorator := range config.RouteDecorators {
		if decorator == nil {
			continue
		}
		if err := runDecorator(decorator, &RouteDocContext{Route: route, Handler: handler, Operation: operation, Config: config}); err != nil {
			return err
		}
	}
	return nil
}

func runDecorator(decorator RouteDocDecorator, context *RouteDocContext) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("decorator panicked: %v", recovered)
		}
	}()
	return decorator(context)
}

var pathParamRe = regexp.MustCompile(`:[^/]+`)
var wildcardParamRe = regexp.MustCompile(`/\*([^/]+)$`)

func ginPathToOpenAPI(path string) string {
	if path == "" {
		path = "/"
	}
	if match := wildcardParamRe.FindStringSubmatchIndex(path); match != nil {
		name := path[match[2]:match[3]]
		path = path[:match[0]] + "/{" + name + "}"
	}
	return pathParamRe.ReplaceAllStringFunc(path, func(parameter string) string { return "{" + parameter[1:] + "}" })
}

func resolveOperationID(operation *OpenAPIOperation, route Route, method, path string, seen map[string]bool, collector *diagnosticCollector, handler Handler) {
	if operation.OperationID == "" {
		operation.OperationID = generateOperationID(method, route.Group, path)
	}
	if seen[operation.OperationID] {
		collector.route("operation.id.duplicate", DiagnosticError, route, handler, "operationId", fmt.Sprintf("duplicate operationId %q", operation.OperationID))
		return
	}
	seen[operation.OperationID] = true
}

func generateOperationID(method, group, path string) string {
	groupPart, pathPart := sanitizeIdentifier(group), sanitizePathIdentifier(path)
	if groupPart == "" {
		return strings.ToLower(method) + "_" + pathPart
	}
	return strings.ToLower(method) + "_" + groupPart + "_" + pathPart
}

func sanitizeIdentifier(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' {
			builder.WriteRune(character)
		} else if strings.ContainsRune(" -:/", character) {
			builder.WriteByte('_')
		}
	}
	result := builder.String()
	for strings.Contains(result, "__") {
		result = strings.ReplaceAll(result, "__", "_")
	}
	if len(result) > 64 {
		result = result[:64]
	}
	return strings.Trim(result, "_")
}

func sanitizePathIdentifier(path string) string {
	var builder strings.Builder
	for _, character := range path {
		if character == '/' {
			builder.WriteByte('_')
		} else if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '*' {
			builder.WriteRune(character)
		}
	}
	result := builder.String()
	for strings.Contains(result, "__") {
		result = strings.ReplaceAll(result, "__", "_")
	}
	return strings.Trim(result, "_")
}

func validateOperation(config OpenAPIConfig, operation *OpenAPIOperation, pathKey string, route Route, handler Handler, collector *diagnosticCollector) {
	for _, conflict := range operation.paramConflict {
		collector.route("parameter.conflict", DiagnosticError, route, handler, "parameters", fmt.Sprintf("conflicting parameter definitions for %s (in %s)", conflict.name, conflict.in))
	}
	for _, status := range operation.responseConflict {
		collector.route("response.duplicate", DiagnosticError, route, handler, "responses."+status, fmt.Sprintf("conflicting response definitions for status %s", status))
	}
	if operation.RequestBody != nil && len(operation.RequestBody.Content) == 0 {
		collector.route("request_body.schema.required", DiagnosticError, route, handler, "requestBody", "request body must define a usable schema and content type")
	}
	if operation.RequestBody != nil {
		for contentType := range operation.RequestBody.Content {
			if !validMediaType(contentType) {
				collector.route("request_body.content_type", DiagnosticError, route, handler, "requestBody.content", fmt.Sprintf("invalid request media type %q", contentType))
			}
		}
	}
	pathParameters := map[string]bool{}
	for _, name := range openAPIPathParamNames(pathKey) {
		pathParameters[name] = true
	}
	declaredPath := map[string]bool{}
	seen := map[paramKey]OpenAPIParameter{}
	for index, parameter := range operation.Parameters {
		location := fmt.Sprintf("parameters[%d]", index)
		key := openAPIParamKey(parameter)
		if existing, exists := seen[key]; exists && !paramsEqual(existing, parameter) {
			collector.route("parameter.duplicate", DiagnosticError, route, handler, location, fmt.Sprintf("incompatible duplicate parameter %q in %s", parameter.Name, parameter.In))
		} else {
			seen[key] = parameter
		}
		for _, message := range validateOpenAPIParameter(parameter) {
			collector.route("parameter.invalid", DiagnosticError, route, handler, location, message)
		}
		if parameter.In != string(DocParamInPath) {
			continue
		}
		if !parameter.Required {
			collector.route("path.parameter.required", DiagnosticError, route, handler, location, fmt.Sprintf("path parameter %q must be required", parameter.Name))
		}
		if !pathParameters[parameter.Name] {
			collector.route("path.parameter.extra", DiagnosticError, route, handler, location, fmt.Sprintf("path parameter %q is not present in path %q", parameter.Name, pathKey))
		}
		declaredPath[parameter.Name] = true
	}
	for name := range pathParameters {
		if !declaredPath[name] {
			collector.route("path.parameter.missing", DiagnosticError, route, handler, "parameters", fmt.Sprintf("path placeholder %q has no matching parameter", name))
		}
	}
	if len(operation.Responses) == 0 {
		collector.route("response.required", DiagnosticError, route, handler, "responses", "operation must declare at least one response")
	}
	hasSuccess := false
	for status, response := range operation.Responses {
		if !validStatusLabel(status) {
			collector.route("response.status", DiagnosticError, route, handler, "responses."+status, fmt.Sprintf("invalid response status %q", status))
			continue
		}
		if status[0] == '2' {
			hasSuccess = true
		}
		if (status == "204" || status == "205") && len(response.Content) > 0 {
			collector.route("response.body.forbidden", DiagnosticError, route, handler, "responses."+status, fmt.Sprintf("response status %s must not define a body schema", status))
		}
		for contentType := range response.Content {
			if !validMediaType(contentType) {
				collector.route("response.content_type", DiagnosticError, route, handler, "responses."+status, fmt.Sprintf("invalid response media type %q", contentType))
			}
		}
	}
	if len(operation.Responses) > 0 && !hasSuccess {
		collector.route("response.success.missing", DiagnosticWarning, route, handler, "responses", "operation declares no 2xx response")
	}
	for index, requirement := range operation.Security {
		if len(requirement) == 0 {
			collector.route("security.requirement.empty", DiagnosticError, route, handler, fmt.Sprintf("security[%d]", index), "security requirement must contain at least one scheme")
			continue
		}
		for name, scopes := range requirement {
			scheme := config.Components.SecuritySchemes[name]
			if scheme == nil {
				collector.route("security.scheme.missing", DiagnosticError, route, handler, fmt.Sprintf("security[%d]", index), fmt.Sprintf("security scheme %q is not defined in OpenAPIConfig.Components.SecuritySchemes", name))
				continue
			}
			if len(scopes) > 0 && scheme.Type != "oauth2" && scheme.Type != "openIdConnect" {
				collector.route("security.scope.invalid", DiagnosticError, route, handler, fmt.Sprintf("security[%d]", index), fmt.Sprintf("security scheme %q of type %q cannot use scopes", name, scheme.Type))
			} else if len(scopes) > 0 && scheme.Type == "oauth2" {
				available := oauthScopes(scheme.Flows)
				for _, scope := range scopes {
					if !available[scope] {
						collector.route("security.scope.unknown", DiagnosticError, route, handler, fmt.Sprintf("security[%d]", index), fmt.Sprintf("security scheme %q does not define scope %q", name, scope))
					}
				}
			}
			if scheme.Type == "apiKey" && scheme.In == "header" {
				for _, parameter := range operation.Parameters {
					if parameter.In == string(DocParamInHeader) && strings.EqualFold(parameter.Name, scheme.Name) {
						collector.route("security.header.duplicate", DiagnosticError, route, handler, "parameters", fmt.Sprintf("API key header %q is declared both as a security scheme and operation parameter", scheme.Name))
					}
				}
			}
		}
	}
}

func validMediaType(value string) bool {
	if value == "" || !strings.Contains(value, "/") {
		return false
	}
	_, _, err := mime.ParseMediaType(value)
	return err == nil
}

func validateOpenAPIParameter(parameter OpenAPIParameter) []string {
	var messages []string
	if parameter.Name == "" {
		messages = append(messages, "parameter name must not be empty")
	}
	switch DocParamIn(parameter.In) {
	case DocParamInHeader, DocParamInPath, DocParamInQuery:
	default:
		messages = append(messages, fmt.Sprintf("unknown parameter location %q for %q", parameter.In, parameter.Name))
	}
	if parameter.Schema == nil {
		return append(messages, fmt.Sprintf("parameter %q in %s must define a schema", parameter.Name, parameter.In))
	}
	if parameter.Schema.Ref == "" && parameter.Schema.Type == "" && len(parameter.Schema.AnyOf) == 0 {
		messages = append(messages, fmt.Sprintf("parameter %q in %s must define a schema type", parameter.Name, parameter.In))
	}
	if parameter.Schema.Type != "" && !validOpenAPIPrimitiveType(parameter.Schema.Type) {
		messages = append(messages, fmt.Sprintf("parameter %q in %s has invalid schema type %q", parameter.Name, parameter.In, parameter.Schema.Type))
	}
	return messages
}

func validOpenAPIPrimitiveType(typ string) bool {
	switch typ {
	case "null", "string", "number", "integer", "boolean", "array", "object":
		return true
	default:
		return false
	}
}

func validStatusLabel(status string) bool {
	if len(status) != 3 {
		return false
	}
	code := 0
	for _, character := range status {
		if character < '0' || character > '9' {
			return false
		}
		code = code*10 + int(character-'0')
	}
	return code >= 100 && code <= 599
}

var openAPIPathParamRe = regexp.MustCompile(`\{([^}/]+)\}`)

func openAPIPathParamNames(path string) []string {
	matches := openAPIPathParamRe.FindAllStringSubmatch(path, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match[1])
	}
	return names
}

func docDeclaresBehavior(documentation *DocConfig) bool {
	return documentation != nil && (len(documentation.Headers) > 0 || len(documentation.QueryParams) > 0 || len(documentation.PathParams) > 0 || documentation.RequestBody != nil || len(documentation.Responses) > 0 || len(documentation.Profiles) > 0)
}

func contractDeclaresBehavior(contract *Contract) bool {
	return contract != nil && (len(contract.Profiles) > 0 || len(contract.Parameters) > 0 || contract.RequestBody != nil || len(contract.Responses) > 0)
}

func sortedTags(names map[string]bool) []OpenAPITag {
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	tags := make([]OpenAPITag, len(sorted))
	for index, name := range sorted {
		tags[index] = OpenAPITag{Name: name}
	}
	return tags
}

func setMethod(item *OpenAPIPathItem, method string, operation *OpenAPIOperation) error {
	var target **OpenAPIOperation
	switch method {
	case http.MethodGet:
		target = &item.Get
	case http.MethodHead:
		target = &item.Head
	case http.MethodPost:
		target = &item.Post
	case http.MethodPut:
		target = &item.Put
	case http.MethodPatch:
		target = &item.Patch
	case http.MethodDelete:
		target = &item.Delete
	case http.MethodOptions:
		target = &item.Options
	case http.MethodTrace:
		target = &item.Trace
	default:
		return fmt.Errorf("unsupported method %q for PathItem", method)
	}
	if *target != nil {
		return fmt.Errorf("duplicate operation for method %s", method)
	}
	*target = operation
	return nil
}

func buildComponents(config OpenAPIConfig, generated map[string]*OpenAPISchema, collector *diagnosticCollector) *OpenAPIComponents {
	if len(config.Components.Schemas) == 0 && len(generated) == 0 && len(config.Components.SecuritySchemes) == 0 {
		return nil
	}
	components := &OpenAPIComponents{}
	if len(config.Components.Schemas) > 0 || len(generated) > 0 {
		components.Schemas = map[string]*OpenAPISchema{}
		for name, schema := range config.Components.Schemas {
			if !validComponentName(name) || schema == nil {
				collector.add(Diagnostic{Code: "schema.component.invalid", Severity: DiagnosticError, Location: "components.schemas." + name, Message: fmt.Sprintf("component schema %q must have a non-empty name and schema", name)})
				continue
			}
			components.Schemas[name] = cloneOpenAPISchema(schema)
		}
		for name, schema := range generated {
			if existing, exists := components.Schemas[name]; exists {
				if !schemasCanonicalEqual(existing, schema) {
					collector.add(Diagnostic{Code: "schema.component.collision", Severity: DiagnosticError, Location: "components.schemas." + name, Message: fmt.Sprintf("generated component %q conflicts with OpenAPIConfig.Components.Schemas", name)})
				}
				continue
			}
			components.Schemas[name] = cloneOpenAPISchema(schema)
		}
	}
	if len(config.Components.SecuritySchemes) > 0 {
		components.SecuritySchemes = map[string]*OpenAPISecurityScheme{}
		for name, scheme := range config.Components.SecuritySchemes {
			if !validComponentName(name) || scheme == nil {
				collector.add(Diagnostic{Code: "security.scheme.invalid", Severity: DiagnosticError, Location: "components.securitySchemes." + name, Message: fmt.Sprintf("security scheme %q must have a non-empty name and definition", name)})
				continue
			}
			for _, message := range validateSecurityScheme(*scheme) {
				collector.add(Diagnostic{Code: "security.scheme.invalid", Severity: DiagnosticError, Location: "components.securitySchemes." + name, Message: fmt.Sprintf("security scheme %q: %s", name, message)})
			}
			components.SecuritySchemes[name] = cloneOpenAPISecurityScheme(scheme)
		}
	}
	return components
}

var componentNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validComponentName(name string) bool { return componentNamePattern.MatchString(name) }

func validateSecurityScheme(scheme OpenAPISecurityScheme) []string {
	var messages []string
	switch scheme.Type {
	case "http":
		if scheme.Scheme == "" {
			messages = append(messages, "HTTP scheme must define scheme")
		}
	case "apiKey":
		if scheme.Name == "" {
			messages = append(messages, "apiKey scheme must define name")
		}
		if scheme.In != "header" && scheme.In != "query" && scheme.In != "cookie" {
			messages = append(messages, "apiKey scheme must use header, query, or cookie")
		}
	case "oauth2":
		if scheme.Flows == nil {
			messages = append(messages, "oauth2 scheme must define flows")
		} else if scheme.Flows.Implicit == nil && scheme.Flows.Password == nil && scheme.Flows.ClientCredentials == nil && scheme.Flows.AuthorizationCode == nil {
			messages = append(messages, "oauth2 scheme must define at least one flow")
		} else {
			messages = append(messages, validateOAuthFlow("implicit", scheme.Flows.Implicit, true, false)...)
			messages = append(messages, validateOAuthFlow("password", scheme.Flows.Password, false, true)...)
			messages = append(messages, validateOAuthFlow("clientCredentials", scheme.Flows.ClientCredentials, false, true)...)
			messages = append(messages, validateOAuthFlow("authorizationCode", scheme.Flows.AuthorizationCode, true, true)...)
		}
	case "openIdConnect":
		if scheme.OpenIDConnectURL == "" {
			messages = append(messages, "openIdConnect scheme must define openIdConnectUrl")
		}
	default:
		messages = append(messages, fmt.Sprintf("unknown type %q", scheme.Type))
	}
	return messages
}

func validateOAuthFlow(name string, flow *OpenAPIOAuthFlow, authorizationURL, tokenURL bool) []string {
	if flow == nil {
		return nil
	}
	var messages []string
	if authorizationURL && flow.AuthorizationURL == "" {
		messages = append(messages, name+" flow must define authorizationUrl")
	}
	if tokenURL && flow.TokenURL == "" {
		messages = append(messages, name+" flow must define tokenUrl")
	}
	if flow.Scopes == nil {
		messages = append(messages, name+" flow must define scopes")
	}
	return messages
}

func oauthScopes(flows *OpenAPIOAuthFlows) map[string]bool {
	result := map[string]bool{}
	if flows == nil {
		return result
	}
	for _, flow := range []*OpenAPIOAuthFlow{flows.Implicit, flows.Password, flows.ClientCredentials, flows.AuthorizationCode} {
		if flow != nil {
			for scope := range flow.Scopes {
				result[scope] = true
			}
		}
	}
	return result
}

func validateComponentRefs(document *OpenAPIDocument, collector *diagnosticCollector) {
	components := map[string]*OpenAPISchema{}
	if document.Components != nil && document.Components.Schemas != nil {
		components = document.Components.Schemas
	}
	for name, schema := range components {
		validateSchemaRefs(schema, components, "components.schemas."+name, collector)
	}
	for path, item := range document.Paths {
		for method, operation := range pathItemOperations(item) {
			if operation == nil {
				continue
			}
			for index := range operation.Parameters {
				validateSchemaRefs(operation.Parameters[index].Schema, components, fmt.Sprintf("paths.%s.%s.parameters[%d]", path, method, index), collector)
			}
			if operation.RequestBody != nil {
				for media, content := range operation.RequestBody.Content {
					validateSchemaRefs(content.Schema, components, fmt.Sprintf("paths.%s.%s.requestBody.%s", path, method, media), collector)
				}
			}
			for status, response := range operation.Responses {
				for media, content := range response.Content {
					validateSchemaRefs(content.Schema, components, fmt.Sprintf("paths.%s.%s.responses.%s.%s", path, method, status, media), collector)
				}
			}
		}
	}
}

func validateSchemaRefs(schema *OpenAPISchema, components map[string]*OpenAPISchema, location string, collector *diagnosticCollector) {
	validateSchemaRefsSeen(schema, components, location, collector, map[*OpenAPISchema]bool{}, map[*OpenAPISchema]bool{})
}

func validateSchemaRefsSeen(schema *OpenAPISchema, components map[string]*OpenAPISchema, location string, collector *diagnosticCollector, visited, stack map[*OpenAPISchema]bool) {
	if schema == nil {
		return
	}
	if stack[schema] {
		collector.add(Diagnostic{Code: "schema.cycle.invalid", Severity: DiagnosticError, Location: location, Message: "schema contains an in-memory cycle; use a local $ref for recursive schemas"})
		return
	}
	if visited[schema] {
		return
	}
	visited[schema], stack[schema] = true, true
	defer delete(stack, schema)
	if schema.Type != "" && !validOpenAPIPrimitiveType(schema.Type) {
		collector.add(Diagnostic{Code: "schema.type.invalid", Severity: DiagnosticError, Location: location, Message: fmt.Sprintf("schema type %q is invalid", schema.Type)})
	}
	const prefix = "#/components/schemas/"
	if schema.Ref != "" {
		if !strings.HasPrefix(schema.Ref, prefix) || components[strings.TrimPrefix(schema.Ref, prefix)] == nil {
			collector.add(Diagnostic{Code: "schema.ref.invalid", Severity: DiagnosticError, Location: location, Message: fmt.Sprintf("schema reference %q does not resolve to a local component", schema.Ref)})
		}
	}
	if len(schema.AnyOf) == 0 && schema.AnyOf != nil {
		collector.add(Diagnostic{Code: "schema.any_of.invalid", Severity: DiagnosticError, Location: location, Message: "schema anyOf must contain at least one schema"})
	}
	required := map[string]bool{}
	for _, name := range schema.Required {
		if required[name] {
			collector.add(Diagnostic{Code: "schema.required.duplicate", Severity: DiagnosticError, Location: location, Message: fmt.Sprintf("schema required contains duplicate property %q", name)})
		}
		required[name] = true
	}
	for index := range schema.AnyOf {
		validateSchemaRefsSeen(&schema.AnyOf[index], components, location+".anyOf", collector, visited, stack)
	}
	validateSchemaRefsSeen(schema.Items, components, location+".items", collector, visited, stack)
	validateSchemaRefsSeen(schema.AdditionalProperties, components, location+".additionalProperties", collector, visited, stack)
	for name, property := range schema.Properties {
		propertyCopy := property
		validateSchemaRefsSeen(&propertyCopy, components, location+".properties."+name, collector, visited, stack)
	}
}

func pathItemOperations(item OpenAPIPathItem) map[string]*OpenAPIOperation {
	return map[string]*OpenAPIOperation{"get": item.Get, "post": item.Post, "put": item.Put, "patch": item.Patch, "delete": item.Delete, "head": item.Head, "options": item.Options, "trace": item.Trace}
}
