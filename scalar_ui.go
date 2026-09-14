package routekit

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	defaultScalarUIPath     = "/docs"
	defaultScalarOpenAPIURL = "/openapi.json"
	defaultScalarTitle      = "API Docs"
	// defaultScalarCDN points at a single standalone script (not a base path
	// for multiple assets like the Swagger/Stoplight CDNs).
	defaultScalarCDN = "https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.28.5"
)

// ScalarUIConfig configures the Scalar API Reference documentation page.
//
// Unlike SwaggerUIConfig.CDNBaseURL and StoplightUIConfig.CDNBaseURL, which
// are base paths that multiple assets are resolved against, CDNBaseURL here
// is the full URL of the single standalone script Scalar publishes.
type ScalarUIConfig struct {
	Path       string
	OpenAPIURL string
	Title      string
	CDNBaseURL string

	Theme                 ScalarTheme
	Layout                ScalarLayout
	HideSidebar           bool
	HideDownloadButton    bool
	HideTestRequestButton bool
	HideModels            bool
}

type ScalarTheme string

const (
	ScalarThemeDefault    ScalarTheme = "default"
	ScalarThemeAlternate  ScalarTheme = "alternate"
	ScalarThemeMoon       ScalarTheme = "moon"
	ScalarThemePurple     ScalarTheme = "purple"
	ScalarThemeSolarized  ScalarTheme = "solarized"
	ScalarThemeBluePlanet ScalarTheme = "bluePlanet"
	ScalarThemeSaturn     ScalarTheme = "saturn"
	ScalarThemeKepler     ScalarTheme = "kepler"
	ScalarThemeMars       ScalarTheme = "mars"
	ScalarThemeDeepSpace  ScalarTheme = "deepSpace"
	ScalarThemeNone       ScalarTheme = "none"
)

type ScalarLayout string

const (
	ScalarLayoutModern  ScalarLayout = "modern"
	ScalarLayoutClassic ScalarLayout = "classic"
)

type scalarUIConfiguration struct {
	Theme                 ScalarTheme  `json:"theme"`
	Layout                ScalarLayout `json:"layout"`
	ShowSidebar           bool         `json:"showSidebar"`
	HideDownloadButton    bool         `json:"hideDownloadButton,omitempty"`
	HideTestRequestButton bool         `json:"hideTestRequestButton,omitempty"`
	HideModels            bool         `json:"hideModels,omitempty"`
}

type scalarUITemplateData struct {
	Title             string
	CDNBaseURL        string
	OpenAPIURL        string
	ConfigurationJSON string
}

func (ar *AppRouter) RegisterScalarUI(engine *gin.Engine, config ScalarUIConfig) error {
	if engine == nil {
		return errors.New("gin engine is required")
	}

	config = normalizeScalarUIConfig(config)
	if err := validateScalarUIConfig(config); err != nil {
		return err
	}

	html, err := renderScalarUIHTML(config)
	if err != nil {
		return err
	}

	return registerDocsPage(engine, config.Path, html)
}

func normalizeScalarUIConfig(config ScalarUIConfig) ScalarUIConfig {
	config.Path = strings.TrimSpace(config.Path)
	config.OpenAPIURL = strings.TrimSpace(config.OpenAPIURL)
	config.Title = strings.TrimSpace(config.Title)
	config.CDNBaseURL = strings.TrimRight(strings.TrimSpace(config.CDNBaseURL), "/")
	config.Theme = ScalarTheme(strings.TrimSpace(string(config.Theme)))
	config.Layout = ScalarLayout(strings.TrimSpace(string(config.Layout)))

	if config.Path == "" {
		config.Path = defaultScalarUIPath
	}
	if config.OpenAPIURL == "" {
		config.OpenAPIURL = defaultScalarOpenAPIURL
	}
	if config.Title == "" {
		config.Title = defaultScalarTitle
	}
	if config.CDNBaseURL == "" {
		config.CDNBaseURL = defaultScalarCDN
	}
	if config.Theme == "" {
		config.Theme = ScalarThemeDefault
	}
	if config.Layout == "" {
		config.Layout = ScalarLayoutModern
	}
	return config
}

func validateScalarUIConfig(config ScalarUIConfig) error {
	if err := validateUIPath("scalar UI", config.Path); err != nil {
		return err
	}
	if !isAbsolutePathOrURL(config.OpenAPIURL) {
		return errors.New("scalar UI OpenAPIURL must be an absolute path or URL")
	}
	if !strings.HasPrefix(config.CDNBaseURL, "http://") && !strings.HasPrefix(config.CDNBaseURL, "https://") {
		return errors.New("scalar UI CDNBaseURL must start with http:// or https://")
	}
	switch config.Theme {
	case ScalarThemeDefault, ScalarThemeAlternate, ScalarThemeMoon, ScalarThemePurple,
		ScalarThemeSolarized, ScalarThemeBluePlanet, ScalarThemeSaturn, ScalarThemeKepler,
		ScalarThemeMars, ScalarThemeDeepSpace, ScalarThemeNone:
	default:
		return errors.New("scalar UI Theme must be one of default, alternate, moon, purple, solarized, bluePlanet, saturn, kepler, mars, deepSpace, none")
	}
	switch config.Layout {
	case ScalarLayoutModern, ScalarLayoutClassic:
	default:
		return errors.New("scalar UI Layout must be one of modern, classic")
	}
	return nil
}

func renderScalarUIHTML(config ScalarUIConfig) (string, error) {
	tmpl, err := template.New("scalar-ui").Parse(scalarUITemplate)
	if err != nil {
		return "", err
	}

	configuration := scalarUIConfiguration{
		Theme:                 config.Theme,
		Layout:                config.Layout,
		ShowSidebar:           !config.HideSidebar,
		HideDownloadButton:    config.HideDownloadButton,
		HideTestRequestButton: config.HideTestRequestButton,
		HideModels:            config.HideModels,
	}
	configJSON, err := json.Marshal(configuration)
	if err != nil {
		return "", err
	}

	data := scalarUITemplateData{
		Title:             config.Title,
		CDNBaseURL:        config.CDNBaseURL,
		OpenAPIURL:        config.OpenAPIURL,
		ConfigurationJSON: string(configJSON),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const scalarUITemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{ .Title }}</title>
  <style>
    html, body { height: 100%; margin: 0; }
  </style>
</head>
<body>
  <script
    id="api-reference"
    data-url="{{ .OpenAPIURL }}"
    data-configuration="{{ .ConfigurationJSON }}"
  ></script>
  <script src="{{ .CDNBaseURL }}"></script>
</body>
</html>`
