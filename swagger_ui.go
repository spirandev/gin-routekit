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
	Path       string
	OpenAPIURL string
	Title      string
	CDNBaseURL string
}

type swaggerUITemplateData struct {
	Title          string
	CDNBaseURL     string
	OpenAPIURLJSON template.JS
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

	engine.GET(config.Path, func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		_, _ = c.Writer.Write([]byte(html))
	})
	return nil
}

func normalizeSwaggerUIConfig(config SwaggerUIConfig) SwaggerUIConfig {
	config.Path = strings.TrimSpace(config.Path)
	config.OpenAPIURL = strings.TrimSpace(config.OpenAPIURL)
	config.Title = strings.TrimSpace(config.Title)
	config.CDNBaseURL = strings.TrimRight(strings.TrimSpace(config.CDNBaseURL), "/")

	if config.Path == "" {
		config.Path = defaultSwaggerUIPath
	}
	if config.OpenAPIURL == "" {
		config.OpenAPIURL = defaultOpenAPIURL
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
	if !strings.HasPrefix(config.Path, "/") {
		return errors.New("swagger UI path must start with /")
	}
	if strings.ContainsAny(config.Path, ":*") {
		return errors.New("swagger UI path must not contain gin path parameters or wildcards")
	}
	if !isAbsolutePathOrURL(config.OpenAPIURL) {
		return errors.New("swagger UI OpenAPIURL must be an absolute path or URL")
	}
	if !strings.HasPrefix(config.CDNBaseURL, "http://") && !strings.HasPrefix(config.CDNBaseURL, "https://") {
		return errors.New("swagger UI CDNBaseURL must start with http:// or https://")
	}
	return nil
}

func isAbsolutePathOrURL(value string) bool {
	return strings.HasPrefix(value, "/") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func renderSwaggerUIHTML(config SwaggerUIConfig) (string, error) {
	openAPIURL, err := json.Marshal(config.OpenAPIURL)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("swagger-ui").Parse(swaggerUITemplate)
	if err != nil {
		return "", err
	}

	data := swaggerUITemplateData{
		Title:          config.Title,
		CDNBaseURL:     config.CDNBaseURL,
		OpenAPIURLJSON: template.JS(openAPIURL),
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
        url: {{ .OpenAPIURLJSON }},
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
