---
layout: home

hero:
  name: "stacklint"
  text: "Static analysis, environment template generation, and image checks for Docker Compose stack repositories"
  tagline: 'A personal, opinionated linter: config-driven mechanics, private tastes. Fork-friendly, PR-free.'
  actions:
    - theme: brand
      text: Get Started
      link: /guide/getting-started
    - theme: alt
      text: GitHub Repo
      link: https://github.com/YewFence/stacklint
    - theme: alt
      text: Shell Completion
      link: /guide/completion

features:
  - title: stacklint check
    details: House rules for Compose repos — digest pinning, port uniqueness, module wiring — with recorded, expiring approvals.
  - title: stacklint env
    details: Generates .env.example templates from real compose-go interpolation analysis and fails CI when they drift.
  - title: stacklint images
    details: Verifies digests against remote registries, incrementally against a git base ref; lists the image inventory as JSON.
  - title: Shell Completion
    details: Generate completion scripts for Bash, Zsh, Fish, and PowerShell.
---
