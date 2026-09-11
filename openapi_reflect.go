package routekit

import (
	"encoding"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"reflect"
	"sort"
	"strings"
	"time"
)

const dateTimeFormat = "date-time"

var (
	timeTimeType        = reflect.TypeOf(time.Time{})
	rawMessageType      = reflect.TypeOf(json.RawMessage{})
	jsonNumberType      = reflect.TypeOf(json.Number(""))
	jsonMarshalerType   = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textMarshalerType   = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

type schemaKey struct {
	typ       reflect.Type
	direction SchemaDirection
}

type schemaComponent struct {
	key          schemaKey
	schema       *OpenAPISchema
	explicitName string
	temporary    string
}

type schemaReflector struct {
	config              OpenAPIConfig
	collector           *diagnosticCollector
	route               Route
	handler             Handler
	components          map[schemaKey]*schemaComponent
	building            map[schemaKey]bool
	refs                []*OpenAPISchema
	registrations       map[schemaKey]SchemaRegistration
	commonRegistrations map[reflect.Type]SchemaRegistration
	named               map[string]*OpenAPISchema
	finalNames          map[string]string
}

func newSchemaReflector(arguments ...any) *schemaReflector {
	var config OpenAPIConfig
	var collector *diagnosticCollector
	if len(arguments) > 0 {
		config, _ = arguments[0].(OpenAPIConfig)
	}
	if len(arguments) > 1 {
		collector, _ = arguments[1].(*diagnosticCollector)
	}
	r := &schemaReflector{
		config: config, collector: collector,
		components: map[schemaKey]*schemaComponent{}, building: map[schemaKey]bool{},
		registrations:       map[schemaKey]SchemaRegistration{},
		commonRegistrations: map[reflect.Type]SchemaRegistration{},
		named:               map[string]*OpenAPISchema{},
	}
	r.indexRegistrations()
	return r
}

func (r *schemaReflector) schemaFromValue(value any) *OpenAPISchema {
	schema, _ := r.schemaFromInput(value, SchemaResponse)
	return schema
}

func (r *schemaReflector) setContext(route Route, handler Handler) {
	r.route, r.handler = route, handler
}

func (r *schemaReflector) indexRegistrations() {
	nameOwners := map[string]SchemaRegistration{}
	for index, registration := range r.config.SchemaRegistrations {
		if registration.typ == nil {
			r.configError("schema.registration.type", fmt.Sprintf("SchemaRegistrations[%d] has no type", index))
			continue
		}
		if registration.name == "" && registration.schema == nil {
			r.configError("schema.registration.empty", fmt.Sprintf("schema registration for %s has neither name nor override", registration.typ))
			continue
		}
		if registration.name != "" {
			if sanitizeComponentName(registration.name) != registration.name {
				r.configError("schema.registration.name", fmt.Sprintf("schema registration name %q is invalid", registration.name))
			}
			if owner, exists := nameOwners[registration.name]; exists && owner.typ != registration.typ {
				compatible := owner.schema != nil && registration.schema != nil && schemasCanonicalEqual(owner.schema, registration.schema)
				if !compatible {
					r.configError("schema.registration.name_conflict", fmt.Sprintf("schema registration name %q is used by incompatible types %s and %s", registration.name, owner.typ, registration.typ))
				}
			} else {
				nameOwners[registration.name] = registration
			}
			if manual := r.config.Components.Schemas[registration.name]; manual != nil && registration.schema != nil && !schemasCanonicalEqual(manual, registration.schema) {
				r.configError("schema.registration.component_conflict", fmt.Sprintf("schema registration name %q conflicts with OpenAPIConfig.Components.Schemas", registration.name))
			}
		}
		if len(registration.directions) == 0 {
			if _, exists := r.commonRegistrations[registration.typ]; exists {
				r.configError("schema.registration.duplicate", fmt.Sprintf("duplicate common schema registration for %s", registration.typ))
				continue
			}
			r.commonRegistrations[registration.typ] = registration
			continue
		}
		for direction := range registration.directions {
			if direction != SchemaRequest && direction != SchemaResponse {
				r.configError("schema.registration.direction", fmt.Sprintf("schema registration for %s has invalid direction %q", registration.typ, direction))
				continue
			}
			key := schemaKey{typ: registration.typ, direction: direction}
			if _, exists := r.registrations[key]; exists {
				r.configError("schema.registration.duplicate", fmt.Sprintf("duplicate %s schema registration for %s", direction, registration.typ))
				continue
			}
			r.registrations[key] = registration
		}
	}
}

func (r *schemaReflector) registration(key schemaKey) (SchemaRegistration, bool) {
	if registration, ok := r.registrations[key]; ok {
		return registration, true
	}
	registration, ok := r.commonRegistrations[key.typ]
	return registration, ok
}

func (r *schemaReflector) schemaFromInput(value any, direction SchemaDirection) (*OpenAPISchema, any) {
	switch schema := value.(type) {
	case nil:
		return nil, nil
	case SchemaDescriptor:
		if schema.Type == nil {
			return nil, cloneAny(schema.Example)
		}
		return r.schemaFromType(schema.Type, direction), cloneAny(schema.Example)
	case *SchemaDescriptor:
		if schema == nil || schema.Type == nil {
			return nil, nil
		}
		return r.schemaFromType(schema.Type, direction), cloneAny(schema.Example)
	case reflect.Type:
		if schema == nil {
			return nil, nil
		}
		return r.schemaFromType(schema, direction), nil
	case *OpenAPISchema:
		return cloneOpenAPISchema(schema), nil
	case OpenAPISchema:
		return cloneOpenAPISchema(&schema), nil
	default:
		return r.schemaFromType(reflect.TypeOf(value), direction), nil
	}
}

func (r *schemaReflector) schemaFromType(typ reflect.Type, direction SchemaDirection) *OpenAPISchema {
	if typ == nil {
		return nil
	}
	return r.schemaForType(typ, direction, true)
}

func (r *schemaReflector) schemaForType(typ reflect.Type, direction SchemaDirection, allowNull bool) *OpenAPISchema {
	key := schemaKey{typ: typ, direction: direction}
	registration, registered := r.registration(key)
	if registered && registration.schema != nil {
		return r.componentForSchema(key, registration.schema, registration.name)
	}
	if schema, ok := r.schemaFromProvider(typ, direction); ok {
		name := ""
		if registered {
			name = registration.name
		}
		return r.componentForSchema(key, schema, name)
	}

	if typ.Kind() == reflect.Pointer {
		base := r.schemaForType(typ.Elem(), direction, allowNull)
		if base == nil {
			return nil
		}
		result := base
		if allowNull {
			result = nullableSchema(base)
		}
		if registered && registration.name != "" {
			return r.componentForSchema(key, result, registration.name)
		}
		return result
	}

	var schema *OpenAPISchema
	switch {
	case typ == timeTimeType:
		schema = &OpenAPISchema{Type: "string", Format: dateTimeFormat}
	case typ == rawMessageType:
		return &OpenAPISchema{}
	case typ == jsonNumberType:
		schema = &OpenAPISchema{Type: "number"}
	case isKnownUUID(typ):
		schema = &OpenAPISchema{Type: "string", Format: "uuid"}
	default:
		if hasJSONCodec(typ, direction) {
			r.schemaError("schema.codec.provider_required", fmt.Sprintf("type %s has a custom JSON codec and requires an explicit schema, provider, or override", typ))
			return nil
		}
		if hasTextCodec(typ, direction) {
			schema = &OpenAPISchema{Type: "string"}
			break
		}
		schema = r.schemaForKind(typ, direction)
	}
	if schema == nil {
		return nil
	}
	if registered && registration.name != "" {
		schema = r.componentForSchema(key, schema, registration.name)
	}
	if direction == SchemaRequest && allowNull && (schema.Type != "" || schema.Ref != "" || len(schema.AnyOf) > 0) {
		return nullableSchema(schema)
	}
	if direction == SchemaResponse && allowNull && (typ.Kind() == reflect.Map || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Interface) {
		return nullableSchema(schema)
	}
	return schema
}

func (r *schemaReflector) schemaForKind(typ reflect.Type, direction SchemaDirection) *OpenAPISchema {
	switch typ.Kind() {
	case reflect.String:
		return &OpenAPISchema{Type: "string"}
	case reflect.Bool:
		return &OpenAPISchema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &OpenAPISchema{Type: "integer", Format: "int32"}
	case reflect.Int64, reflect.Uint64, reflect.Uintptr:
		return &OpenAPISchema{Type: "integer", Format: "int64"}
	case reflect.Float32:
		return &OpenAPISchema{Type: "number", Format: "float"}
	case reflect.Float64:
		return &OpenAPISchema{Type: "number", Format: "double"}
	case reflect.Slice:
		if typ.Elem().Kind() == reflect.Uint8 {
			return &OpenAPISchema{Type: "string", Format: "byte"}
		}
		return r.containerSchema(typ, direction, func() *OpenAPISchema {
			return &OpenAPISchema{Type: "array", Items: r.schemaForType(typ.Elem(), direction, true)}
		})
	case reflect.Array:
		return r.containerSchema(typ, direction, func() *OpenAPISchema {
			return &OpenAPISchema{Type: "array", Items: r.schemaForType(typ.Elem(), direction, true)}
		})
	case reflect.Map:
		if !validMapKey(typ.Key(), direction) {
			r.schemaError("schema.map.key", fmt.Sprintf("map type %s has a key unsupported by encoding/json for %s", typ, direction))
			return nil
		}
		return r.containerSchema(typ, direction, func() *OpenAPISchema {
			return &OpenAPISchema{Type: "object", AdditionalProperties: r.schemaForType(typ.Elem(), direction, true)}
		})
	case reflect.Struct:
		return r.componentForStruct(typ, direction)
	case reflect.Interface:
		return &OpenAPISchema{}
	default:
		r.schemaError("schema.kind.unsupported", fmt.Sprintf("kind %s in type %s is not representable by encoding/json", typ.Kind(), typ))
		return nil
	}
}

func (r *schemaReflector) containerSchema(typ reflect.Type, direction SchemaDirection, build func() *OpenAPISchema) *OpenAPISchema {
	if typ.Name() == "" {
		return build()
	}
	key := schemaKey{typ: typ, direction: direction}
	if component, exists := r.components[key]; exists {
		return r.componentRef(component.temporary)
	}
	component := r.newComponent(key, nil, "")
	if registration, ok := r.registration(key); ok {
		component.explicitName = registration.name
	}
	r.building[key] = true
	component.schema = build()
	delete(r.building, key)
	return r.componentRef(component.temporary)
}

func (r *schemaReflector) componentForStruct(typ reflect.Type, direction SchemaDirection) *OpenAPISchema {
	key := schemaKey{typ: typ, direction: direction}
	if component, exists := r.components[key]; exists {
		return r.componentRef(component.temporary)
	}
	component := r.newComponent(key, nil, "")
	if registration, ok := r.registration(key); ok {
		component.explicitName = registration.name
	}
	if r.building[key] {
		return r.componentRef(component.temporary)
	}
	r.building[key] = true
	component.schema = r.structSchema(typ, direction)
	delete(r.building, key)
	return r.componentRef(component.temporary)
}

func (r *schemaReflector) componentForSchema(key schemaKey, schema *OpenAPISchema, explicitName string) *OpenAPISchema {
	if component, exists := r.components[key]; exists {
		if explicitName != "" {
			component.explicitName = explicitName
		}
		return r.componentRef(component.temporary)
	}
	component := r.newComponent(key, cloneOpenAPISchema(schema), explicitName)
	return r.componentRef(component.temporary)
}

func (r *schemaReflector) newComponent(key schemaKey, schema *OpenAPISchema, explicitName string) *schemaComponent {
	temporary := "__routekit_" + shortIdentityHash(schemaKeyIdentity(key))
	component := &schemaComponent{key: key, schema: schema, explicitName: explicitName, temporary: temporary}
	r.components[key] = component
	return component
}

func (r *schemaReflector) componentRef(name string) *OpenAPISchema {
	ref := &OpenAPISchema{Ref: "#/components/schemas/" + name}
	r.refs = append(r.refs, ref)
	return ref
}

func (r *schemaReflector) structSchema(typ reflect.Type, direction SchemaDirection) *OpenAPISchema {
	schema := &OpenAPISchema{Type: "object", Properties: map[string]OpenAPISchema{}}
	for _, field := range discoverJSONFields(typ) {
		if direction == SchemaRequest && field.unexportedPointer {
			r.schemaError("schema.embedded_pointer.unallocatable", fmt.Sprintf("type %s promotes field %q through an unexported nil pointer that encoding/json cannot allocate", typ, field.name))
			continue
		}
		allowNull := !field.bindingRequired
		if direction == SchemaResponse {
			allowNull = !field.omitEmpty && !field.omitZero
		}
		fieldSchema := r.schemaForType(field.typ, direction, allowNull)
		if field.stringEncoded && supportsStringOption(field.typ) {
			fieldSchema = &OpenAPISchema{Type: "string"}
			if direction == SchemaRequest && allowNull || direction == SchemaResponse && allowNull && field.typ.Kind() == reflect.Pointer {
				fieldSchema = nullableSchema(fieldSchema)
			}
		}
		if fieldSchema == nil {
			continue
		}
		if field.deprecated {
			fieldSchema.Deprecated = true
		}
		schema.Properties[field.name] = *fieldSchema
		responseRequired := !field.throughPointer && !field.omitZero && !(field.omitEmpty && omitemptyCanOmit(field.typ))
		if direction == SchemaRequest && field.bindingRequired || direction == SchemaResponse && responseRequired {
			schema.Required = append(schema.Required, field.name)
		}
	}
	if len(schema.Properties) == 0 {
		schema.Properties = nil
	}
	return schema
}

func nullableSchema(schema *OpenAPISchema) *OpenAPISchema {
	if schema == nil {
		return nil
	}
	for _, option := range schema.AnyOf {
		if option.Type == "null" {
			return cloneOpenAPISchema(schema)
		}
	}
	return &OpenAPISchema{AnyOf: []OpenAPISchema{*cloneOpenAPISchema(schema), {Type: "null"}}}
}

func (r *schemaReflector) schemaFromProvider(typ reflect.Type, direction SchemaDirection) (schema *OpenAPISchema, found bool) {
	value, directional, common := providerValue(typ)
	if !directional && !common {
		return nil, false
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			r.schemaError("schema.provider.panic", fmt.Sprintf("schema provider for %s panicked: %v", typ, recovered))
			schema, found = nil, true
		}
	}()
	if directional {
		provided := value.Interface().(DirectionalOpenAPISchemaProvider).OpenAPISchemaFor(direction)
		return cloneOpenAPISchema(&provided), true
	}
	provided := value.Interface().(OpenAPISchemaProvider).OpenAPISchema()
	return cloneOpenAPISchema(&provided), true
}

