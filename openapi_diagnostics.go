package routekit

import (
	"fmt"
	"sort"
	"strings"
)

type DiagnosticSeverity string

const (
	DiagnosticError   DiagnosticSeverity = "error"
	DiagnosticWarning DiagnosticSeverity = "warning"
)

type Diagnostic struct {
	Code     string             `json:"code"`
	Severity DiagnosticSeverity `json:"severity"`
	Method   string             `json:"method,omitempty"`
	Path     string             `json:"path,omitempty"`
	Route    string             `json:"route,omitempty"`
	Location string             `json:"location,omitempty"`
	Message  string             `json:"message"`
}

type DiagnosticReport struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func (r DiagnosticReport) HasErrors() bool {
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Severity == DiagnosticError {
			return true
		}
	}
	return false
}

type DiagnosticsError struct {
	Report DiagnosticReport
}

func (e *DiagnosticsError) Error() string {
	if e == nil {
		return "OpenAPI validation failed"
	}
	messages := make([]string, 0, len(e.Report.Diagnostics))
	for _, diagnostic := range e.Report.Diagnostics {
		if diagnostic.Severity == DiagnosticError {
			messages = append(messages, diagnostic.Message)
		}
	}
	if len(messages) == 0 {
		return "OpenAPI validation failed"
	}
	return fmt.Sprintf("OpenAPI validation failed: %s", strings.Join(messages, "; "))
}

type diagnosticCollector struct {
	diagnostics []Diagnostic
}

func (c *diagnosticCollector) add(diagnostic Diagnostic) {
	if diagnostic.Severity == "" {
		diagnostic.Severity = DiagnosticError
	}
	c.diagnostics = append(c.diagnostics, diagnostic)
}

func (c *diagnosticCollector) route(code string, severity DiagnosticSeverity, route Route, handler Handler, location, message string) {
	path := registeredRoutePath(route.Path, handler.RelativePath)
	c.add(Diagnostic{
		Code: code, Severity: severity, Method: handler.Method, Path: path,
		Route: handler.Definition, Location: location, Message: message,
	})
}

func (c *diagnosticCollector) report() DiagnosticReport {
	diagnostics := append([]Diagnostic(nil), c.diagnostics...)
	sort.SliceStable(diagnostics, func(i, j int) bool {
		a, b := diagnostics[i], diagnostics[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Method != b.Method {
			return a.Method < b.Method
		}
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Location != b.Location {
			return a.Location < b.Location
		}
		return a.Message < b.Message
	})
	return DiagnosticReport{Diagnostics: diagnostics}
}
