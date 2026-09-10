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
	defaultSwaggerUIPath = "/docs"
	defaultOpenAPIURL    = "/openapi.json"
	defaultSwaggerTitle  = "API Docs"
	defaultSwaggerCDN    = "https://unpkg.com/swagger-ui-dist"
)

type SwaggerUIConfig struct {
	Path               string
	OpenAPIURL         string
	OpenAPIURLs        []SwaggerUISpec
	PrimaryOpenAPIName string
	Title              string
	CDNBaseURL         string
}

type SwaggerUISpec struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type swaggerUITemplateData struct {
	Title                  string
	CDNBaseURL             string
	OpenAPIURLJSON         template.JS
	OpenAPIURLsJSON        template.JS
	PrimaryOpenAPINameJSON template.JS
	MultipleSpecs          bool
}

func (ar *AppRouter) RegisterSwaggerUI(engine *gin.Engine, config SwaggerUIConfig) error {
	if engine == nil {
		return errors.New("gin engine is required")
	}

	config = normalizeSwaggerUIConfig(config)
	if err := validateSwaggerUIConfig(config); err != nil {
		return err
	}

	html, err := renderSwaggerUIHTML(config)
	if err != nil {
		return err
	}

	return registerDocsPage(engine, config.Path, html)
}

func normalizeSwaggerUIConfig(config SwaggerUIConfig) SwaggerUIConfig {
	config.Path = strings.TrimSpace(config.Path)
	config.OpenAPIURL = strings.TrimSpace(config.OpenAPIURL)
	config.PrimaryOpenAPIName = strings.TrimSpace(config.PrimaryOpenAPIName)
	config.Title = strings.TrimSpace(config.Title)
	config.CDNBaseURL = strings.TrimRight(strings.TrimSpace(config.CDNBaseURL), "/")

	if len(config.OpenAPIURLs) > 0 {
		openAPIURLs := make([]SwaggerUISpec, len(config.OpenAPIURLs))
		for i, spec := range config.OpenAPIURLs {
			openAPIURLs[i] = SwaggerUISpec{
				Name: strings.TrimSpace(spec.Name),
				URL:  strings.TrimSpace(spec.URL),
			}
		}
		config.OpenAPIURLs = openAPIURLs
	}

	if config.Path == "" {
		config.Path = defaultSwaggerUIPath
	}
	if len(config.OpenAPIURLs) == 0 && config.OpenAPIURL == "" {
		config.OpenAPIURL = defaultOpenAPIURL
	}
	if len(config.OpenAPIURLs) > 0 && config.PrimaryOpenAPIName == "" {
		config.PrimaryOpenAPIName = config.OpenAPIURLs[0].Name
	}
	if config.Title == "" {
		config.Title = defaultSwaggerTitle
	}
	if config.CDNBaseURL == "" {
		config.CDNBaseURL = defaultSwaggerCDN
	}
	return config
}

func validateSwaggerUIConfig(config SwaggerUIConfig) error {
	if err := validateUIPath("swagger UI", config.Path); err != nil {
		return err
	}
	if len(config.OpenAPIURLs) == 0 && !isAbsolutePathOrURL(config.OpenAPIURL) {
		return errors.New("swagger UI OpenAPIURL must be an absolute path or URL")
	}
	if len(config.OpenAPIURLs) > 0 {
		seenNames := make(map[string]struct{}, len(config.OpenAPIURLs))
		for _, spec := range config.OpenAPIURLs {
			if spec.Name == "" {
				return errors.New("swagger UI OpenAPIURLs name is required")
			}
			if _, ok := seenNames[spec.Name]; ok {
				return errors.New("swagger UI OpenAPIURLs name must be unique")
			}
			seenNames[spec.Name] = struct{}{}

			if !isAbsolutePathOrURL(spec.URL) {
				return errors.New("swagger UI OpenAPIURLs URL must be an absolute path or URL")
			}
		}
		if _, ok := seenNames[config.PrimaryOpenAPIName]; !ok {
			return errors.New("swagger UI PrimaryOpenAPIName must match an OpenAPIURLs name")
		}
	}
	if !strings.HasPrefix(config.CDNBaseURL, "http://") && !strings.HasPrefix(config.CDNBaseURL, "https://") {
		return errors.New("swagger UI CDNBaseURL must start with http:// or https://")
	}
	return nil
}

func renderSwaggerUIHTML(config SwaggerUIConfig) (string, error) {
	tmpl, err := template.New("swagger-ui").Parse(swaggerUITemplate)
	if err != nil {
		return "", err
	}

	data := swaggerUITemplateData{
		Title:      config.Title,
		CDNBaseURL: config.CDNBaseURL,
	}

	if len(config.OpenAPIURLs) > 0 {
		openAPIURLs, err := json.Marshal(config.OpenAPIURLs)
		if err != nil {
			return "", err
		}
		primaryOpenAPIName, err := json.Marshal(config.PrimaryOpenAPIName)
		if err != nil {
			return "", err
		}
		data.OpenAPIURLsJSON = template.JS(openAPIURLs)
		data.PrimaryOpenAPINameJSON = template.JS(primaryOpenAPIName)
		data.MultipleSpecs = true
	} else {
		openAPIURL, err := json.Marshal(config.OpenAPIURL)
		if err != nil {
			return "", err
		}
		data.OpenAPIURLJSON = template.JS(openAPIURL)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const swaggerUITemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>{{ .Title }}</title>
  <link rel="stylesheet" href="{{ .CDNBaseURL }}/swagger-ui.css" />
  <style>
    html, body { margin: 0; padding: 0; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="{{ .CDNBaseURL }}/swagger-ui-bundle.js"></script>
  <script src="{{ .CDNBaseURL }}/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = function() {
      window.ui = SwaggerUIBundle({
{{- if .MultipleSpecs }}
        urls: {{ .OpenAPIURLsJSON }},
        "urls.primaryName": {{ .PrimaryOpenAPINameJSON }},
{{- else }}
        url: {{ .OpenAPIURLJSON }},
{{- end }}
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        plugins: [
          SwaggerUIBundle.plugins.DownloadUrl
        ],
        layout: 'StandaloneLayout'
      });
    };
  </script>
</body>
</html>`