func providerValue(typ reflect.Type) (reflect.Value, bool, bool) {
	var value reflect.Value
	if typ.Kind() == reflect.Pointer {
		value = reflect.New(typ.Elem())
	} else {
		value = reflect.New(typ).Elem()
		if !value.CanInterface() {
			return reflect.Value{}, false, false
		}
	}
	if value.Type().Implements(reflect.TypeOf((*DirectionalOpenAPISchemaProvider)(nil)).Elem()) {
		return value, true, true
	}
	if value.Type().Implements(reflect.TypeOf((*OpenAPISchemaProvider)(nil)).Elem()) {
		return value, false, true
	}
	if typ.Kind() != reflect.Pointer {
		pointer := reflect.New(typ)
		if pointer.Type().Implements(reflect.TypeOf((*DirectionalOpenAPISchemaProvider)(nil)).Elem()) {
			return pointer, true, true
		}
		if pointer.Type().Implements(reflect.TypeOf((*OpenAPISchemaProvider)(nil)).Elem()) {
			return pointer, false, true
		}
	}
	return reflect.Value{}, false, false
}

func hasJSONCodec(typ reflect.Type, direction SchemaDirection) bool {
	iface := jsonMarshalerType
	if direction == SchemaRequest {
		iface = jsonUnmarshalerType
	}
	return implementsType(typ, iface)
}

