package routekit

import (
	"reflect"
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
		reflector := newInlineSchemaReflector()
		schema, _ := reflector.schemaFromInput(schema)
		content["application/json"] = OpenAPIMediaType{
			Schema: schema,
		}
	}
	if op.Responses == nil {
		op.Responses = OpenAPIResponses{}
	}
	op.Responses[statusLabel(status)] = OpenAPIResponse{
		Description: description,
		Content:     content,
	}
}

func paramsEqual(a, b OpenAPIParameter) bool {
	if openAPIParamKey(a) != openAPIParamKey(b) || a.Description != b.Description || a.Required != b.Required {
		return false
	}
	if (a.Schema == nil) != (b.Schema == nil) {
		return false
	}
	if a.Schema != nil && b.Schema != nil {
		if a.Schema.Type != b.Schema.Type || a.Schema.Format != b.Schema.Format {
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
	return itoa(status)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	buf := [12]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
