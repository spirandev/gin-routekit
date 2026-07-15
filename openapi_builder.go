package routekit

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

var (
	errRegisterRoutesRequired = errors.New("RegisterRoutes must be called before RegisterOpenAPI")
)

// BuildOpenAPI builds an OpenAPI 3.0.3 document from the given routes and config.
func BuildOpenAPI(routes []Route, config OpenAPIConfig) (*OpenAPIDocument, error) {
	if config.Title == "" || config.Version == "" {
		return nil, errors.New("OpenAPIConfig.Title and OpenAPIConfig.Version are required")
	}
	if err := validateDocumentationMode(config.DocumentationMode); err != nil {
		return nil, err
	}

	reflector := newSchemaReflector()

	doc := &OpenAPIDocument{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       config.Title,
			Description: config.Description,
			Version:     config.Version,
		},
		Paths: OpenAPIPaths{},
	}

	if len(config.Servers) > 0 {
		doc.Servers = append([]OpenAPIServer(nil), config.Servers...)
	}

	tagOrder := map[string]int{}
	nextTagOrder := 0
	operationIDs := map[string]bool{}

	for _, route := range routes {
		for _, h := range route.Handlers {
			resolved, err := resolveRouteDocumentation(config, route, h)
			if err != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, err)
			}
			if !resolved.Enabled {
				continue
			}
			if err := validateMethod(h.Method); err != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, err)
			}

			operation := buildOperation(route, h, resolved, reflector)

			if err := runDecorators(config, route, h, operation); err != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, err)
			}

			addAutoPathParams(operation, route, h)

			pathKey := ginPathToOpenAPI(route.Path + h.RelativePath)
			if opErr := validateOperation(config, operation, pathKey); opErr != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, opErr)
			}
			if err := resolveOperationID(operation, route, h.Method, h.Path, operationIDs); err != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, err)
			}

			ensureDefaultResponse(operation)

			registerTags(operation, tagOrder, &nextTagOrder)

			item := doc.Paths[pathKey]
			if err := setMethod(&item, h.Method, operation); err != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, err)
			}
			doc.Paths[pathKey] = item
		}
	}

	doc.Tags = buildTagList(tagOrder)

	reflector.rewriteRenamedRefs()
	components := buildComponents(config, &reflector.named)
	if components != nil {
		doc.Components = components
	}

	return doc, nil
}

func validateDocumentationMode(mode DocumentationMode) error {
	switch mode {
	case DocumentationModeUnspecified, DocumentAll, DocumentOptIn:
		return nil
	default:
		return fmt.Errorf("unknown DocumentationMode %q", mode)
	}
}

func validateMethod(method string) error {
	_, ok := ValidMethods[method]
	if !ok {
		return fmt.Errorf("unsupported HTTP method %q", method)
	}
	if method == http.MethodConnect {
		return errors.New("HTTP CONNECT is not representable in OpenAPI 3.0.3 PathItem")
	}
	return nil
}

func buildOperation(_ Route, _ Handler, resolved *resolvedDocumentation, reflector *schemaReflector) *OpenAPIOperation {
	op := &OpenAPIOperation{
		Tags:        append([]string(nil), resolved.Tags...),
		Summary:     resolved.Summary,
		Description: resolved.Description,
		OperationID: resolved.OperationID,
		Responses:   OpenAPIResponses{},
	}
	for _, p := range resolved.Parameters {
		op.AddParameter(docParamToOpenAPIParameter(p))
	}
	if resolved.RequestBody != nil {
		op.RequestBody = buildRequestBody(resolved.RequestBody, resolved.RequestContentType, reflector)
	}
	for _, resp := range resolved.Responses {
		op.Responses[statusLabel(resp.Status)] = buildResponse(resp, resolved.ResponseContentType, reflector)
	}
	for _, requirement := range resolved.Security {
		op.AddSecurityRequirementFromMap(requirement)
	}

	return op
}