func hasTextCodec(typ reflect.Type, direction SchemaDirection) bool {
	iface := textMarshalerType
	if direction == SchemaRequest {
		iface = textUnmarshalerType
	}
	return implementsType(typ, iface)
}

func implementsType(typ, iface reflect.Type) bool {
	return typ.Implements(iface) || typ.Kind() != reflect.Pointer && reflect.PointerTo(typ).Implements(iface)
}

func validMapKey(typ reflect.Type, direction SchemaDirection) bool {
	if typ.Kind() == reflect.String || typ.Kind() >= reflect.Int && typ.Kind() <= reflect.Int64 || typ.Kind() >= reflect.Uint && typ.Kind() <= reflect.Uintptr {
		return true
	}
	if direction == SchemaResponse {
		return typ.Implements(textMarshalerType)
	}
	return typ.Kind() != reflect.Pointer && reflect.PointerTo(typ).Implements(textUnmarshalerType)
}

func isKnownUUID(typ reflect.Type) bool {
	if typ.Name() != "UUID" {
		return false
	}
	switch typ.PkgPath() {
	case "github.com/google/uuid", "github.com/gofrs/uuid", "github.com/gofrs/uuid/v5", "github.com/satori/go.uuid":
		return true
	default:
		return false
	}
}

func schemaKeyIdentity(key schemaKey) string {
	return schemaTypeIdentity(key.typ) + "|" + string(key.direction)
}

