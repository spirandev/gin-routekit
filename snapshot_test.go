package routekit

import (
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

// fullHandler returns a Handler with every field set to a non-zero value.
func fullHandler() Handler {
	return Handler{
		Handler:                   okHandler,
		Middleware:                []gin.HandlerFunc{okHandler},
		Method:                    "GET",
		Path:                      "/resource",
		Definition:                "resource",
		RouteId:                   7,
		RelativePath:              "/resource",
		IsAuthentication:          boolPtr(true),
		IsAuthorization:           boolPtr(true),
		RequiresClientContext:     boolPtr(true),
		IsBasic:                   boolPtr(true),
		IsM2M:                     boolPtr(true),
		IsSameApplicationRequired: boolPtr(true),
		IsIntegration:             boolPtr(true),
		IsRestricted:              boolPtr(true),
		Scopes:                    []string{"scope:a"},
		Doc:                       &DocConfig{Summary: "summary", Tags: []string{"tag"}},
		Contract:                  &Contract{Profiles: []string{"profile"}},
		DocRemovals:               docRemovals{Profiles: []string{"removed"}},
		MiddlewareMetadata:        []MiddlewareMetadata{{Profiles: []string{"mw"}}},
	}
}

func TestCloneHandlersPreservesEveryField(t *testing.T) {
	original := fullHandler()

	origValue := reflect.ValueOf(original)
	for i := 0; i < origValue.NumField(); i++ {
		if origValue.Field(i).IsZero() {
			t.Fatalf("fullHandler must set every field; %s is zero", origValue.Type().Field(i).Name)
		}
	}

	cloned := cloneHandlers([]Handler{original})[0]
	clonedValue := reflect.ValueOf(cloned)
	for i := 0; i < clonedValue.NumField(); i++ {
		name := clonedValue.Type().Field(i).Name
		if clonedValue.Field(i).IsZero() {
			t.Errorf("cloned Handler lost field %s", name)
			continue
		}
		origField, clonedField := origValue.Field(i), clonedValue.Field(i)
		switch {
		case origField.Kind() == reflect.Func:
			if origField.Pointer() != clonedField.Pointer() {
				t.Errorf("cloned Handler changed func field %s", name)
			}
		case origField.Kind() == reflect.Slice && origField.Type().Elem().Kind() == reflect.Func:
			if origField.Len() != clonedField.Len() {
				t.Errorf("cloned Handler changed len of %s", name)
			}
		default:
			if !reflect.DeepEqual(origField.Interface(), clonedField.Interface()) {
				t.Errorf("cloned Handler field %s = %#v, want %#v", name, clonedField.Interface(), origField.Interface())
			}
		}
	}
}

func TestCloneHandlersIsIndependentFromOriginal(t *testing.T) {
	handlers := []Handler{fullHandler()}
	cloned := cloneHandlers(handlers)

	original := &handlers[0]
	assertNoSharedReferences(t, "Handler", reflect.ValueOf(original).Elem(), reflect.ValueOf(cloned[0]))

	if cloned[0].IsRestricted == nil || cloned[0].IsIntegration == nil {
		t.Fatal("cloned Handler lost a bool pointer field")
	}

	*original.IsRestricted = false
	*original.IsIntegration = false
	original.Scopes[0] = "mutated"
	original.Middleware[0] = nil
	original.Doc.Tags[0] = "mutated"
	original.Contract.Profiles[0] = "mutated"
	original.DocRemovals.Profiles[0] = "mutated"
	original.MiddlewareMetadata[0].Profiles[0] = "mutated"

	got := cloned[0]
	if !*got.IsRestricted || !*got.IsIntegration {
		t.Error("mutating original bool pointers changed the clone")
	}
	if got.Scopes[0] != "scope:a" {
		t.Error("mutating original Scopes changed the clone")
	}
	if got.Middleware[0] == nil {
		t.Error("mutating original Middleware changed the clone")
	}
	if got.Doc.Tags[0] != "tag" {
		t.Error("mutating original Doc changed the clone")
	}
	if got.Contract.Profiles[0] != "profile" {
		t.Error("mutating original Contract changed the clone")
	}
	if got.DocRemovals.Profiles[0] != "removed" {
		t.Error("mutating original DocRemovals changed the clone")
	}
	if got.MiddlewareMetadata[0].Profiles[0] != "mw" {
		t.Error("mutating original MiddlewareMetadata changed the clone")
	}
}

// assertNoSharedReferences fails when a non-nil pointer, slice or map in orig
// shares its backing memory with the same field in clone, descending into
// nested struct values.
func assertNoSharedReferences(t *testing.T, path string, orig, clone reflect.Value) {
	t.Helper()
	for i := 0; i < orig.NumField(); i++ {
		name := path + "." + orig.Type().Field(i).Name
		origField, clonedField := orig.Field(i), clone.Field(i)
		switch origField.Kind() {
		case reflect.Ptr, reflect.Map:
			if !origField.IsNil() && origField.Pointer() == clonedField.Pointer() {
				t.Errorf("cloned %s shares memory with original", name)
			}
		case reflect.Slice:
			if origField.Len() > 0 && origField.Pointer() == clonedField.Pointer() {
				t.Errorf("cloned %s shares memory with original", name)
			}
		case reflect.Struct:
			assertNoSharedReferences(t, name, origField, clonedField)
		}
	}
}

func TestRestrictSurvivesExportAndSnapshot(t *testing.T) {
	newRestrictedGroup := func(engine *gin.Engine) groupRegistrar {
		group := newGroup(t, engine, "/api", "api", 123)
		group.group.GET("/restricted", okHandler, "restricted", 1).Restrict()
		group.group.GET("/open", okHandler, "open", 2)
		return group
	}

	assertRestricted := func(t *testing.T, source string, handlers []Handler) {
		t.Helper()
		if len(handlers) != 2 {
			t.Fatalf("%s: expected 2 handlers, got %d", source, len(handlers))
		}
		if handlers[0].IsRestricted == nil || !*handlers[0].IsRestricted {
			t.Errorf("%s: restricted route lost IsRestricted", source)
		}
		if handlers[1].IsRestricted == nil || *handlers[1].IsRestricted {
			t.Errorf("%s: open route IsRestricted should be false", source)
		}
	}

	assertRestricted(t, "Export", newRestrictedGroup(newEngine()).group.Export("api", 123).Handlers)

	engine := newEngine()
	ar := newTestEngine(t, newRestrictedGroup(engine))
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	routes := ar.Routes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	assertRestricted(t, "AppRouter.Routes", routes[0].Handlers)
}
