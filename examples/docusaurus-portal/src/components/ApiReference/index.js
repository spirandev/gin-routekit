import React, { useEffect, useRef } from 'react';
import '@stoplight/elements/styles.min.css';

// Client-side OpenAPI renderer (Stoplight Elements), self-hosted via the
// Docusaurus bundle - no CDN. It consumes the runtime /openapi.json served
// by the Go binary, so the contract always matches the running process.
//
// router="hash" avoids conflicts between Elements' client-side routing and
// Docusaurus routing.
export default function ApiReference() {
  const ref = useRef(null);

  useEffect(() => {
    let cancelled = false;
    import('@stoplight/elements/web-components.min.js').then(() => {
      if (cancelled) return;
      const el = ref.current;
      if (!el) return;
      el.apiDescriptionUrl = '/openapi.json';
      el.router = 'hash';
      el.layout = 'sidebar';
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return <elements-api ref={ref} style={{ display: 'block', height: '100%' }} />;
}
