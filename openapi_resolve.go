package routekit

import (
	"fmt"
	"strings"
)

type resolvedDocumentation struct {
	Enabled             bool
	Summary             string
	Description         string
	Tags                []string
	OperationID         string
	Parameters          []DocParam
	RequestBody         *DocBody
	Responses           []DocResponse
	Security            []OpenAPISecurityRequirement
	RequestContentType  string
	ResponseContentType string
}

type docLayer struct {
	name                string
	profiles            []string
	parameters          []DocParam
	requestBody         *DocBody
	responses           []DocResponse
	security            []OpenAPISecurityRequirement
	requestContentType  string
	responseContentType string
}

type documentationAccumulator struct {
	profiles            []string
	seenProfiles        map[string]bool
	parameters          []DocParam
	parameterIndex      map[paramKey]int
	responses           []DocResponse
	responseIndex       map[int]int
	requestBody         *DocBody
	security            []OpenAPISecurityRequirement
	requestContentType  string
	responseContentType string
	profileTombstones   map[string]bool
	responseTombstones  map[int]bool
	parameterTombstones map[paramKey]bool
}

func resolveRouteDocumentation(config OpenAPIConfig, route Route, handler Handler) (*resolvedDocumentation, error) {
	enabled, err := resolveDocumentationEnabled(config, route, handler)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return &resolvedDocumentation{Enabled: false}, nil
	}

	acc := newDocumentationAccumulator(handler.DocRemovals)
	layers := []docLayer{
		layerFromDefaults("global defaults", config.Defaults),
		layerFromDefaults("group defaults", route.DocumentationDefaults),
	}
	for i, metadata := range route.GroupMiddlewareMetadata {
		layers = append(layers, layerFromMiddleware(fmt.Sprintf("group middleware %d", i), metadata))
	}
	for i, metadata := range handler.MiddlewareMetadata {
		layers = append(layers, layerFromMiddleware(fmt.Sprintf("route middleware %d", i), metadata))
	}
	for _, layer := range layers {
		if err := acc.applyLayer(config, layer, true); err != nil {
			return nil, err
		}
	}

	acc.applyTombstones()

	if handler.Contract != nil {
		if err := acc.applyLayer(config, layerFromContract("contract", *handler.Contract), false); err != nil {
			return nil, err
		}
	}
	if handler.Doc != nil {
		if err := acc.applyLayer(config, layerFromDocConfig("endpoint", *handler.Doc), false); err != nil {
			return nil, err
		}
	}

	summary := handler.Definition
	description := ""
	tags := []string{route.Group}
	operationID := ""
	if handler.Doc != nil {
		if handler.Doc.Summary != "" {
			summary = handler.Doc.Summary
		}
		if handler.Doc.Description != "" {
			description = handler.Doc.Description
		}
		if len(handler.Doc.Tags) > 0 {
			tags = append([]string(nil), handler.Doc.Tags...)
		}
		operationID = handler.Doc.OperationID
	}

	return &resolvedDocumentation{
		Enabled:             true,
		Summary:             summary,
		Description:         description,
		Tags:                tags,
		OperationID:         operationID,
		Parameters:          cloneDocParams(acc.parameters),
		RequestBody:         cloneDocBody(acc.requestBody),
		Responses:           cloneDocResponses(acc.responses),
		Security:            cloneSecurityRequirements(acc.security),
		RequestContentType:  acc.requestContentType,
		ResponseContentType: acc.responseContentType,
	}, nil
}

func resolveDocumentationEnabled(config OpenAPIConfig, route Route, handler Handler) (bool, error) {
	var enabled bool
	switch config.DocumentationMode {
	case DocumentationModeUnspecified:
		enabled = config.EnabledByDefault
	case DocumentAll:
		enabled = true
	case DocumentOptIn:
		enabled = false
	default:
		return false, fmt.Errorf("unknown DocumentationMode %q", config.DocumentationMode)
	}
	if config.Defaults.Enabled != nil {
		enabled = *config.Defaults.Enabled
	}
	if route.DocumentationDefaults.Enabled != nil {
		enabled = *route.DocumentationDefaults.Enabled
	}
	if handler.Doc != nil && handler.Doc.Enabled != nil {
		enabled = *handler.Doc.Enabled
	}
	return enabled, nil
}

