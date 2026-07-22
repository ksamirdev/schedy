import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docs: [
    'intro',
    {
      type: 'category',
      label: 'Getting Started',
      collapsed: false,
      items: ['installation', 'configuration', 'authentication'],
    },
    {
      type: 'category',
      label: 'Concepts',
      collapsed: false,
      items: [
        'concepts/status',
        'concepts/retries',
        'concepts/delivery',
        'concepts/idempotency',
      ],
    },
    {
      type: 'category',
      label: 'API Reference',
      collapsed: false,
      items: [
        'api/create',
        'api/list',
        'api/get',
        'api/cancel',
        'api/bulk-delete',
      ],
    },
  ],
};

export default sidebars;
