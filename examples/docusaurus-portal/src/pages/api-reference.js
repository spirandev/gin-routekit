import React from 'react';
import Layout from '@theme/Layout';
import ApiReference from '@site/src/components/ApiReference';

export default function ApiReferencePage() {
  return (
    <Layout title="API Reference" description="Instances API reference rendered at runtime">
      <main
        style={{
          height: 'calc(100vh - var(--ifm-navbar-height))',
          overflow: 'hidden',
        }}
      >
        <ApiReference />
      </main>
    </Layout>
  );
}
