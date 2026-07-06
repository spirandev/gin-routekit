package routekit

import (
	"reflect"
	"strings"
	"time"
)

const dateTimeFormat = "date-time"

var timeTimeType = reflect.TypeOf(time.Time{})

type schemaReflector struct {
	named       map[string]*OpenAPISchema
	visited     map[string]bool
	inlineNamed bool
}

func newSchemaReflector() *schemaReflector {
	return &schemaReflector{
		named:   map[string]*OpenAPISchema{},
		visited: map[string]bool{},
	}
}

func newInlineSchemaReflector() *schemaReflector {
	return &schemaReflector{
		named:       map[string]*OpenAPISchema{},
		visited:     map[string]bool{},
		inlineNamed: true,
	}
}

func schemaTypeName(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() == "" {
		return ""
	}
	return t.PkgPath() + "." + t.Name()
}

func sanitizedSchemaName(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() == "" {
		return ""
	}
	name := t.Name()
	// Sanitize to a valid OpenAPI component key.
	name = strings.NewReplacer(".", "_", "/", "_").Replace(name)
	return name
}

func (r *schemaReflector) schemaFromValue(v any) *OpenAPISchema {
	if v == nil {
		return nil
	}
	return r.schemaFromType(reflect.TypeOf(v))
}

func (r *schemaReflector) schemaFromType(t reflect.Type) *OpenAPISchema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeTimeType {
		return &OpenAPISchema{Type: "string", Format: dateTimeFormat}
	}
	typeName := t.Name()
	if typeName == "UUID" && t.PkgPath() != "" {
		// Detect uuid-like named types without a hard dependency.
		return &OpenAPISchema{Type: "string", Format: "uuid"}
	}

	switch t.Kind() {
	case reflect.String:
		return &OpenAPISchema{Type: "string"}
	case reflect.Bool:
		return &OpenAPISchema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &OpenAPISchema{Type: "integer", Format: "int32"}
	case reflect.Int64, reflect.Uint64:
		return &OpenAPISchema{Type: "integer", Format: "int64"}
	case reflect.Float32:
		return &OpenAPISchema{Type: "number", Format: "float"}
	case reflect.Float64:
		return &OpenAPISchema{Type: "number", Format: "double"}
	case reflect.Slice, reflect.Array:
		return &OpenAPISchema{
			Type:  "array",
			Items: r.schemaFromType(t.Elem()),
		}
	case reflect.Map:
		key := t.Key()
		if key.Kind() == reflect.String {
			return &OpenAPISchema{
				Type:                 "object",
				AdditionalProperties: r.schemaFromType(t.Elem()),
			}
		}
		return &OpenAPISchema{Type: "object"}
	case reflect.Struct:
		if r.inlineNamed {
			fq := schemaTypeName(t)
			if fq != "" {
				if r.visited[fq] {
					return &OpenAPISchema{Type: "object"}
				}
				r.visited[fq] = true
				defer delete(r.visited, fq)
			}
			return r.structSchema(t)
		}
		if name := sanitizedSchemaName(t); name != "" {
			fq := schemaTypeName(t)
			if r.named[name] != nil {
				return &OpenAPISchema{Ref: "#/components/schemas/" + name}
			}
			r.named[name] = &OpenAPISchema{}
			if r.visited[fq] {
				// Recursive reference: return ref without populating again.
				delete(r.named, name)
				return &OpenAPISchema{Ref: "#/components/schemas/" + name}
			}
			r.visited[fq] = true
			schema := r.structSchema(t)
			r.named[name] = schema
			return &OpenAPISchema{Ref: "#/components/schemas/" + name}
		}
		return r.structSchema(t)
	case reflect.Interface:
		return &OpenAPISchema{}
	default:
		return &OpenAPISchema{Type: "string"}
	}
}

func (r *schemaReflector) structSchema(t reflect.Type) *OpenAPISchema {
	schema := &OpenAPISchema{Type: "object", Properties: map[string]OpenAPISchema{}}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		jsonTag, jsonOpts := parseJSONTag(field.Tag.Get("json"))
		if jsonTag == "-" {
			continue
		}
		if jsonTag == "" {
			jsonTag = field.Name
		}
		hasOmitempty := jsonOpts["omitempty"]
		bindingRequired := parseBindingRequired(field.Tag.Get("binding"))

		fieldSchema := r.schemaFromType(field.Type)
		if bindingRequired {
			schema.Required = append(schema.Required, jsonTag)
		} else if !hasOmitempty {
			// First-version heuristics: only mark required when explicitly
			// requested via binding tag. Do not infer from omitempty absence.
		}
		// Pointers expose as nullable when not required and not a $ref
		// (OpenAPI 3.0.3 ignores siblings of $ref).
		if field.Type.Kind() == reflect.Pointer && !bindingRequired && fieldSchema != nil && fieldSchema.Ref == "" {
			fieldSchema.Nullable = true
		}
		if fieldSchema != nil {
			schema.Properties[jsonTag] = *fieldSchema
		}
	}
	return schema
}

func parseJSONTag(raw string) (string, map[string]bool) {
	opts := map[string]bool{}
	parts := strings.Split(raw, ",")
	name := parts[0]
	for _, p := range parts[1:] {
		opts[strings.TrimSpace(p)] = true
	}
	return name, opts
}

func parseBindingRequired(raw string) bool {
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "required" {
			return true
		}
	}
	return false
}
