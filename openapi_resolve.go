package routekit

import (
	"errors"
	"fmt"
	"strings"
)

type resolvedDocumentation struct {
	Enabled             bool
	Deprecated          bool
	Summary             string
	Description         string
	Tags                []string
	Section             []string
	OperationID         string
	Parameters          []DocParam
	RequestBody         *DocBody
	Responses           []DocResponse
	Security            []OpenAPISecurityRequirement
	RequestContentType  string
	ResponseContentType string
	Profiles            []string
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
	profiles                []string
	seenProfiles            map[string]bool
	parameters              []DocParam
	parameterIndex          map[paramKey]int
	responses               []DocResponse
	responseIndex           map[int]int
	requestBody             *DocBody
	security                []OpenAPISecurityRequirement
	requestContentType      string
	responseContentType     string
	profileTombstones       map[string]bool
	responseTombstones      map[int]bool
	parameterTombstones     map[paramKey]bool
	profileParameters       map[paramKey]DocParam
	profileSources          map[paramKey]string
	profileTombstoneTargets map[string]bool
}

func resolveRouteDocumentation(config OpenAPIConfig, route Route, handler Handler, collector *diagnosticCollector) (*resolvedDocumentation, error) {
	enabled, err := resolveDocumentationEnabled(config, route, handler)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return &resolvedDocumentation{Enabled: false}, nil
	}

	acc := newDocumentationAccumulator(handler.DocRemovals)
	var resolutionErrors []error
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
	for i, rule := range config.SecurityRules {
		if metadata, ok := securityRuleMetadata(rule, route, handler, collector); ok {
			layers = append(layers, layerFromMiddleware(fmt.Sprintf("security rule %d", i), metadata))
		}
	}
	profileLayers := append([]docLayer(nil), layers...)
	if handler.Contract != nil {
		profileLayers = append(profileLayers, layerFromContract("contract", *handler.Contract))
	}
	if handler.Doc != nil {
		profileLayers = append(profileLayers, layerFromDocConfig("endpoint", *handler.Doc))
	}
	warnDuplicateProfiles(profileLayers, handler, route, collector)
	for _, layer := range layers {
		if err := acc.applyLayer(config, layer, true); err != nil {
			resolutionErrors = append(resolutionErrors, err)
		}
	}

	acc.applyTombstones(collector, route, handler)

	if handler.Contract != nil {
		for _, issue := range handler.Contract.validationIssues {
			collector.route("contract.option.incoherent", DiagnosticError, route, handler, "contract", issue)
		}
		layer := layerFromContract("contract", *handler.Contract)
		if err := acc.applyLayer(config, layer, false); err != nil {
			resolutionErrors = append(resolutionErrors, err)
		}
	}
	if handler.Doc != nil {
		layer := layerFromDocConfig("endpoint", *handler.Doc)
		if err := acc.applyLayer(config, layer, false); err != nil {
			resolutionErrors = append(resolutionErrors, err)
		}
	}

	summary := handler.Definition
	description := ""
	tags := []string{route.Group}
	operationID := ""
	var section []string
	deprecated := config.Defaults.Deprecated ||
		route.DocumentationDefaults.Deprecated ||
		(handler.Doc != nil && handler.Doc.Deprecated)
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
		if len(handler.Doc.Section) > 0 {
			section = append([]string(nil), handler.Doc.Section...)
		}
		operationID = handler.Doc.OperationID
	}

	return &resolvedDocumentation{
		Enabled:             true,
		Deprecated:          deprecated,
		Summary:             summary,
		Description:         description,
		Tags:                tags,
		Section:             section,
		OperationID:         operationID,
		Parameters:          cloneDocParams(acc.parameters),
		RequestBody:         cloneDocBody(acc.requestBody),
		Responses:           cloneDocResponses(acc.responses),
		Security:            cloneSecurityRequirements(acc.security),
		RequestContentType:  acc.requestContentType,
		ResponseContentType: acc.responseContentType,
		Profiles:            append([]string(nil), acc.profiles...),
	}, errors.Join(resolutionErrors...)
}

func warnDuplicateProfiles(layers []docLayer, handler Handler, route Route, collector *diagnosticCollector) {
	seen := map[string]bool{}
	for _, layer := range layers {
		for _, name := range layer.profiles {
			if seen[name] {
				collector.route("profile.duplicate", DiagnosticWarning, route, handler, layer.name, fmt.Sprintf("profile %q is applied more than once", name))
			}
			seen[name] = true
		}
	}
}