func schemaTypeIdentity(typ reflect.Type) string {
	if typ.PkgPath() != "" && typ.Name() != "" {
		return typ.PkgPath() + "." + typ.String()
	}
	return typ.String()
}

func schemaIdentity(typ reflect.Type) string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return schemaTypeIdentity(typ)
}

func publicSchemaName(typ reflect.Type) string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	name := typ.Name()
	if name == "" {
		name = typ.String()
	}
	return sanitizeComponentName(name)
}

func sanitizeComponentName(name string) string {
	var builder strings.Builder
	underscore := false
	for _, character := range name {
		allowed := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-'
		if allowed {
			builder.WriteRune(character)
			underscore = false
			continue
		}
		if !underscore {
			builder.WriteByte('_')
			underscore = true
		}
	}
	if result := strings.Trim(builder.String(), "_"); result != "" {
		return result
	}
	return "Schema"
}

func shortIdentityHash(identity string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(identity))
	return fmt.Sprintf("%08x", hash.Sum32())
}

func (r *schemaReflector) finalize() map[string]*OpenAPISchema {
	keys := make([]schemaKey, 0, len(r.components))
	for key := range r.components {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return schemaKeyIdentity(keys[i]) < schemaKeyIdentity(keys[j]) })

	shared := map[schemaKey]schemaKey{}
	for _, key := range keys {
		if key.direction != SchemaRequest {
			continue
		}
		responseKey := schemaKey{typ: key.typ, direction: SchemaResponse}
		requestComponent, requestOK := r.components[key]
		responseComponent, responseOK := r.components[responseKey]
		if requestOK && responseOK && requestComponent.explicitName == responseComponent.explicitName && r.componentsEquivalent(requestComponent, responseComponent, &schemaEquivalenceState{components: map[schemaComponentPair]bool{}, schemas: map[schemaPointerPair]bool{}}) {
			shared[responseKey] = key
		}
	}

	desired := map[schemaKey]string{}
	for _, key := range keys {
		if canonical, ok := shared[key]; ok {
			key = canonical
		}
		if _, exists := desired[key]; exists {
			continue
		}
		component := r.components[key]
		name := component.explicitName
		if name == "" {
			name = publicSchemaName(key.typ)
			other := schemaKey{typ: key.typ, direction: SchemaRequest}
			if key.direction == SchemaRequest {
				other.direction = SchemaResponse
			}
			if _, exists := r.components[other]; exists {
				if _, merged := shared[other]; !merged && shared[key] != other {
					if key.direction == SchemaRequest {
						name += "Request"
					} else {
						name += "Response"
					}
				}
			}
		}
		desired[key] = sanitizeComponentName(name)
	}

	nameOwners := map[string][]schemaKey{}
	for key, name := range desired {
		nameOwners[name] = append(nameOwners[name], key)
	}
	for name, owners := range nameOwners {
		if len(owners) < 2 {
			continue
		}
		hasExplicitName := false
		for _, key := range owners {
			if r.components[key].explicitName != "" {
				hasExplicitName = true
				break
			}
		}
		if hasExplicitName {
			continue
		}
		for _, key := range owners {
			desired[key] = name + "_" + shortIdentityHash(schemaTypeIdentity(key.typ))
		}
	}

	tempToFinal := map[string]string{}
	for _, key := range keys {
		canonical := key
		if merged, ok := shared[key]; ok {
			canonical = merged
		}
		tempToFinal[r.components[key].temporary] = desired[canonical]
	}
	for _, ref := range r.refs {
		rewriteSchemaRefs(ref, tempToFinal)
	}
	r.finalNames = tempToFinal

	named := map[string]*OpenAPISchema{}
	for _, key := range keys {
		if _, merged := shared[key]; merged {
			continue
		}
		component := cloneOpenAPISchema(r.components[key].schema)
		rewriteSchemaRefs(component, tempToFinal)
		name := desired[key]
		if existing, exists := named[name]; exists && !reflect.DeepEqual(existing, component) {
			r.configError("schema.component.name_conflict", fmt.Sprintf("component name %q resolves to incompatible schemas", name))
			continue
		}
		named[name] = component
	}
	r.named = named
	return named
}

