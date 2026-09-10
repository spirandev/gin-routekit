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
    ...(process.env.OPENAPI_DOCS_PLUGIN === '1' ? ['docusaurus-theme-openapi-docs'] : []),
  ],

  // Optional build-time pipeline (Option A): with OPENAPI_DOCS_PLUGIN=1 the
  // docusaurus-plugin-openapi-docs generates versioned MDX from the exported
  // openapi.json (run `go run ./export` first). Without it, the site stays on
  // the default runtime flow (Option B) - see README.
  plugins: [
    ...(process.env.OPENAPI_DOCS_PLUGIN === '1'
      ? [
          [
            'docusaurus-plugin-openapi-docs',
            {
              id: 'api',
              docsPluginId: 'default',
              config: {
                main: {
                  specPath: 'openapi.json',
                  outputDir: 'docs/api',
                  sidebarOptions: { groupPathsBy: 'tag' },
                  showSchemas: true,
                },
              },
            },
          ],
        ]
      : []),
  ],

  themeConfig: {
    colorMode: {
      defaultMode: 'dark',
      respectPrefersColorScheme: true,
    },
    // The build-time pipeline (Option A) docs are reachable through the
    // sidebar ("main" category); no navbar entry is needed for them.
    navbar: {
      title: 'Instances API',
      items: [
        { to: '/', label: 'Docs', position: 'left' },
        { to: '/api-reference', label: 'API Reference', position: 'left' },
        { to: '/changelog', label: 'Changelog', position: 'right' },
      ],
    },
  },
};

module.exports = config;
