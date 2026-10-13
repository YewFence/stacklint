# Env Markers

stacklint reads everything it needs to know about a Compose variable from ordinary YAML comments next to that variable. These comments, called **env markers**, all share the `@env` family and form a single scan channel — every marker lives next to the expression it describes, in the same file, so there is nothing to keep in sync across files.

```yaml
environment:
  # @env PROMETHEUS_REMOTE_WRITE_URL: Grafana Cloud Metrics remote_write address.
  # @env-example PROMETHEUS_REMOTE_WRITE_URL: https://ci-metrics.example.invalid/api/prom/push
  PROMETHEUS_REMOTE_WRITE_URL: ${PROMETHEUS_REMOTE_WRITE_URL:?required}
```

## The mental model

The four markers sit on two axes — **who reads them** (a human vs. a tool), and **whether the variable gets an assignment slot in the generated template**:

| Marker | Who reads it | Assignment slot? |
| --- | --- | --- |
| `@env` | human (documentation) | yes, unchanged |
| `@env-preamble` | human (group documentation) | yes, on the anchor |
| `@env-source` | tool (removes the slot) | **no** — external provider |
| `@env-example` | tool (holds a value) | yes, unchanged |

`@env` and `@env-preamble` only decorate — they never affect the value, the required-ness, or the default. `@env-source` and `@env-example` carry real semantics: one removes the assignment entry entirely, the other supplies a value that tools consume but deployments never see. Hold onto that distinction and the rest follows.

## `@env` — variable description

```yaml
# @env FORGEJO_USER_UID: UID of the user running inside the container.
FORGEJO_USER_UID: ${FORGEJO_USER_UID:-1000}
```

A human-readable description for a single variable. Optional and repeatable; identical text is deduplicated, distinct descriptions are all kept in source order. It becomes the `##` description line above the variable's entry in `.env.example`. It has no effect on the value, required-ness, or default.

## `@env-preamble` — group lead-in

```yaml
# @env-preamble OPENAI_API_KEY: Optional: AI auto-tagging, summaries, semantic search. Disabled when the whole group is unset.
# @env-preamble OPENAI_API_KEY: Supports OpenAI-compatible endpoints and Ollama; see https://docs.karakeep.app/...
# @env OPENAI_API_KEY: API key for an OpenAI-compatible endpoint.
OPENAI_API_KEY:
# @env OPENAI_BASE_URL: OpenAI-compatible endpoint URL.
OPENAI_BASE_URL:
```

A lead-in paragraph for a **group of adjacent variables**, attached by anchor variable name. It is emitted as an independent comment block above the anchor's entry, before any `@env` description of that anchor. Repeatable; kept in source order, with duplicate text deduplicated. An empty body (`# @env-preamble OPENAI_API_KEY:`) emits a blank comment line, useful for separating paragraphs.

The anchor must have an assignment entry in that composition's template. Declaring a preamble anchored to a variable that does not exist, or to an `@env-source` external-provided input (which has no assignment entry), is a hard error.

## `@env-source` — external provider

```yaml
# @env-source BIND_ADDRESS: mise run ready
ports:
  - "${BIND_ADDRESS:-127.0.0.1}:12345:12345"
```

Declares that this input is provided externally at deploy time — for example by `mise run ready` writing it into the deployment `.env`. The generated template gives it **no assignment entry**; instead it appears only in the **source reference** block at the end of the template:

```dotenv
## BIND_ADDRESS: mise run ready
```

Same-named sources must agree across the file graph. A variable cannot have both a wiring provider and an `@env-source` — stacklint fails rather than guessing which wins. This is the formal way to say "this variable does not belong in your `.env.example`."

## `@env-example` — example value

```yaml
# @env-example PROMETHEUS_REMOTE_WRITE_URL: https://ci-metrics.example.invalid/api/prom/push
PROMETHEUS_REMOTE_WRITE_URL: ${PROMETHEUS_REMOTE_WRITE_URL:?required}
```

A value for **tools to consume**, never a runtime default. It feeds three consumers:

1. **Check rendering** — stacklint interpolates the Compose model to verify it; required (`:?`) variables need a value to render at all.
2. **Template status line** — emitted as `## Required; Example: <value>` above the empty assignment.
3. **`env ci` artifacts** — materialized into `.env.ci` for `docker compose config` and other Compose CLI consumers.

The rules:

- A `:?` variable (must not be empty) **must** declare an example; `env generate` reports the full missing list up front.
- A `?` variable (may be empty) optionally declares one, for shapes where an empty value would break rendering — an empty port, for instance.
- Repeated declarations across one composition's file graph must agree.
- Declaring an example on a variable that already has a default value or a wiring provider fails — it would never be consulted.
- A leftover marker after its expression is deleted is treated as stale and fails.

Example values are **not** runtime defaults. They never enter the deployment `.env`, and their presence does not prove the deployment environment actually provides the variable. The generated template keeps required entries as empty assignments, so copying the template without filling them in still fails at deploy render time via the underlying `:?`.

## What the example produces

Given the markers above, `stacklint env generate` emits an `.env.example` entry like:

```dotenv
## Grafana Cloud Metrics remote_write address.
## Required; Example: https://ci-metrics.example.invalid/api/prom/push
PROMETHEUS_REMOTE_WRITE_URL=
```

and, at the end of the file, the source reference for any `@env-source` inputs:

```dotenv
## TAILSCALE_MACHINE_NAME: mise run ready
## BIND_ADDRESS: mise run ready
```

## Shared rules

- **One scan channel.** All four markers are parsed line by line from yaml.v3 comment fields. Format is `# @env-xxx NAME: text`, tolerating an omitted leading `#`; `NAME` must be a valid environment variable name, and `text` is required — except `@env-preamble`, which allows an empty body for paragraph separation.
- **Fail closed.** Malformed markers, markers referencing nonexistent variables, conflicting repeated declarations, and stale markers left after their expression was deleted are all hard errors. stacklint never guesses semantics.
- **`COMPOSE_PROJECT_NAME` is reserved** to Docker Compose and cannot carry any marker.

The authoritative, fine-grained rules — ordering, encoding round-trips, exemption interaction, multi-file behavior — live in the [requirements document](/design/requirements#stacklint-env-generate-targets-stacklint-env-check-targets). This page is the concept guide; when the two could disagree, requirements wins.