type schemaComponentPair struct{ left, right schemaKey }
type schemaPointerPair struct{ left, right *OpenAPISchema }
type schemaEquivalenceState struct {
	components map[schemaComponentPair]bool
	schemas    map[schemaPointerPair]bool
}

func (r *schemaReflector) componentsEquivalent(left, right *schemaComponent, seen *schemaEquivalenceState) bool {
	if left == nil || right == nil {
		return left == right
	}
	pair := schemaComponentPair{left: left.key, right: right.key}
	if seen.components[pair] {
		return true
	}
	seen.components[pair] = true
	return r.schemasEquivalent(left.schema, right.schema, seen)
}

func (r *schemaReflector) schemasEquivalent(left, right *OpenAPISchema, seen *schemaEquivalenceState) bool {
	if left == nil || right == nil {
		return left == right
	}
	pair := schemaPointerPair{left: left, right: right}
	if seen.schemas[pair] {
		return true
	}
	seen.schemas[pair] = true
	if left.Ref != "" || right.Ref != "" {
		if left.Ref == "" || right.Ref == "" {
			return false
		}
		leftComponent, leftOK := r.componentByRef(left.Ref)
		rightComponent, rightOK := r.componentByRef(right.Ref)
		if leftOK || rightOK {
			return leftOK && rightOK && leftComponent.key.typ == rightComponent.key.typ && r.componentsEquivalent(leftComponent, rightComponent, seen)
		}
		return left.Ref == right.Ref
	}
	if left.Type != right.Type || left.Format != right.Format || left.Description != right.Description ||
		left.Deprecated != right.Deprecated || !reflect.DeepEqual(left.Required, right.Required) ||
		len(left.AnyOf) != len(right.AnyOf) || len(left.Properties) != len(right.Properties) {
		return false
	}
	for index := range left.AnyOf {
		if !r.schemasEquivalent(&left.AnyOf[index], &right.AnyOf[index], seen) {
			return false
		}
	}
	if !r.schemasEquivalent(left.Items, right.Items, seen) || !r.schemasEquivalent(left.AdditionalProperties, right.AdditionalProperties, seen) {
		return false
	}
	for name, property := range left.Properties {
		other, exists := right.Properties[name]
		if !exists || !r.schemasEquivalent(&property, &other, seen) {
			return false
		}
	}
	return true
}

