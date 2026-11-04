import { defineConfig } from 'astro/config';
import { fileURLToPath } from 'node:url';
import mdx from '@astrojs/mdx';
import starlight from '@astrojs/starlight';
import { unified } from '@astrojs/markdown-remark';

import {
  polipagePreset,
  enforcePageShape,
  canonicalSlugs,
} from './src/preset/index.js';

// The preset lives at docs/src/preset/. It's vendored from the
// sdk-docs-preset reference repo; see docs/src/preset/README.md.
// The `@preset` alias is what every MDX file uses to import shared
// components (e.g. `@preset/components/ApiKeyCallout.astro`) and the
// shared CSS path (`@preset/styles/poli-page.css`).
const presetRoot = fileURLToPath(new URL('./src/preset', import.meta.url));

// The page-set lint, with the integration profile. It goes in both places:
// `markdown.processor` lints `.md` pages, `mdx()` lints `.mdx` pages.
const lint = [enforcePageShape, [canonicalSlugs, { profile: 'integration' }]];

// Run `astro build` from this folder (`npm run build` in docs/): the preset
// and the lint read the pages from the working directory.
export default defineConfig({
  site: 'https://poli-page.github.io',
  base: '/gin',
  vite: {
    resolve: {
      alias: { '@preset': presetRoot },
    },
  },
  markdown: {
    processor: unified({ remarkPlugins: lint }),
  },
  integrations: [
    starlight(
      polipagePreset({
        profile: 'integration',
        framework: 'Gin',
        repo: 'poli-page/gin',
      }),
    ),
    // After starlight(), so Starlight uses this mdx() instead of its own.
    mdx({ optimize: true, remarkPlugins: lint }),
  ],
});
