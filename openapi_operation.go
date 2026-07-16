package routekit

import (
	"reflect"
	"strconv"
	"strings"
)

type paramKey struct {
	name string
	in   string
}

func (op *OpenAPIOperation) AddHeader(name, typ string, required bool, description string) {
	if typ == "" {
		typ = "string"
	}
	op.AddParameter(OpenAPIParameter{
		Name:        name,
		In:          string(DocParamInHeader),
		Description: description,
		Required:    required,
		Schema:      &OpenAPISchema{Type: typ},
	})
}

func (op *OpenAPIOperation) AddQuery(name, typ string, required bool, description string) {
	if typ == "" {
		typ = "string"
	}
	op.AddParameter(OpenAPIParameter{
		Name:        name,
		In:          string(DocParamInQuery),
		Description: description,
		Required:    required,
		Schema:      &OpenAPISchema{Type: typ},
	})
}

func (op *OpenAPIOperation) AddPathParam(name, typ string, description string) {
	if typ == "" {
		typ = "string"
	}
	op.AddParameter(OpenAPIParameter{
		Name:        name,
		In:          string(DocParamInPath),
		Description: description,
		Required:    true,
		Schema:      &OpenAPISchema{Type: typ},
	})
}

// AddParameter inserts a parameter, deduplicating by (name, in). On duplicate
// with a different definition, the conflicting parameter is kept and a marker
// is returned via the error channel of the builder; here we keep the first
// definition to avoid silent overwrites during decorator execution.
func (op *OpenAPIOperation) AddParameter(param OpenAPIParameter) {
	newKey := openAPIParamKey(param)
	for _, existing := range op.Parameters {
		if openAPIParamKey(existing) == newKey {
			if paramsEqual(existing, param) {
				return
			}
			op.paramConflict = append(op.paramConflict, paramKey{name: param.Name, in: param.In})
			return
		}
	}
	op.Parameters = append(op.Parameters, param)
}

func (op *OpenAPIOperation) AddSecurity(name string) {
	op.AddSecurityRequirement(name)
}

func (op *OpenAPIOperation) AddSecurityRequirement(names ...string) {
	requirement := OpenAPISecurityRequirement{}
	for _, name := range names {
		if name == "" {
			continue
		}
		requirement[name] = []string{}
	}
	op.AddSecurityRequirementFromMap(requirement)
}

func (op *OpenAPIOperation) AddSecurityRequirementFromMap(requirement OpenAPISecurityRequirement) {
	if len(requirement) == 0 {
		if op.Security == nil {
			op.Security = []OpenAPISecurityRequirement{}
		}
		op.Security = append(op.Security, OpenAPISecurityRequirement{})
		return
	}
	requirement = cloneSecurityRequirement(requirement)
	if op.Security == nil {
		op.Security = []OpenAPISecurityRequirement{}
	}
	for _, existing := range op.Security {
		if valuesEqual(existing, requirement) {
			return
		}
	}
	op.Security = append(op.Security, requirement)
}

func (op *OpenAPIOperation) AddResponse(status int, description string, schema any) {
	content := map[string]OpenAPIMediaType{}
	if schema != nil {
		reflector := op.schemaReflector
		if reflector == nil {
			reflector = newSchemaReflector()
		}
		schema, _ := reflector.schemaFromInput(schema, SchemaResponse)
		if op.schemaReflector == nil {
			named := reflector.finalize()
			rewriteSchemaRefs(schema, reflector.finalNames)
			schema = inlineComponentRefs(schema, named, map[string]bool{})
		}
		content["application/json"] = OpenAPIMediaType{
			Schema: schema,
		}
	}
	if op.Responses == nil {
		op.Responses = OpenAPIResponses{}
	}
	label := statusLabel(status)
	response := OpenAPIResponse{
		Description: description,
		Content:     content,
	}
	if _, exists := op.Responses[label]; exists {
		op.responseConflict = append(op.responseConflict, label)
		return
	}
	op.Responses[label] = response
}

func inlineComponentRefs(schema *OpenAPISchema, components map[string]*OpenAPISchema, seen map[string]bool) *OpenAPISchema {
	if schema == nil {
		return nil
	}
	const prefix = "#/components/schemas/"
	if strings.HasPrefix(schema.Ref, prefix) {
		name := strings.TrimPrefix(schema.Ref, prefix)
		if seen[name] {
			return &OpenAPISchema{Type: "object"}
		}
		if component := components[name]; component != nil {
			seen[name] = true
			inlined := inlineComponentRefs(component, components, seen)
			delete(seen, name)
			return inlined
		}
	}
	cloned := cloneOpenAPISchema(schema)
	for index := range cloned.AnyOf {
		item := inlineComponentRefs(&cloned.AnyOf[index], components, seen)
		if item != nil {
			cloned.AnyOf[index] = *item
		}
	}
	cloned.Items = inlineComponentRefs(cloned.Items, components, seen)
	cloned.AdditionalProperties = inlineComponentRefs(cloned.AdditionalProperties, components, seen)
	for name, property := range cloned.Properties {
		item := inlineComponentRefs(&property, components, seen)
		if item != nil {
			cloned.Properties[name] = *item
		}
	}
	return cloned
}

func paramsEqual(a, b OpenAPIParameter) bool {
	if openAPIParamKey(a) != openAPIParamKey(b) || a.Description != b.Description || a.Required != b.Required {
		return false
	}
	if (a.Schema == nil) != (b.Schema == nil) {
		return false
	}
	if a.Schema != nil && b.Schema != nil {
		if !reflect.DeepEqual(a.Schema, b.Schema) {
			return false
		}
	}
	if !valuesEqual(a.Example, b.Example) {
		return false
	}
	return true
}

func openAPIParamKey(param OpenAPIParameter) paramKey {
	name := param.Name
	if param.In == string(DocParamInHeader) {
		name = strings.ToLower(name)
	}
	return paramKey{name: name, in: param.In}
}

func valuesEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

// statusLabel converts an HTTP status code to its string representation.
func statusLabel(status int) string {
	// fast path for common statuses
	switch status {
	case 200:
		return "200"
	case 201:
		return "201"
	case 204:
		return "204"
	case 400:
		return "400"
	case 401:
		return "401"
	case 403:
		return "403"
	case 404:
		return "404"
	case 500:
		return "500"
	}
	return strconv.Itoa(status)
}
