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
			if !isDocumented(config.EnabledByDefault, h.Doc) {
				continue
			}
			if err := validateMethod(h.Method); err != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, err)
			}

			operation := buildOperation(route, h, config, reflector)
			if err := applyProfiles(config, h, operation); err != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, err)
			}

			if err := runDecorators(config, route, h, operation); err != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, err)
			}

			addAutoPathParams(operation, route, h)

			if opErr := validateOperation(operation); opErr != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, opErr)
			}
			if err := resolveOperationID(operation, route, h.Method, h.Path, operationIDs); err != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, err)
			}

			ensureDefaultResponse(operation)

			registerTags(operation, tagOrder, &nextTagOrder)

			pathKey := ginPathToOpenAPI(route.Path + h.RelativePath)
			item := doc.Paths[pathKey]
			if err := setMethod(&item, h.Method, operation); err != nil {
				return nil, fmt.Errorf("route %s %s: %w", h.Method, h.Path, err)
			}
			doc.Paths[pathKey] = item
		}
	}

	doc.Tags = buildTagList(tagOrder)

	components := buildComponents(config, &reflector.named)
	if components != nil {
		doc.Components = components
	}

	return doc, nil
}

func isDocumented(enabledByDefault bool, doc *DocConfig) bool {
	if doc == nil {
		return enabledByDefault
	}
	if doc.Enabled != nil {
		return *doc.Enabled
	}
	return enabledByDefault
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

func buildOperation(route Route, h Handler, _ OpenAPIConfig, reflector *schemaReflector) *OpenAPIOperation {
	tags := []string{route.Group}
	if h.Doc != nil && len(h.Doc.Tags) > 0 {
		tags = append([]string{}, h.Doc.Tags...)
	}
	summary := h.Definition
	description := ""
	if h.Doc != nil {
		if h.Doc.Summary != "" {
			summary = h.Doc.Summary
		}
		if h.Doc.Description != "" {
			description = h.Doc.Description
		}
	}

	op := &OpenAPIOperation{
		Tags:        tags,
		Summary:     summary,
		Description: description,
		OperationID: "",
		Responses:   OpenAPIResponses{},
	}
	if h.Doc != nil && h.Doc.OperationID != "" {
		op.OperationID = h.Doc.OperationID
	}

	if h.Doc != nil {
		for _, p := range h.Doc.Headers {
			op.AddParameter(docParamToOpenAPIParameter(p))
		}
		for _, p := range h.Doc.QueryParams {
			op.AddParameter(docParamToOpenAPIParameter(p))
		}
		for _, p := range h.Doc.PathParams {
			op.AddParameter(docParamToOpenAPIParameter(p))
		}

		if h.Doc.RequestBody != nil {
			op.RequestBody = buildRequestBody(h.Doc.RequestBody, reflector)
		}
		for _, resp := range h.Doc.Responses {
			op.Responses[statusLabel(resp.Status)] = buildResponse(resp, reflector)
		}
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

func buildRequestBody(body *DocBody, reflector *schemaReflector) *OpenAPIRequestBody {
	contentType := body.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	content := map[string]OpenAPIMediaType{}
	if body.Schema != nil {
		content[contentType] = OpenAPIMediaType{
			Schema:  reflector.schemaFromValue(body.Schema),
			Example: body.Example,
		}
	}
	return &OpenAPIRequestBody{
		Description: body.Description,
		Required:    body.Required,
		Content:     content,
	}
}

func buildResponse(resp DocResponse, reflector *schemaReflector) OpenAPIResponse {
	hasSchema := resp.Schema != nil
	contentType := resp.ContentType
	if hasSchema && contentType == "" {
		contentType = "application/json"
	}
	r := OpenAPIResponse{Description: resp.Description}
	if hasSchema {
		if r.Content == nil {
			r.Content = map[string]OpenAPIMediaType{}
		}
		r.Content[contentType] = OpenAPIMediaType{
			Schema:  reflector.schemaFromValue(resp.Schema),
			Example: resp.Example,
		}
	}
	return r
}

func applyProfiles(config OpenAPIConfig, h Handler, op *OpenAPIOperation) error {
	if h.Doc == nil {
		return nil
	}
	for _, name := range h.Doc.Profiles {
		profile, ok := config.Profiles[name]
		if !ok {
			return fmt.Errorf("profile %q not found in OpenAPIConfig.Profiles", name)
		}
		for _, p := range profile.Headers {
			op.AddParameter(docParamToOpenAPIParameter(p))
		}
		for _, p := range profile.QueryParams {
			op.AddParameter(docParamToOpenAPIParameter(p))
		}
		for _, p := range profile.PathParams {
			op.AddParameter(docParamToOpenAPIParameter(p))
		}
		for _, s := range profile.Security {
			op.AddSecurity(s)
		}
	}
	return nil
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

func validateOperation(op *OpenAPIOperation) error {
	if len(op.paramConflict) > 0 {
		keys := make([]string, len(op.paramConflict))
		for i, k := range op.paramConflict {
			keys[i] = fmt.Sprintf("%s (in %s)", k.name, k.in)
		}
		return fmt.Errorf("conflicting parameter definitions for: %s", strings.Join(keys, ", "))
	}
	return nil
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
		item.Get = op
	case http.MethodHead:
		item.Head = op
	case http.MethodPost:
		item.Post = op
	case http.MethodPut:
		item.Put = op
	case http.MethodPatch:
		item.Patch = op
	case http.MethodDelete:
		item.Delete = op
	case http.MethodOptions:
		item.Options = op
	case http.MethodTrace:
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
			comp.Schemas[name] = s
		}
		for name, s := range *named {
			if existing, ok := comp.Schemas[name]; ok && existing != nil {
				continue
			}
			comp.Schemas[name] = s
		}
	}
	if hasSchemes {
		comp.SecuritySchemes = map[string]*OpenAPISecurityScheme{}
		for name, s := range schemes {
			comp.SecuritySchemes[name] = s
		}
	}
	return comp
}
