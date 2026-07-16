package routekit

import "reflect"

type SchemaRegistration struct {
	typ        reflect.Type
	name       string
	schema     *OpenAPISchema
	directions map[SchemaDirection]bool
}

type SchemaRegistrationOption func(*schemaRegistrationOptions)

type schemaRegistrationOptions struct {
	directions map[SchemaDirection]bool
}

// ForSchemaRequest limits a registration to request schemas.
func ForSchemaRequest() SchemaRegistrationOption {
	return ForSchemaDirections(SchemaRequest)
}

// ForSchemaResponse limits a registration to response schemas.
func ForSchemaResponse() SchemaRegistrationOption {
	return ForSchemaDirections(SchemaResponse)
}

func ForSchemaDirections(directions ...SchemaDirection) SchemaRegistrationOption {
	return func(options *schemaRegistrationOptions) {
		if options.directions == nil {
			options.directions = map[SchemaDirection]bool{}
		}
		for _, direction := range directions {
			options.directions[direction] = true
		}
	}
}

func RegisterSchemaAs[T any](name string, options ...SchemaRegistrationOption) SchemaRegistration {
	return newSchemaRegistration[T](name, nil, options...)
}

func OverrideSchemaOf[T any](schema OpenAPISchema, options ...SchemaRegistrationOption) SchemaRegistration {
	return newSchemaRegistration[T]("", cloneOpenAPISchema(&schema), options...)
}

func newSchemaRegistration[T any](name string, schema *OpenAPISchema, options ...SchemaRegistrationOption) SchemaRegistration {
	settings := schemaRegistrationOptions{}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}
	return SchemaRegistration{
		typ:        reflect.TypeOf((*T)(nil)).Elem(),
		name:       name,
		schema:     cloneOpenAPISchema(schema),
		directions: settings.directions,
	}
}

func (registration SchemaRegistration) applies(direction SchemaDirection) bool {
	return len(registration.directions) == 0 || registration.directions[direction]
}
