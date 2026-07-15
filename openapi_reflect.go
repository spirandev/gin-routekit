package routekit

import (
	"fmt"
	"hash/fnv"
	"reflect"
	"strings"
	"time"
)

const dateTimeFormat = "date-time"

var timeTimeType = reflect.TypeOf(time.Time{})

type schemaReflector struct {
	named            map[string]*OpenAPISchema
	visited          map[string]bool
	identityToName   map[string]string
	publicIdentities map[string]string
	refRenames       map[string]string
	generatedRefs    map[string][]*OpenAPISchema
	inlineNamed      bool
}

func newSchemaReflector() *schemaReflector {
	return &schemaReflector{
		named:            map[string]*OpenAPISchema{},
		visited:          map[string]bool{},
		identityToName:   map[string]string{},
		publicIdentities: map[string]string{},
		refRenames:       map[string]string{},
		generatedRefs:    map[string][]*OpenAPISchema{},
	}
}

func newInlineSchemaReflector() *schemaReflector {
	return &schemaReflector{
		named:            map[string]*OpenAPISchema{},
		visited:          map[string]bool{},
		identityToName:   map[string]string{},
		publicIdentities: map[string]string{},
		refRenames:       map[string]string{},
		generatedRefs:    map[string][]*OpenAPISchema{},
		inlineNamed:      true,
	}
}

func schemaIdentity(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.PkgPath() != "" && t.Name() != "" {
		return t.PkgPath() + "." + t.String()
	}
	return t.String()
}

func publicSchemaName(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	name := t.Name()
	if name == "" {
		name = t.String()
	}
	return sanitizeComponentName(name)
}

func sanitizeComponentName(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, ch := range name {
		allowed := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == '-'
		if allowed {
			b.WriteRune(ch)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "Schema"
	}
	return out
}

func (r *schemaReflector) schemaFromValue(v any) *OpenAPISchema {
	schema, _ := r.schemaFromInput(v)
	return schema
}

func (r *schemaReflector) schemaFromInput(v any) (*OpenAPISchema, any) {
	switch schema := v.(type) {
	case nil:
		return nil, nil
	case SchemaDescriptor:
		if schema.Type == nil {
			return nil, cloneAny(schema.Example)
		}
		return r.schemaFromType(schema.Type), cloneAny(schema.Example)
	case *SchemaDescriptor:
		if schema == nil || schema.Type == nil {
			return nil, nil
		}
		return r.schemaFromType(schema.Type), cloneAny(schema.Example)
	case reflect.Type:
		if schema == nil {
			return nil, nil
		}
		return r.schemaFromType(schema), nil
	case *OpenAPISchema:
		return cloneOpenAPISchema(schema), nil
	case OpenAPISchema:
		return cloneOpenAPISchema(&schema), nil
	default:
		return r.schemaFromType(reflect.TypeOf(v)), nil
	}
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
			fq := schemaIdentity(t)
			if fq != "" {
				if r.visited[fq] {
					return &OpenAPISchema{Type: "object"}
				}
				r.visited[fq] = true
				defer delete(r.visited, fq)
			}
			return r.structSchema(t)
		}
		identity := schemaIdentity(t)
		name := r.componentName(t, identity)
		if existingName, ok := r.identityToName[identity]; ok {
			return r.componentRef(identity, existingName)
		}
		r.identityToName[identity] = name
		r.named[name] = &OpenAPISchema{}
		if r.visited[identity] {
			return r.componentRef(identity, name)
		}
		r.visited[identity] = true
		schema := r.structSchema(t)
		delete(r.visited, identity)
		finalName := r.identityToName[identity]
		if finalName != name {
			delete(r.named, name)
		}
		r.named[finalName] = schema
		return r.componentRef(identity, finalName)
	case reflect.Interface:
		return &OpenAPISchema{}
	default:
		return &OpenAPISchema{Type: "string"}
	}
}

func (r *schemaReflector) componentName(t reflect.Type, identity string) string {
	base := publicSchemaName(t)
	if existingIdentity, ok := r.publicIdentities[base]; !ok || existingIdentity == identity {
		r.publicIdentities[base] = identity
		return base
	}
	existingIdentity := r.publicIdentities[base]
	if r.identityToName[existingIdentity] == base {
		existingName := base + "_" + shortIdentityHash(existingIdentity)
		r.identityToName[existingIdentity] = existingName
		if schema, ok := r.named[base]; ok {
			delete(r.named, base)
			r.named[existingName] = schema
		}
		r.refRenames[base] = existingName
		for _, ref := range r.generatedRefs[existingIdentity] {
			ref.Ref = "#/components/schemas/" + existingName
		}
	}
	name := base + "_" + shortIdentityHash(identity)
	r.publicIdentities[name] = identity
	return name
}

func (r *schemaReflector) componentRef(identity, name string) *OpenAPISchema {
	ref := &OpenAPISchema{Ref: "#/components/schemas/" + name}
	r.generatedRefs[identity] = append(r.generatedRefs[identity], ref)
	return ref
}

func (r *schemaReflector) rewriteRenamedRefs() {
	if len(r.refRenames) == 0 {
		return
	}
	for _, schema := range r.named {
		r.rewriteSchemaRef(schema)
	}
}

func (r *schemaReflector) rewriteSchemaRef(schema *OpenAPISchema) {
	if schema == nil {
		return
	}
	const prefix = "#/components/schemas/"
	if strings.HasPrefix(schema.Ref, prefix) {
		if name, ok := r.refRenames[strings.TrimPrefix(schema.Ref, prefix)]; ok {
			schema.Ref = prefix + name
		}
	}
	r.rewriteSchemaRef(schema.Items)
	r.rewriteSchemaRef(schema.AdditionalProperties)
	for name, property := range schema.Properties {
		r.rewriteSchemaRef(&property)
		schema.Properties[name] = property
	}
}

func shortIdentityHash(identity string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(identity))
	return fmt.Sprintf("%08x", h.Sum32())[:8]
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