func (r *schemaReflector) componentByRef(ref string) (*schemaComponent, bool) {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return nil, false
	}
	name := strings.TrimPrefix(ref, prefix)
	for _, component := range r.components {
		if component.temporary == name {
			return component, true
		}
	}
	return nil, false
}

func schemasCanonicalEqual(left, right *OpenAPISchema) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func rewriteSchemaRefs(schema *OpenAPISchema, names map[string]string) {
	rewriteSchemaRefsSeen(schema, names, map[*OpenAPISchema]bool{})
}

func rewriteSchemaRefsSeen(schema *OpenAPISchema, names map[string]string, seen map[*OpenAPISchema]bool) {
	if schema == nil {
		return
	}
	if seen[schema] {
		return
	}
	seen[schema] = true
	const prefix = "#/components/schemas/"
	if strings.HasPrefix(schema.Ref, prefix) {
		if name, ok := names[strings.TrimPrefix(schema.Ref, prefix)]; ok {
			schema.Ref = prefix + name
		}
	}
	for index := range schema.AnyOf {
		rewriteSchemaRefsSeen(&schema.AnyOf[index], names, seen)
	}
	rewriteSchemaRefsSeen(schema.Items, names, seen)
	rewriteSchemaRefsSeen(schema.AdditionalProperties, names, seen)
	for name, property := range schema.Properties {
		rewriteSchemaRefsSeen(&property, names, seen)
		schema.Properties[name] = property
	}
}

func (r *schemaReflector) schemaError(code, message string) {
	if r.collector != nil {
		r.collector.route(code, DiagnosticError, r.route, r.handler, "schema", message)
	}
}

func (r *schemaReflector) configError(code, message string) {
	if r.collector != nil {
		r.collector.add(Diagnostic{Code: code, Severity: DiagnosticError, Location: "config.schemaRegistrations", Message: message})
	}
}
