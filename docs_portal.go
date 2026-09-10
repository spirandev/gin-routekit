package routekit

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	defaultDocsPortalPath  = "/docs"
	defaultDocsPortalIndex = "index.html"
	assetsCacheControl     = "public, max-age=31536000, immutable"
	docsCacheControl       = "no-store"
)

type DocsPortalConfig struct {
	Path         string
	Assets       fs.FS
	AssetsPrefix string
	Index        string
}

func (ar *AppRouter) RegisterDocsPortal(engine *gin.Engine, config DocsPortalConfig) error {
	if engine == nil {
		return errors.New("gin engine is required")
	}

	config = normalizeDocsPortalConfig(config)
	if err := validateDocsPortalConfig(config); err != nil {
		return err
	}

	files := config.Assets
	if config.AssetsPrefix != "" {
		sub, err := fs.Sub(config.Assets, config.AssetsPrefix)
		if err != nil {
			return err
		}
		files = sub
	}

	if _, err := fs.Stat(files, config.Index); err != nil {
		return fmt.Errorf("docs portal Index %q was not found in Assets", config.Index)
	}

	if err := checkDocsPortalConflicts(engine, config.Path); err != nil {
		return err
	}

	handler := serveDocsPortal(files, config.Index)
	if config.Path == "/" {
		engine.GET("/*filepath", handler)
		return nil
	}
	engine.GET(config.Path, handler)
	engine.GET(config.Path+"/*filepath", handler)
	return nil
}

func normalizeDocsPortalConfig(config DocsPortalConfig) DocsPortalConfig {
	config.Path = strings.TrimSpace(config.Path)
	config.AssetsPrefix = strings.TrimSpace(config.AssetsPrefix)
	config.Index = strings.TrimSpace(config.Index)

	if config.Path == "" {
		config.Path = defaultDocsPortalPath
	}
	if config.Index == "" {
		config.Index = defaultDocsPortalIndex
	}
	return config
}

func validateDocsPortalConfig(config DocsPortalConfig) error {
	if err := validateUIPath("docs portal", config.Path); err != nil {
		return err
	}
	if config.Assets == nil {
		return errors.New("docs portal Assets is required")
	}
	if config.AssetsPrefix != "" {
		if path.IsAbs(config.AssetsPrefix) || strings.Contains(config.AssetsPrefix, "..") {
			return errors.New("docs portal AssetsPrefix must be a relative path")
		}
	}
	return nil
}

func checkDocsPortalConflicts(engine *gin.Engine, docsPath string) error {
	routes := engine.Routes()
	if len(routes) == 0 {
		return nil
	}
	if docsPath == "/" {
		return fmt.Errorf("docs portal path %q conflicts with registered route %q", docsPath, routes[0].Path)
	}
	prefix := strings.TrimSuffix(docsPath, "/") + "/"
	for _, route := range routes {
		if route.Path == docsPath || strings.HasPrefix(route.Path, prefix) {
			return fmt.Errorf("docs portal path %q conflicts with registered route %q", docsPath, route.Path)
		}
	}
	return nil
}

func serveDocsPortal(files fs.FS, index string) gin.HandlerFunc {
	return func(c *gin.Context) {
		target := strings.TrimPrefix(c.Param("filepath"), "/")
		if target == "" {
			c.Header("Cache-Control", docsCacheControl)
			serveDocsFile(c, files, index)
			return
		}

		cleaned := path.Clean(target)
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			c.Header("Cache-Control", docsCacheControl)
			c.Status(http.StatusNotFound)
			return
		}

		if info, err := fs.Stat(files, cleaned); err == nil && !info.IsDir() {
			if strings.HasPrefix(cleaned, "assets/") {
				c.Header("Cache-Control", assetsCacheControl)
			} else {
				c.Header("Cache-Control", docsCacheControl)
			}
			serveDocsFile(c, files, cleaned)
			return
		}

		c.Header("Cache-Control", docsCacheControl)
		if segmentHasExtension(cleaned) {
			c.Status(http.StatusNotFound)
			return
		}
		// Directories resolve to their own index page (static hosting
		// convention; Docusaurus emits <page>/index.html) before the SPA
		// fallback to the root index.
		if indexTarget := cleaned + "/" + index; indexTarget != index {
			if info, err := fs.Stat(files, indexTarget); err == nil && !info.IsDir() {
				serveDocsFile(c, files, indexTarget)
				return
			}
		}
		serveDocsFile(c, files, index)
	}
}

func serveDocsFile(c *gin.Context, files fs.FS, name string) {
	file, err := files.Open(name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}
	if seeker, ok := file.(io.ReadSeeker); ok {
		http.ServeContent(c.Writer, c.Request, name, info.ModTime(), seeker)
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, mime.TypeByExtension(path.Ext(name)), data)
}

func segmentHasExtension(name string) bool {
	segment := name
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		segment = name[slash+1:]
	}
	return strings.Contains(segment, ".")
}
