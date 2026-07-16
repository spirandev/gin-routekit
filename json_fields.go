package routekit

import (
	"reflect"
	"sort"
	"strings"
	"unicode"
)

type jsonField struct {
	name              string
	typ               reflect.Type
	index             []int
	tagged            bool
	omitEmpty         bool
	omitZero          bool
	stringEncoded     bool
	bindingRequired   bool
	throughPointer    bool
	unexportedPointer bool
}

type jsonFieldLevel struct {
	typ               reflect.Type
	index             []int
	throughPointer    bool
	unexportedPointer bool
}

func discoverJSONFields(root reflect.Type) []jsonField {
	current := []jsonFieldLevel{{typ: root}}
	visited := map[reflect.Type]bool{}
	var candidates []jsonField
	for len(current) > 0 {
		next := []jsonFieldLevel{}
		processed := map[reflect.Type]bool{}
		for _, level := range current {
			typ := level.typ
			if visited[typ] {
				continue
			}
			processed[typ] = true
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				fieldType := field.Type
				throughPointer := level.throughPointer
				unexportedPointer := level.unexportedPointer
				nextThroughPointer := throughPointer
				nextUnexportedPointer := unexportedPointer
				if fieldType.Kind() == reflect.Pointer && fieldType.Name() == "" {
					if field.Anonymous {
						nextThroughPointer = true
						if !field.IsExported() {
							nextUnexportedPointer = true
						}
					}
					fieldType = fieldType.Elem()
				}
				if field.Anonymous {
					anonymousType := field.Type
					if anonymousType.Kind() == reflect.Pointer {
						anonymousType = anonymousType.Elem()
					}
					if !field.IsExported() && anonymousType.Kind() != reflect.Struct {
						continue
					}
				} else if !field.IsExported() {
					continue
				}
				name, options := parseJSONTag(field.Tag.Get("json"))
				if name == "-" {
					continue
				}
				if name != "" && !validJSONTagName(name) {
					name = ""
				}
				index := append(append([]int(nil), level.index...), i)
				tagged := name != ""
				if name != "" || !field.Anonymous || fieldType.Kind() != reflect.Struct {
					if name == "" {
						name = field.Name
					}
					candidates = append(candidates, jsonField{
						name: name, typ: field.Type, index: index, tagged: tagged,
						omitEmpty: options["omitempty"], omitZero: options["omitzero"],
						stringEncoded: options["string"], bindingRequired: parseBindingRequired(field.Tag.Get("binding")),
						throughPointer:    throughPointer,
						unexportedPointer: unexportedPointer || field.Anonymous && !field.IsExported() && field.Type.Kind() == reflect.Pointer,
					})
					continue
				}
				next = append(next, jsonFieldLevel{typ: fieldType, index: index, throughPointer: nextThroughPointer, unexportedPointer: nextUnexportedPointer})
			}
		}
		for typ := range processed {
			visited[typ] = true
		}
		current = next
	}

	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.name != b.name {
			return a.name < b.name
		}
		if len(a.index) != len(b.index) {
			return len(a.index) < len(b.index)
		}
		if a.tagged != b.tagged {
			return a.tagged
		}
		return lessIndex(a.index, b.index)
	})
	selected := make([]jsonField, 0, len(candidates))
	for index := 0; index < len(candidates); {
		next := index + 1
		for next < len(candidates) && candidates[next].name == candidates[index].name {
			next++
		}
		group := candidates[index:next]
		if len(group) == 1 || len(group[0].index) < len(group[1].index) || group[0].tagged != group[1].tagged {
			selected = append(selected, group[0])
		}
		index = next
	}
	sort.Slice(selected, func(i, j int) bool { return lessIndex(selected[i].index, selected[j].index) })
	return selected
}

func lessIndex(left, right []int) bool {
	for index := 0; index < len(left) && index < len(right); index++ {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return len(left) < len(right)
}

func parseJSONTag(raw string) (string, map[string]bool) {
	parts := strings.Split(raw, ",")
	options := map[string]bool{}
	for _, option := range parts[1:] {
		options[option] = true
	}
	return parts[0], options
}

func omitemptyCanOmit(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.Array:
		return typ.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.String:
		return true
	default:
		return false
	}
}

func validJSONTagName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			continue
		}
		switch character {
		case '!', '#', '$', '%', '&', '(', ')', '*', '+', '-', '.', '/', ':', ';', '<', '=', '>', '?', '@', '[', ']', '^', '_', '{', '|', '}', '~', ' ':
			continue
		}
		return false
	}
	return true
}

func parseBindingRequired(raw string) bool {
	for _, option := range strings.Split(raw, ",") {
		if strings.TrimSpace(option) == "required" {
			return true
		}
	}
	return false
}

func supportsStringOption(typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}
