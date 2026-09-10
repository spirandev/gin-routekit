package routekit

import (
	"reflect"
	"strings"
	"testing"
)

func tagGroupsConfig() OpenAPIConfig {
	config := baseConfig()
	config.TagGroups = []OpenAPITagGroup{
		{Name: "Instances", Tags: []string{"instances"}, Description: "Instance management"},
	}
	return config
}

func marshalDoc(t *testing.T, doc *OpenAPIDocument) string {
	t.Helper()
	payload, err := MarshalOpenAPI(doc)
	if err != nil {
		t.Fatalf("MarshalOpenAPI: %v", err)
	}
	return string(payload)
}

func assertDiagnostic(t *testing.T, report DiagnosticReport, code string, severity DiagnosticSeverity, location string) {
	t.Helper()
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code != code {
			continue
		}
		if diagnostic.Severity != severity {
			t.Fatalf("diagnostic %s severity = %q, want %q", code, diagnostic.Severity, severity)
		}
		if diagnostic.Location != location {
			t.Fatalf("diagnostic %s location = %q, want %q", code, diagnostic.Location, location)
		}
		return
	}
	t.Fatalf("expected diagnostic %s, got %+v", code, report.Diagnostics)
}

func TestTagGroupsEmittedAsExtension(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "instances", 123)
	group.group.GET("/instances", okHandler, "List instances", 1).Document().
		Response(200, "OK", nil)

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, tagGroupsConfig())

	if len(doc.TagGroups) != 1 {
		t.Fatalf("expected 1 tag group, got %d", len(doc.TagGroups))
	}
	tagGroup := doc.TagGroups[0]
	if tagGroup.Name != "Instances" || tagGroup.Description != "Instance management" || !reflect.DeepEqual(tagGroup.Tags, []string{"instances"}) {
		t.Fatalf("unexpected tag group %+v", tagGroup)
	}

	assertContains(t, marshalDoc(t, doc), `"x-tagGroups"`)
	assertContains(t, marshalDoc(t, doc), `"name": "Instances"`)
	assertContains(t, marshalDoc(t, doc), `"instances"`)
}

func TestTagGroupsNotEmittedWithoutConfig(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "instances", 123)
	group.group.GET("/instances", okHandler, "List instances", 1).Document().
		Response(200, "OK", nil)

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())

	if doc.TagGroups != nil {
		t.Fatalf("expected no tag groups, got %+v", doc.TagGroups)
	}
	assertNotContains(t, marshalDoc(t, doc), `"x-tagGroups"`)
}

func TestTagGroupsUnknownTagReturnsDiagnostic(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "instances", 123)
	group.group.GET("/instances", okHandler, "List instances", 1).Document().
		Response(200, "OK", nil)

	ar := newTestEngine(t, group)
	config := tagGroupsConfig()
	config.TagGroups[0].Tags = []string{"missing"}

	report, err := ValidateOpenAPI(ar.routes, config)
	if err == nil {
		t.Fatal("expected error")
	}
	assertDiagnostic(t, report, "tag_groups.tag.unknown", DiagnosticError, "config.tagGroups[0].tags[0]")
}

func TestTagGroupsDuplicateName(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "instances", 123)
	group.group.GET("/instances", okHandler, "List instances", 1).Document().
		Response(200, "OK", nil)

	ar := newTestEngine(t, group)
	config := tagGroupsConfig()
	config.TagGroups = append(config.TagGroups, OpenAPITagGroup{Name: "Instances", Tags: []string{"instances"}})

	report, err := ValidateOpenAPI(ar.routes, config)
	if err == nil {
		t.Fatal("expected error")
	}
	assertDiagnostic(t, report, "tag_groups.name.duplicate", DiagnosticError, "config.tagGroups[1].name")
}

func TestTagGroupsInvalidName(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "instances", 123)
	group.group.GET("/instances", okHandler, "List instances", 1).Document().
		Response(200, "OK", nil)

	ar := newTestEngine(t, group)
	config := tagGroupsConfig()
	config.TagGroups[0].Name = ""

	report, err := ValidateOpenAPI(ar.routes, config)
	if err == nil {
		t.Fatal("expected error")
	}
	assertDiagnostic(t, report, "tag_groups.name.invalid", DiagnosticError, "config.tagGroups[0].name")
}