func newDocumentationAccumulator(removals docRemovals) *documentationAccumulator {
	profileTombstones := map[string]bool{}
	for _, name := range removals.Profiles {
		profileTombstones[name] = true
	}
	responseTombstones := map[int]bool{}
	for _, status := range removals.Responses {
		responseTombstones[status] = true
	}
	parameterTombstones := map[paramKey]bool{}
	for _, removal := range removals.Parameters {
		parameterTombstones[docParamKey(DocParam{In: removal.In, Name: removal.Name})] = true
	}
	return &documentationAccumulator{
		seenProfiles:        map[string]bool{},
		parameterIndex:      map[paramKey]int{},
		responseIndex:       map[int]int{},
		profileTombstones:   profileTombstones,
		responseTombstones:  responseTombstones,
		parameterTombstones: parameterTombstones,
	}
}

func layerFromDefaults(name string, defaults DocumentationDefaults) docLayer {
	parameters := cloneDocParams(defaults.Headers)
	parameters = append(parameters, cloneDocParams(defaults.QueryParams)...)
	parameters = append(parameters, cloneDocParams(defaults.PathParams)...)
	return docLayer{
		name:                name,
		profiles:            append([]string(nil), defaults.Profiles...),
		parameters:          parameters,
		responses:           cloneDocResponses(defaults.Responses),
		requestContentType:  defaults.RequestContentType,
		responseContentType: defaults.ResponseContentType,
	}
}

func layerFromMiddleware(name string, metadata MiddlewareMetadata) docLayer {
	return docLayer{
		name:       name,
		profiles:   append([]string(nil), metadata.Profiles...),
		parameters: cloneDocParams(metadata.Parameters),
		responses:  cloneDocResponses(metadata.Responses),
		security:   cloneSecurityRequirements(metadata.Security),
	}
}

func layerFromContract(name string, contract Contract) docLayer {
	return docLayer{
		name:        name,
		profiles:    append([]string(nil), contract.Profiles...),
		parameters:  cloneDocParams(contract.Parameters),
		requestBody: cloneDocBody(contract.RequestBody),
		responses:   cloneDocResponses(contract.Responses),
	}
}

func layerFromDocConfig(name string, doc DocConfig) docLayer {
	parameters := cloneDocParams(doc.Headers)
	parameters = append(parameters, cloneDocParams(doc.QueryParams)...)
	parameters = append(parameters, cloneDocParams(doc.PathParams)...)
	return docLayer{
		name:        name,
		profiles:    append([]string(nil), doc.Profiles...),
		parameters:  parameters,
		requestBody: cloneDocBody(doc.RequestBody),
		responses:   cloneDocResponses(doc.Responses),
	}
}

func (acc *documentationAccumulator) applyLayer(config OpenAPIConfig, layer docLayer, inherited bool) error {
	if layer.requestContentType != "" {
		acc.requestContentType = layer.requestContentType
	}
	if layer.responseContentType != "" {
		acc.responseContentType = layer.responseContentType
	}

	expanded, err := expandLayerProfiles(config, layer, inherited, acc.profileTombstones)
	if err != nil {
		return err
	}
	for _, profileName := range expanded.profileNames {
		if acc.seenProfiles[profileName] {
			continue
		}
		acc.seenProfiles[profileName] = true
		acc.profiles = append(acc.profiles, profileName)
	}
	if err := acc.applyParams(layer.name, expanded.parameters); err != nil {
		return err
	}
	if err := acc.applyParams(layer.name, layer.parameters); err != nil {
		return err
	}
	if err := acc.applyResponses(layer.name, layer.responses); err != nil {
		return err
	}
	for _, requirement := range expanded.security {
		acc.addSecurity(requirement)
	}
	for _, requirement := range layer.security {
		acc.addSecurity(requirement)
	}
	if layer.requestBody != nil {
		acc.requestBody = cloneDocBody(layer.requestBody)
	}
	return nil
}

type expandedProfiles struct {
	profileNames []string
	parameters   []DocParam
	security     []OpenAPISecurityRequirement
}