func docParamToOpenAPIParameter(p DocParam) OpenAPIParameter {
	if p.Type == "" {
		p.Type = "string"
	}
	return OpenAPIParameter{
		Name:        p.Name,
		In:          string(p.In),
		Description: p.Description,
		Required:    p.Required,
		Schema:      &OpenAPISchema{Type: p.Type},
		Example:     p.Example,
	}
}

func buildRequestBody(body *DocBody, defaultContentType string, reflector *schemaReflector) *OpenAPIRequestBody {
	contentType := body.ContentType
	if contentType == "" {
		contentType = defaultContentType
	}
	if body.Schema != nil && contentType == "" {
		contentType = "application/json"
	}
	content := map[string]OpenAPIMediaType{}
	if schema, descriptorExample := reflector.schemaFromInput(body.Schema); schema != nil {
		example := body.Example
		if example == nil {
			example = descriptorExample
		}
		content[contentType] = OpenAPIMediaType{
			Schema:  schema,
			Example: example,
		}
	}
	return &OpenAPIRequestBody{
		Description: body.Description,
		Required:    body.Required,
		Content:     content,
	}
}

func buildResponse(resp DocResponse, defaultContentType string, reflector *schemaReflector) OpenAPIResponse {
	schema, descriptorExample := reflector.schemaFromInput(resp.Schema)
	hasSchema := schema != nil
	contentType := resp.ContentType
	if contentType == "" {
		contentType = defaultContentType
	}
	if hasSchema && contentType == "" {
		contentType = "application/json"
	}
	r := OpenAPIResponse{Description: resp.Description}
	if hasSchema {
		if r.Content == nil {
			r.Content = map[string]OpenAPIMediaType{}
		}
		example := resp.Example
		if example == nil {
			example = descriptorExample
		}
		r.Content[contentType] = OpenAPIMediaType{
			Schema:  schema,
			Example: example,
		}
	}
	return r
}

func runDecorators(config OpenAPIConfig, route Route, h Handler, op *OpenAPIOperation) error {
	for _, decorator := range config.RouteDecorators {
		ctx := &RouteDocContext{
			Route:     route,
			Handler:   h,
			Operation: op,
			Config:    config,
		}
		if err := decorator(ctx); err != nil {
			return err
		}
	}
	return nil
}

var pathParamRe = regexp.MustCompile(`:[^/]+`)
var wildcardParamRe = regexp.MustCompile(`/\*([^/]+)$`)

func ginPathToOpenAPI(p string) string {
	if p == "" {
		p = "/"
	}
	// "/files/*path" -> "/files/{path}"
	if m := wildcardParamRe.FindStringSubmatchIndex(p); m != nil {
		start := m[2]
		end := m[3]
		name := p[start:end]
		p = p[:m[0]] + "/{" + name + "}"
	}
	// "/:id" -> "/{id}"
	p = pathParamRe.ReplaceAllStringFunc(p, func(s string) string {
		return "{" + s[1:] + "}"
	})
	return p
}

// pathParamNames returns the parameter names declared in a Gin path.
func pathParamNames(p string) []string {
	var names []string
	if m := wildcardParamRe.FindStringSubmatch(p); m != nil {
		names = append(names, m[1])
	}
	for _, m := range pathParamRe.FindAllStringSubmatch(p, -1) {
		names = append(names, m[0][1:])
	}
	return names
}

func addAutoPathParams(op *OpenAPIOperation, route Route, h Handler) {
	fullPath := route.Path + h.RelativePath
	declared := map[string]bool{}
	for _, p := range op.Parameters {
		if p.In == string(DocParamInPath) {
			declared[p.Name] = true
		}
	}
	for _, name := range pathParamNames(fullPath) {
		if declared[name] {
			continue
		}
		op.AddParameter(OpenAPIParameter{
			Name:     name,
			In:       string(DocParamInPath),
			Required: true,
			Schema:   &OpenAPISchema{Type: "string"},
		})
		declared[name] = true
	}
}