func TestSectionRecordedInDocsExtension(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "instances", 123)
	group.group.GET("/instances", okHandler, "List instances", 1).Document().
		Section("Instances", "V1").
		Response(200, "OK", nil)

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())

	operationID := doc.Paths["/api/instances"].Get.OperationID
	if doc.RoutekitDocs == nil {
		t.Fatal("expected x-routekit-docs to be emitted")
	}
	section, ok := doc.RoutekitDocs.Sections[operationID]
	if !ok {
		t.Fatalf("expected section for operationId %q, got %+v", operationID, doc.RoutekitDocs.Sections)
	}
	if !reflect.DeepEqual(section, []string{"Instances", "V1"}) {
		t.Fatalf("section = %v, want [Instances V1]", section)
	}

	assertContains(t, marshalDoc(t, doc), `"x-routekit-docs"`)
	assertContains(t, marshalDoc(t, doc), `"sections"`)
}

func TestSectionTrimsEmptySegments(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "instances", 123)
	group.group.GET("/instances", okHandler, "List instances", 1).Document().
		Section(" Instances ", "").
		Response(200, "OK", nil)

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())

	operationID := doc.Paths["/api/instances"].Get.OperationID
	section := doc.RoutekitDocs.Sections[operationID]
	if !reflect.DeepEqual(section, []string{"Instances"}) {
		t.Fatalf("section = %v, want [Instances]", section)
	}
}

func TestSectionOnlyFluent(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "instances", 123)
	route := group.group.GET("/instances", okHandler, "List instances", 1).Document().
		Response(200, "OK", nil)
	if route.Section("", "   ") != route {
		t.Fatal("expected Section to return the route")
	}

	ar := newTestEngine(t, group)
	doc := buildDoc(t, ar, baseConfig())
	if doc.RoutekitDocs != nil {
		t.Fatalf("expected no x-routekit-docs, got %+v", doc.RoutekitDocs)
	}
}

func TestRoutesClonePreservesSection(t *testing.T) {
	engine := newEngine()
	group := newGroup(t, engine, "/api", "instances", 123)
	group.group.GET("/instances", okHandler, "List instances", 1).Document().
		Section("Instances", "V1").
		Response(200, "OK", nil)

	ar := newTestEngine(t, group)
	if err := ar.RegisterRoutes(engine); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	routes := ar.Routes()
	if !reflect.DeepEqual(routes[0].Handlers[0].Doc.Section, []string{"Instances", "V1"}) {
		t.Fatalf("cloneDocConfig did not preserve Section: %v", routes[0].Handlers[0].Doc.Section)
	}

	routes[0].Handlers[0].Doc.Section[0] = "Mutated"
	second := ar.Routes()
	if second[0].Handlers[0].Doc.Section[0] != "Instances" {
		t.Fatal("Routes() did not return a defensive copy of Section")
	}
}

func TestGoldenDocumentWithoutOptionalMetadata(t *testing.T) {
	type createUserRequest struct {
		Name string `json:"name"`
	}
	type userResponse struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	engine := newEngine()
	first := newGroup(t, engine, "/api/v1", "instances-v1", 1)
	first.group.GET("/instances", okHandler, "List instances", 10).Document().
		DocProfile("tenant").
		Response(200, "OK", nil)
	first.group.POST("/instances", okHandler, "Create instance", 11).Document().
		Header("X-Request-ID", "string", true, "Request correlation ID").
		Body(SchemaOf[createUserRequest]()).
		Response(201, "Created", SchemaOf[userResponse]())
	second := newGroup(t, engine, "/api/v2", "instances-v2", 2)
	second.group.GET("/instances", okHandler, "List instances", 20).Document().
		Response(200, "OK", nil)

	ar := newTestEngine(t, first, second)
	config := OpenAPIConfig{
		Title:             "Test API",
		Version:           "1.0.0",
		JSONPath:          "/openapi.json",
		BasePath:          "/",
		PathMode:          FullRegisteredPaths,
		DocumentationMode: DocumentAll,
		Defaults:          WithJSONDefaults(DefaultResponseOf[userResponse](500, "Internal Server Error")),
		Profiles: map[string]DocProfile{
			"tenant": {Description: "Tenant scoped"},
		},
	}

	doc := buildDoc(t, ar, config)
	if doc.TagGroups != nil {
		t.Fatalf("expected no tag groups, got %+v", doc.TagGroups)
	}
	if doc.RoutekitDocs != nil {
		t.Fatalf("expected no x-routekit-docs, got %+v", doc.RoutekitDocs)
	}

	output := marshalDoc(t, doc)
	assertNotContains(t, output, `"x-tagGroups"`)
	assertNotContains(t, output, `"x-routekit-docs"`)
	assertContains(t, output, `"x-routekit-profiles"`)
	if strings.Contains(output, `"deprecated"`) {
		t.Fatal("golden document must not contain deprecated keys")
	}
}
