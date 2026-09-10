// @ts-check

const sidebars = {
  docsSidebar: [
    { type: 'doc', id: 'intro', label: 'Introduction' },
    {
      type: 'category',
      label: 'Guides',
      items: [
        { type: 'doc', id: 'guides/authentication', label: 'Authentication' },
        { type: 'doc', id: 'guides/errors', label: 'Errors' },
        { type: 'doc', id: 'guides/webhooks', label: 'Webhooks' },
      ],
    },
    { type: 'doc', id: 'endpoints', label: 'Endpoint map' },
    { type: 'doc', id: 'changelog', label: 'Changelog' },
    // The API Reference is a client-side page rendering Stoplight Elements
    // from the runtime /openapi.json served by the Go binary.
    { type: 'link', href: '/api-reference', label: 'API Reference' },
  ],
};

module.exports = sidebars;
