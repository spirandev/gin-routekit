// @ts-check

// IMPORTANT: baseUrl MUST match the Path used in RegisterDocsPortal (Go side).
// Docusaurus keeps the trailing slash; the Go Path does not have one.
// See examples/docusaurus-portal/README.md, pitfall #1.
const config = {
  title: 'Instances API',
  tagline: 'Self-hosted documentation portal powered by gin-routekit',
  url: 'http://localhost:8080',
  baseUrl: '/docs/',

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  // The Go binary embeds the build output with go:embed (go/embed.go).
  // There is no outDir config field in Docusaurus: the output directory is
  // set via `docusaurus build --out-dir go/portal-build` (see package.json).
  // The committed go/portal-build/index.html is a placeholder so that
  // `go build` works without Node; `npm run build` overwrites it.

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          // baseUrl already places the site under /docs/, so docs live at the
          // site root: /docs/intro, /docs/changelog, /docs/guides/...
          routeBasePath: '/',
          sidebarPath: require.resolve('./sidebars.js'),
        },
        blog: false,
      },
    ],
  ],

  themes: [
    [
      require.resolve('@easyops-cn/docusaurus-search-local'),
      {
        hashed: true,
        indexDocs: true,
        indexBlog: false,
        indexPages: false,
      },
    ],
  ],

  themeConfig: {
    colorMode: {
      defaultMode: 'dark',
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Instances API',
      items: [
        { to: '/', label: 'Docs', position: 'left' },
        { to: '/api-reference', label: 'API Reference', position: 'left' },
        { to: '/changelog', label: 'Changelog', position: 'right' },
      ],
    },
    // Optional build-time OpenAPI docs (Option A, phase 6 pipeline):
    // set DOCUSAURUS_OPENAPI_DOCS=1 and provide docusaurus-plugin-openapi-docs.
    // The runtime API Reference page consumes /openapi.json directly (Option B,
    // the default) and does not depend on this.
  },

  customFields: {
    buildTimeOpenAPIDocs: process.env.DOCUSAURUS_OPENAPI_DOCS === '1',
  },
};

module.exports = config;
