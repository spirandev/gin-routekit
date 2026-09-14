package routekit

import (
	"bytes"
	"errors"
	"html/template"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	defaultStoplightUIPath     = "/docs"
	defaultStoplightOpenAPIURL = "/openapi.json"
	defaultStoplightTitle      = "API Docs"
	defaultStoplightCDN        = "https://cdn.jsdelivr.net/npm/@stoplight/elements@9.0.24"
)

type StoplightUIConfig struct {
	Path       string
	OpenAPIURL string
	Title      string
	CDNBaseURL string

	HideExport     bool
	HideSchemas    bool
	HideTryIt      bool
	HideTryItPanel bool
	Layout         StoplightLayout
	Router         StoplightRouter
	Logo           string
}

type StoplightLayout string

const (
	StoplightLayoutSidebar    StoplightLayout = "sidebar"
	StoplightLayoutResponsive StoplightLayout = "responsive"
	StoplightLayoutStacked    StoplightLayout = "stacked"
)

type StoplightRouter string

const (
	StoplightRouterHash    StoplightRouter = "hash"
	StoplightRouterHistory StoplightRouter = "history"
	StoplightRouterMemory  StoplightRouter = "memory"
	StoplightRouterStatic  StoplightRouter = "static"
)

type stoplightUITemplateData struct {
	Title          string
	CDNBaseURL     string
	OpenAPIURL     string
	Logo           string
	Layout         StoplightLayout
	Router         StoplightRouter
	BasePath       string
	HideExport     bool
	HideSchemas    bool
	HideTryIt      bool
	HideTryItPanel bool
}

func (ar *AppRouter) RegisterStoplightUI(engine *gin.Engine, config StoplightUIConfig) error {
	if engine == nil {
		return errors.New("gin engine is required")
	}

	config = normalizeStoplightUIConfig(config)
	if err := validateStoplightUIConfig(config); err != nil {
		return err
	}

	html, err := renderStoplightUIHTML(config)
	if err != nil {
		return err
	}

	return registerDocsPage(engine, config.Path, html)
}

func normalizeStoplightUIConfig(config StoplightUIConfig) StoplightUIConfig {
	config.Path = strings.TrimSpace(config.Path)
	config.OpenAPIURL = strings.TrimSpace(config.OpenAPIURL)
	config.Title = strings.TrimSpace(config.Title)
	config.Logo = strings.TrimSpace(config.Logo)
	config.CDNBaseURL = strings.TrimRight(strings.TrimSpace(config.CDNBaseURL), "/")
	config.Layout = StoplightLayout(strings.TrimSpace(string(config.Layout)))
	config.Router = StoplightRouter(strings.TrimSpace(string(config.Router)))

	if config.Path == "" {
		config.Path = defaultStoplightUIPath
	}
	if config.OpenAPIURL == "" {
		config.OpenAPIURL = defaultStoplightOpenAPIURL
	}
	if config.Title == "" {
		config.Title = defaultStoplightTitle
	}
	if config.CDNBaseURL == "" {
		config.CDNBaseURL = defaultStoplightCDN
	}
	if config.Layout == "" {
		config.Layout = StoplightLayoutSidebar
	}
	if config.Router == "" {
		config.Router = StoplightRouterHash
	}
	return config
}

func validateStoplightUIConfig(config StoplightUIConfig) error {
	if err := validateUIPath("stoplight UI", config.Path); err != nil {
		return err
	}
	if !isAbsolutePathOrURL(config.OpenAPIURL) {
		return errors.New("stoplight UI OpenAPIURL must be an absolute path or URL")
	}
	if !strings.HasPrefix(config.CDNBaseURL, "http://") && !strings.HasPrefix(config.CDNBaseURL, "https://") {
		return errors.New("stoplight UI CDNBaseURL must start with http:// or https://")
	}
	switch config.Layout {
	case StoplightLayoutSidebar, StoplightLayoutResponsive, StoplightLayoutStacked:
	default:
		return errors.New("stoplight UI Layout must be one of sidebar, responsive, stacked")
	}
	switch config.Router {
	case StoplightRouterHash, StoplightRouterHistory, StoplightRouterMemory, StoplightRouterStatic:
	default:
		return errors.New("stoplight UI Router must be one of hash, history, memory, static")
	}
	return nil
}

func renderStoplightUIHTML(config StoplightUIConfig) (string, error) {
	tmpl, err := template.New("stoplight-ui").Parse(stoplightUITemplate)
	if err != nil {
		return "", err
	}

	data := stoplightUITemplateData{
		Title:          config.Title,
		CDNBaseURL:     config.CDNBaseURL,
		OpenAPIURL:     config.OpenAPIURL,
		Logo:           config.Logo,
		Layout:         config.Layout,
		Router:         config.Router,
		HideExport:     config.HideExport,
		HideSchemas:    config.HideSchemas,
		HideTryIt:      config.HideTryIt,
		HideTryItPanel: config.HideTryItPanel,
	}
	if config.Router == StoplightRouterHistory {
		data.BasePath = config.Path
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const stoplightUITemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{ .Title }}</title>
  <link rel="stylesheet" href="{{ .CDNBaseURL }}/styles.min.css" />
  <style>
    html, body { height: 100%; margin: 0; }
  </style>
</head>
<body>
  <elements-api
    id="stoplight-docs"
    apiDescriptionUrl="{{ .OpenAPIURL }}"
    layout="{{ .Layout }}"
    router="{{ .Router }}"
    {{- if .Logo }}
    logo="{{ .Logo }}"
    {{- end }}
    {{- if .BasePath }}
    basePath="{{ .BasePath }}"
    {{- end }}
  ></elements-api>
  <script src="{{ .CDNBaseURL }}/web-components.min.js"></script>
  <script>
    window.addEventListener('load', function() {
      var docs = document.getElementById('stoplight-docs');
      {{- if .HideTryIt }}
      docs.hideTryIt = true;
      {{- end }}
      {{- if .HideTryItPanel }}
      docs.hideTryItPanel = true;
      {{- end }}
      {{- if .HideExport }}
      docs.hideExport = true;
      {{- end }}
      {{- if .HideSchemas }}
      docs.hideSchemas = true;
      {{- end }}
    });
  </script>
</body>
</html>`
