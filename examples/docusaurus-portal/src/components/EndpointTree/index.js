import React, { useEffect, useState } from 'react';

// Runtime endpoint map derived from the served /openapi.json.
//
// The logical tree comes from `x-routekit-docs.sections` (set per route with
// .Section(...) in Go) and the "Deprecated vs Current" split comes from
// `operation.deprecated` - the contract remains the single source of truth
// (see ADR 0002).
const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete', 'head', 'options', 'trace'];

function buildTree(document) {
  const sections = document?.['x-routekit-docs']?.sections ?? {};
  const tree = new Map();

  for (const [path, item] of Object.entries(document?.paths ?? {})) {
    for (const [method, operation] of Object.entries(item)) {
      if (!HTTP_METHODS.includes(method)) continue;
      const sectionPath = sections[operation.operationId] ?? ['Other'];
      const deprecated = operation.deprecated === true;

      let node = tree;
      for (const segment of sectionPath) {
        if (!node.has(segment)) node.set(segment, new Map());
        node = node.get(segment);
      }
      if (!node.has(deprecated)) node.set(deprecated, []);
      node.get(deprecated).push({ method, path, summary: operation.summary ?? '' });
    }
  }
  return tree;
}

function EndpointBranch({ branch }) {
  const current = branch.get(false) ?? [];
  const deprecated = branch.get(true) ?? [];

  return (
    <ul>
      {current.length > 0 && (
        <li>
          <strong>Current</strong>
          <EndpointList endpoints={current} />
        </li>
      )}
      {deprecated.length > 0 && (
        <li>
          <strong>Deprecated</strong>
          <EndpointList endpoints={deprecated} />
        </li>
      )}
    </ul>
  );
}

function EndpointList({ endpoints }) {
  return (
    <ul>
      {endpoints.map((endpoint) => (
        <li key={`${endpoint.method} ${endpoint.path}`}>
          <code>
            {endpoint.method.toUpperCase()} {endpoint.path}
          </code>
          {endpoint.summary ? ` — ${endpoint.summary}` : ''}
        </li>
      ))}
    </ul>
  );
}

export default function EndpointTree() {
  const [tree, setTree] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    fetch('/openapi.json')
      .then((response) => {
        if (!response.ok) throw new Error(`openapi.json returned ${response.status}`);
        return response.json();
      })
      .then((document) => setTree(buildTree(document)))
      .catch((cause) => setError(String(cause)));
  }, []);

  if (error) return <p>Failed to load the OpenAPI document: {error}</p>;
  if (!tree) return <p>Loading /openapi.json…</p>;
  if (tree.size === 0) return <p>No documented endpoints found.</p>;

  return (
    <ul>
      {[...tree.entries()].map(([resource, versions]) => (
        <li key={resource}>
          <strong>{resource}</strong>
          <ul>
            {[...versions.entries()].map(([version, branch]) => (
              <li key={version}>
                {version}
                <EndpointBranch branch={branch} />
              </li>
            ))}
          </ul>
        </li>
      ))}
    </ul>
  );
}
