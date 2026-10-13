import { defineConfig } from 'vitepress'

export default defineConfig({
  base: '/stacklint/',
  lang: 'en-US',
  title: "stacklint",
  description: 'Static analysis, environment template generation, and image checks for Docker Compose stack repositories',

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Shell Completion', link: '/guide/completion' },
      { text: 'GitHub', link: 'https://github.com/YewFence/stacklint' }
    ],

    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Getting Started', link: '/guide/getting-started' },
          { text: 'Env Markers', link: '/guide/env-markers' },
          { text: 'Shell Completion', link: '/guide/completion' }
        ]
      },
      {
        text: 'Design',
        items: [
          { text: 'Requirements', link: '/design/requirements' }
        ]
      }
    ],

    search: {
      provider: 'local'
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/YewFence/stacklint' }
    ],

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Copyright © YewFence'
    },

    docFooter: {
      prev: 'Previous page',
      next: 'Next page'
    },

    outline: {
      label: 'On this page'
    },

    lastUpdated: {
      text: 'Last updated'
    }
  }
})
