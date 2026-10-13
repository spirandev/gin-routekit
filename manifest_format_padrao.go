package routekit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const BodyFormatPadrao = "padrao"

// PadraoBodyFormat returns the built-in "padrao" manifest format.
func PadraoBodyFormat() BodyFormat {
	return padraoBodyFormat{}
}

type padraoBodyFormat struct{}

type padraoManifest struct {
	Version string        `json:"version"`
	Routes  []padraoRoute `json:"routes"`
}

type padraoRoute struct {
	RouteID               int32    `json:"route_id"`
	Method                string   `json:"method"`
	Path                  string   `json:"path"`
	Definition            string   `json:"definition"`
	Category              string   `json:"category"`
	IsAuthentication      bool     `json:"is_authentication"`
	IsAuthorization       bool     `json:"is_authorization"`
	RequiresClientContext bool     `json:"requires_client_context"`
	AccessTypes           []string `json:"access_types"`
	Scopes                []string `json:"scopes"`
	IsIntegration         bool     `json:"is_integration"`
	M2M                   bool     `json:"m2m"`
	IsBasic               bool     `json:"is_basic"`
	Restricted            bool     `json:"restricted"`
}

type padraoResponse struct {
	Message string          `json:"message"`
	Code    string          `json:"code"`
	Data    map[string]any  `json:"data"`
	Errors  json.RawMessage `json:"errors"`
	Details map[string]any  `json:"details"`
}

type padraoResponseError struct {
	RouteIndex *int   `json:"route_index"`
	RouteID    *int32 `json:"route_id"`
	Field      string `json:"field"`
	Message    string `json:"message"`
}

func (padraoBodyFormat) Name() string {
	return BodyFormatPadrao
}

func (padraoBodyFormat) ContentType() string {
	return "application/json"
}

func (padraoBodyFormat) Build(input ManifestInput) ([]byte, error) {
	var buildErrors []ManifestSyncError
	if strings.TrimSpace(input.Version) == "" {
		buildErrors = append(buildErrors, ManifestSyncError{Field: "version", Message: "version is required"})
	}

	routes := []padraoRoute{}
	seen := map[int32]int{}
	for _, route := range input.Routes {
		for _, handler := range route.Handlers {
			index := len(routes)
			routeID := handler.RouteId
			if routeID <= 0 {
				buildErrors = append(buildErrors, ManifestSyncError{
					RouteIndex: &index, RouteID: &routeID, Field: "route_id",
					Message: fmt.Sprintf("route %d (%s %s): route_id must be greater than zero, got %d", index, handler.Method, handler.RelativePath, routeID),
				})
			} else if first, exists := seen[routeID]; exists {
				buildErrors = append(buildErrors, ManifestSyncError{
					RouteIndex: &index, RouteID: &routeID, Field: "route_id",
					Message: fmt.Sprintf("route %d (%s %s): route_id %d duplicates route %d", index, handler.Method, handler.RelativePath, routeID, first),
				})
			} else {
				seen[routeID] = index
			}

			category := strings.TrimSpace(handler.Category)
			if category == "" {
				category = route.Group
			}

			routes = append(routes, padraoRoute{
				RouteID:               routeID,
				Method:                handler.Method,
				Path:                  registeredRoutePath(route.Path, handler.RelativePath),
				Definition:            handler.Definition,
				Category:              category,
				IsAuthentication:      boolValue(handler.IsAuthentication, true),
				IsAuthorization:       boolValue(handler.IsAuthorization, true),
				RequiresClientContext: boolValue(handler.RequiresClientContext, false),
				AccessTypes:           append([]string{}, handler.AccessTypes...),
				Scopes:                append([]string{}, handler.Scopes...),
				IsIntegration:         boolValue(handler.IsIntegration, false),
				M2M:                   boolValue(handler.IsM2M, false),
				IsBasic:               boolValue(handler.IsBasic, false),
				Restricted:            boolValue(handler.IsRestricted, false),
			})
		}
	}

	if len(buildErrors) > 0 {
		return nil, &ManifestBuildError{Errors: buildErrors}
	}

	sort.SliceStable(routes, func(i, j int) bool {
		return routes[i].RouteID < routes[j].RouteID
	})
	return json.Marshal(padraoManifest{Version: input.Version, Routes: routes})
}

func (padraoBodyFormat) ParseResponse(statusCode int, body []byte) ManifestSyncResult {
	var response padraoResponse
	decoded := json.Unmarshal(body, &response) == nil

	if isSuccessStatus(statusCode) {
		result := ManifestSyncResult{Status: ManifestStatusApplied}
		if !decoded {
			return result
		}
		result.Message = response.Message
		result.Code = response.Code
		result.Details = response.Data
		if status, ok := response.Data["status"].(string); ok {
			switch status {
			case ManifestStatusApplied, ManifestStatusUnchanged, ManifestStatusValid:
				result.Status = status
			}
		}
		return result
	}

	result := ManifestSyncResult{Status: ManifestStatusRejected}
	if statusCode >= 500 {
		result.Status = ManifestStatusError
	}
	if !decoded {
		return result
	}
	result.Message = response.Message
	result.Code = response.Code
	result.Details = response.Details

	// errors may be a plain list of codes (e.g. ["CODE"]); only the structured
	// per-route form is exposed.
	var responseErrors []padraoResponseError
	if len(response.Errors) > 0 && json.Unmarshal(response.Errors, &responseErrors) == nil {
		for _, item := range responseErrors {
			result.Errors = append(result.Errors, ManifestSyncError{
				RouteIndex: item.RouteIndex,
				RouteID:    item.RouteID,
				Field:      item.Field,
				Message:    item.Message,
			})
		}
	}
	return result
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