func resolveOperationID(op *OpenAPIOperation, route Route, method, path string, seen map[string]bool) error {
	if op.OperationID == "" {
		op.OperationID = generateOperationID(method, route.Group, path)
	}
	if seen[op.OperationID] {
		return fmt.Errorf("duplicate operationId %q", op.OperationID)
	}
	seen[op.OperationID] = true
	return nil
}

func generateOperationID(method, group, path string) string {
	lowerMethod := strings.ToLower(method)

	groupPart := sanitizeIdentifier(group)
	pathPart := sanitizePathIdentifier(path)

	if groupPart == "" {
		return lowerMethod + "_" + pathPart
	}
	return lowerMethod + "_" + groupPart + "_" + pathPart
}

func sanitizeIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == ':' || r == '/' {
			b.WriteByte('_')
		}
	}
	out := b.String()
	out = strings.ReplaceAll(out, "__", "_")
	if len(out) > 64 {
		out = out[:64]
	}
	return strings.Trim(out, "_")
}

func sanitizePathIdentifier(path string) string {
	// Convert path segments into an identifier using placeholders for params.
	var b strings.Builder
	for _, r := range path {
		if r == '/' {
			b.WriteByte('_')
		} else if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '*' {
			b.WriteRune(r)
		} else if r == ':' {
			// ":" starts a path param in Gin; skip the colon, keep the name.
			continue
		}
	}
	out := b.String()
	out = strings.ReplaceAll(out, "__", "_")
	out = strings.Trim(out, "_")
	return out
}

func validateOperation(config OpenAPIConfig, op *OpenAPIOperation, pathKey string) error {
	if len(op.paramConflict) > 0 {
		keys := make([]string, len(op.paramConflict))
		for i, k := range op.paramConflict {
			keys[i] = fmt.Sprintf("%s (in %s)", k.name, k.in)
		}
		return fmt.Errorf("conflicting parameter definitions for: %s", strings.Join(keys, ", "))
	}
	pathParams := map[string]bool{}
	for _, name := range openAPIPathParamNames(pathKey) {
		pathParams[name] = true
	}
	declaredPathParams := map[string]bool{}
	for _, param := range op.Parameters {
		if err := validateOpenAPIParameter(param); err != nil {
			return err
		}
		if param.In != string(DocParamInPath) {
			continue
		}
		if !param.Required {
			return fmt.Errorf("path parameter %q must be required", param.Name)
		}
		if !pathParams[param.Name] {
			return fmt.Errorf("path parameter %q is not present in path %q", param.Name, pathKey)
		}
		declaredPathParams[param.Name] = true
	}
	for name := range pathParams {
		if !declaredPathParams[name] {
			return fmt.Errorf("path placeholder %q has no matching parameter", name)
		}
	}
	for status := range op.Responses {
		if !validStatusLabel(status) {
			return fmt.Errorf("invalid response status %q", status)
		}
	}
	for _, requirement := range op.Security {
		if len(requirement) == 0 {
			return errors.New("security requirement must contain at least one scheme")
		}
		for name := range requirement {
			if len(config.Components.SecuritySchemes) > 0 && config.Components.SecuritySchemes[name] == nil {
				return fmt.Errorf("security scheme %q is not defined in OpenAPIConfig.Components.SecuritySchemes", name)
			}
		}
	}
	return nil
}

func validateOpenAPIParameter(param OpenAPIParameter) error {
	switch DocParamIn(param.In) {
	case DocParamInHeader, DocParamInPath, DocParamInQuery:
	default:
		return fmt.Errorf("unknown parameter location %q for %q", param.In, param.Name)
	}
	if param.Schema == nil {
		return fmt.Errorf("parameter %q in %s must define a schema", param.Name, param.In)
	}
	if param.Schema.Ref == "" && param.Schema.Type == "" {
		return fmt.Errorf("parameter %q in %s must define a schema type", param.Name, param.In)
	}
	if param.Schema.Type != "" && !validOpenAPIPrimitiveType(param.Schema.Type) {
		return fmt.Errorf("parameter %q in %s has invalid schema type %q", param.Name, param.In, param.Schema.Type)
	}
	return nil
}