func expandLayerProfiles(config OpenAPIConfig, layer docLayer, inherited bool, tombstones map[string]bool) (expandedProfiles, error) {
	var expanded expandedProfiles
	for _, name := range layer.profiles {
		if inherited && tombstones[name] {
			continue
		}
		profile, ok := config.Profiles[name]
		if !ok {
			return expandedProfiles{}, fmt.Errorf("%s references profile %q not found in OpenAPIConfig.Profiles", layer.name, name)
		}
		expanded.profileNames = append(expanded.profileNames, name)
		expanded.parameters = append(expanded.parameters, cloneDocParams(profile.Headers)...)
		expanded.parameters = append(expanded.parameters, cloneDocParams(profile.QueryParams)...)
		expanded.parameters = append(expanded.parameters, cloneDocParams(profile.PathParams)...)
		for _, securityName := range profile.Security {
			expanded.security = append(expanded.security, OpenAPISecurityRequirement{securityName: {}})
		}
	}
	return expanded, nil
}

func (acc *documentationAccumulator) applyParams(layerName string, params []DocParam) error {
	seenInLayer := map[paramKey]DocParam{}
	for _, param := range params {
		param = cloneDocParam(param)
		key := docParamKey(param)
		if existing, ok := seenInLayer[key]; ok {
			if !docParamsEqual(existing, param) {
				return fmt.Errorf("%s has conflicting parameter %q in %s", layerName, param.Name, param.In)
			}
			continue
		}
		seenInLayer[key] = param
		if idx, ok := acc.parameterIndex[key]; ok {
			acc.parameters[idx] = param
			continue
		}
		acc.parameterIndex[key] = len(acc.parameters)
		acc.parameters = append(acc.parameters, param)
	}
	return nil
}

func (acc *documentationAccumulator) applyResponses(layerName string, responses []DocResponse) error {
	seenInLayer := map[int]DocResponse{}
	for _, response := range responses {
		response = cloneDocResponse(response)
		if existing, ok := seenInLayer[response.Status]; ok {
			if !docResponsesEqual(existing, response) {
				return fmt.Errorf("%s has conflicting response status %d", layerName, response.Status)
			}
			continue
		}
		seenInLayer[response.Status] = response
		if idx, ok := acc.responseIndex[response.Status]; ok {
			acc.responses[idx] = response
			continue
		}
		acc.responseIndex[response.Status] = len(acc.responses)
		acc.responses = append(acc.responses, response)
	}
	return nil
}

func (acc *documentationAccumulator) addSecurity(requirement OpenAPISecurityRequirement) {
	requirement = cloneSecurityRequirement(requirement)
	for _, existing := range acc.security {
		if securityRequirementsEqual(existing, requirement) {
			return
		}
	}
	acc.security = append(acc.security, requirement)
}

func (acc *documentationAccumulator) applyTombstones() {
	if len(acc.profileTombstones) > 0 {
		profiles := acc.profiles[:0]
		for _, name := range acc.profiles {
			if !acc.profileTombstones[name] {
				profiles = append(profiles, name)
			}
		}
		acc.profiles = profiles
	}
	if len(acc.parameterTombstones) > 0 {
		params := acc.parameters[:0]
		acc.parameterIndex = map[paramKey]int{}
		for _, param := range acc.parameters {
			key := docParamKey(param)
			if acc.parameterTombstones[key] {
				continue
			}
			acc.parameterIndex[key] = len(params)
			params = append(params, param)
		}
		acc.parameters = params
	}
	if len(acc.responseTombstones) > 0 {
		responses := acc.responses[:0]
		acc.responseIndex = map[int]int{}
		for _, response := range acc.responses {
			if acc.responseTombstones[response.Status] {
				continue
			}
			acc.responseIndex[response.Status] = len(responses)
			responses = append(responses, response)
		}
		acc.responses = responses
	}
}

func docParamKey(param DocParam) paramKey {
	name := param.Name
	if param.In == DocParamInHeader {
		name = strings.ToLower(name)
	}
	return paramKey{name: name, in: string(param.In)}
}

func docParamsEqual(a, b DocParam) bool {
	return docParamKey(a) == docParamKey(b) && a.Type == b.Type && a.Required == b.Required && a.Description == b.Description && valuesEqual(a.Example, b.Example)
}

func docResponsesEqual(a, b DocResponse) bool {
	return a.Status == b.Status && a.Description == b.Description && a.ContentType == b.ContentType && valuesEqual(a.Schema, b.Schema) && valuesEqual(a.Example, b.Example)
}

func securityRequirementsEqual(a, b OpenAPISecurityRequirement) bool {
	return valuesEqual(a, b)
}