func securityRuleMetadata(rule RouteSecurityRule, route Route, handler Handler, collector *diagnosticCollector) (metadata MiddlewareMetadata, matched bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			collector.route("security.rule.panic", DiagnosticError, route, handler, "securityRules", fmt.Sprintf("security rule predicate panicked: %v", recovered))
			metadata, matched = MiddlewareMetadata{}, false
		}
	}()
	return rule.metadata(route, handler)
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
		seenProfiles:            map[string]bool{},
		parameterIndex:          map[paramKey]int{},
		responseIndex:           map[int]int{},
		profileTombstones:       profileTombstones,
		responseTombstones:      responseTombstones,
		parameterTombstones:     parameterTombstones,
		profileParameters:       map[paramKey]DocParam{},
		profileSources:          map[paramKey]string{},
		profileTombstoneTargets: map[string]bool{},
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
	var layerErrors []error
	if layer.requestContentType != "" {
		acc.requestContentType = layer.requestContentType
	}
	if layer.responseContentType != "" {
		acc.responseContentType = layer.responseContentType
	}

	if inherited {
		for _, name := range layer.profiles {
			if acc.profileTombstones[name] {
				acc.profileTombstoneTargets[name] = true
			}
		}
	}
	expanded, err := expandLayerProfiles(config, layer, inherited, acc.profileTombstones)
	if err != nil {
		layerErrors = append(layerErrors, err)
	}
	for _, profileName := range expanded.profileNames {
		if acc.seenProfiles[profileName] {
			continue
		}
		acc.seenProfiles[profileName] = true
		acc.profiles = append(acc.profiles, profileName)
	}
	for index, parameter := range expanded.parameters {
		key := docParamKey(parameter)
		if existing, exists := acc.profileParameters[key]; exists && !docParamsEqual(existing, parameter) {
			layerErrors = append(layerErrors, fmt.Errorf("profiles %q and %q have conflicting parameter %q in %s", acc.profileSources[key], expanded.parameterProfiles[index], parameter.Name, parameter.In))
			continue
		}
		acc.profileParameters[key] = cloneDocParam(parameter)
		acc.profileSources[key] = expanded.parameterProfiles[index]
		if err := acc.applyParams(layer.name+" profile "+expanded.parameterProfiles[index], []DocParam{parameter}); err != nil {
			layerErrors = append(layerErrors, err)
		}
	}
	if err := acc.applyParams(layer.name, layer.parameters); err != nil {
		layerErrors = append(layerErrors, err)
	}
	if err := acc.applyResponses(layer.name, layer.responses); err != nil {
		layerErrors = append(layerErrors, err)
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
	return errors.Join(layerErrors...)
}

type expandedProfiles struct {
	profileNames      []string
	parameters        []DocParam
	parameterProfiles []string
	security          []OpenAPISecurityRequirement
}

func expandLayerProfiles(config OpenAPIConfig, layer docLayer, inherited bool, tombstones map[string]bool) (expandedProfiles, error) {
	var expanded expandedProfiles
	var profileErrors []error
	for _, name := range layer.profiles {
		if inherited && tombstones[name] {
			continue
		}
		profile, ok := config.Profiles[name]
		if !ok {
			profileErrors = append(profileErrors, fmt.Errorf("%s references profile %q not found in OpenAPIConfig.Profiles", layer.name, name))
			continue
		}
		expanded.profileNames = append(expanded.profileNames, name)
		expanded.parameters = append(expanded.parameters, cloneDocParams(profile.Headers)...)
		for range profile.Headers {
			expanded.parameterProfiles = append(expanded.parameterProfiles, name)
		}
		expanded.parameters = append(expanded.parameters, cloneDocParams(profile.QueryParams)...)
		for range profile.QueryParams {
			expanded.parameterProfiles = append(expanded.parameterProfiles, name)
		}
		expanded.parameters = append(expanded.parameters, cloneDocParams(profile.PathParams)...)
		for range profile.PathParams {
			expanded.parameterProfiles = append(expanded.parameterProfiles, name)
		}
		for _, securityName := range profile.Security {
			expanded.security = append(expanded.security, OpenAPISecurityRequirement{securityName: {}})
		}
	}
	return expanded, errors.Join(profileErrors...)
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
			_ = existing
			return fmt.Errorf("%s has duplicate response status %d", layerName, response.Status)
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

func (acc *documentationAccumulator) applyTombstones(collector *diagnosticCollector, route Route, handler Handler) {
	if len(acc.profileTombstones) > 0 {
		for name := range acc.profileTombstones {
			if !acc.profileTombstoneTargets[name] {
				collector.route("tombstone.no_target", DiagnosticWarning, route, handler, "profiles", fmt.Sprintf("profile tombstone %q has no inherited target", name))
			}
		}
		profiles := acc.profiles[:0]
		for _, name := range acc.profiles {
			if !acc.profileTombstones[name] {
				profiles = append(profiles, name)
			}
		}
		acc.profiles = profiles
	}
	if len(acc.parameterTombstones) > 0 {
		for key := range acc.parameterTombstones {
			if _, exists := acc.parameterIndex[key]; !exists {
				collector.route("tombstone.no_target", DiagnosticWarning, route, handler, "parameters", fmt.Sprintf("parameter tombstone %q in %s has no inherited target", key.name, key.in))
			}
		}
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
		for status := range acc.responseTombstones {
			if _, exists := acc.responseIndex[status]; !exists {
				collector.route("tombstone.no_target", DiagnosticWarning, route, handler, "responses", fmt.Sprintf("response tombstone %d has no inherited target", status))
			}
		}
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
