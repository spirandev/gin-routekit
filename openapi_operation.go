package routekit

import "reflect"

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
	for _, existing := range op.Parameters {
		if existing.Name == param.Name && existing.In == param.In {
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
	if op.Security == nil {
		op.Security = []OpenAPISecurityRequirement{}
	}
	for _, req := range op.Security {
		if _, ok := req[name]; ok {
			return
		}
	}
	op.Security = append(op.Security, OpenAPISecurityRequirement{name: {}})
}

func (op *OpenAPIOperation) AddResponse(status int, description string, schema any) {
	content := map[string]OpenAPIMediaType{}
	if schema != nil {
		reflector := newInlineSchemaReflector()
		content["application/json"] = OpenAPIMediaType{
			Schema: reflector.schemaFromValue(schema),
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
	if a.Name != b.Name || a.In != b.In || a.Description != b.Description || a.Required != b.Required {
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