func validOpenAPIPrimitiveType(typ string) bool {
	switch typ {
	case "string", "number", "integer", "boolean", "array", "object":
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
	for _, r := range status {
		if r < '0' || r > '9' {
			return false
		}
		code = code*10 + int(r-'0')
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

func ensureDefaultResponse(op *OpenAPIOperation) {
	if len(op.Responses) > 0 {
		return
	}
	op.Responses["200"] = OpenAPIResponse{Description: "OK"}
}

func registerTags(op *OpenAPIOperation, order map[string]int, next *int) {
	for _, t := range op.Tags {
		if _, ok := order[t]; ok {
			continue
		}
		order[t] = *next
		(*next)++
	}
}

func buildTagList(order map[string]int) []OpenAPITag {
	if len(order) == 0 {
		return nil
	}
	names := make([]string, 0, len(order))
	for name := range order {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return order[names[i]] < order[names[j]]
	})
	tags := make([]OpenAPITag, len(names))
	for i, name := range names {
		tags[i] = OpenAPITag{Name: name}
	}
	return tags
}

func setMethod(item *OpenAPIPathItem, method string, op *OpenAPIOperation) error {
	switch method {
	case http.MethodGet:
		if item.Get != nil {
			return fmt.Errorf("duplicate operation for method %s", method)
		}
		item.Get = op
	case http.MethodHead:
		if item.Head != nil {
			return fmt.Errorf("duplicate operation for method %s", method)
		}
		item.Head = op
	case http.MethodPost:
		if item.Post != nil {
			return fmt.Errorf("duplicate operation for method %s", method)
		}
		item.Post = op
	case http.MethodPut:
		if item.Put != nil {
			return fmt.Errorf("duplicate operation for method %s", method)
		}
		item.Put = op
	case http.MethodPatch:
		if item.Patch != nil {
			return fmt.Errorf("duplicate operation for method %s", method)
		}
		item.Patch = op
	case http.MethodDelete:
		if item.Delete != nil {
			return fmt.Errorf("duplicate operation for method %s", method)
		}
		item.Delete = op
	case http.MethodOptions:
		if item.Options != nil {
			return fmt.Errorf("duplicate operation for method %s", method)
		}
		item.Options = op
	case http.MethodTrace:
		if item.Trace != nil {
			return fmt.Errorf("duplicate operation for method %s", method)
		}
		item.Trace = op
	default:
		return fmt.Errorf("unsupported method %q for PathItem", method)
	}
	return nil
}

func buildComponents(config OpenAPIConfig, named *map[string]*OpenAPISchema) *OpenAPIComponents {
	schemaNames := config.Components.Schemas
	schemes := config.Components.SecuritySchemes

	hasSchemas := len(schemaNames) > 0 || len(*named) > 0
	hasSchemes := len(schemes) > 0
	if !hasSchemas && !hasSchemes {
		return nil
	}

	comp := &OpenAPIComponents{}
	if hasSchemas {
		comp.Schemas = map[string]*OpenAPISchema{}
		for name, s := range schemaNames {
			comp.Schemas[name] = cloneOpenAPISchema(s)
		}
		for name, s := range *named {
			if existing, ok := comp.Schemas[name]; ok && existing != nil {
				continue
			}
			comp.Schemas[name] = cloneOpenAPISchema(s)
		}
	}
	if hasSchemes {
		comp.SecuritySchemes = map[string]*OpenAPISecurityScheme{}
		for name, s := range schemes {
			if s == nil {
				continue
			}
			cloned := *s
			comp.SecuritySchemes[name] = &cloned
		}
	}
	return comp
}
