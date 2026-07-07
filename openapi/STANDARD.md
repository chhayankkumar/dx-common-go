# OpenAPI Standard for CDPG Go Services

This is the **one** way every Go service in the platform handles API
documentation and request validation. It exists so each service exposes a
browsable, accurate, machine-readable contract the same way, with no per-service
divergence.

**TL;DR** — code-first hand-authored spec → `//go:embed`'d into the binary →
loaded via this package → drives both **request validation** and a **Swagger
UI** at `/docs`. Fail closed on a bad spec. The gateway is the one exception.

---

## The pattern

### 1. Author the spec (code-first)

Hand-write `openapi/openapi.yaml` at the repo root from your handler's wire
structs and routes. OpenAPI 3.1. Keep it **permissive** — only mark genuinely
required fields and avoid `additionalProperties: false` — so turning validation
on never rejects currently-valid traffic.

List both server bases so "try it out" works directly and through the gateway:

```yaml
servers:
  - { url: /,            description: Direct (service-local paths) }
  - { url: /marketplace, description: Through dx-gateway-go (prefix stripped) }
```

### 2. Embed it (never read from disk)

`openapi/spec.go`:

```go
package openapi

import _ "embed"

//go:embed openapi.yaml
var SpecBytes []byte
```

Embedding means a self-contained binary, **no Dockerfile `COPY`**, and the spec
can't drift from the build. Do **not** `os.ReadFile` the spec at runtime — that
is fragile in distroless and silently disables validation on a path miss.

### 3. Add a spec-load test (catch a bad spec in CI)

`openapi/spec_test.go`:

```go
func TestEmbeddedSpecLoads(t *testing.T) {
    if _, err := dxopenapi.NewLoaderFromBytes(openapispec.SpecBytes); err != nil {
        t.Fatalf("embedded OpenAPI spec failed to load: %v", err)
    }
}
```

### 4. Use the shared config block (don't roll your own)

Embed [`openapi.Config`](config.go) in your service config — never a bespoke
`OpenAPIConfig`:

```go
type Config struct {
    // ...
    OpenAPI dxopenapi.Config `mapstructure:"openapi"`
}
```

Standard `config.yaml` block (and matching `Load` defaults):

```yaml
openapi:
  swagger_ui_enabled: true     # dev/staging on; prod configurable
  swagger_ui_path: /docs
  validate_requests: true       # on in every environment
  validate_responses: false
```

### 5. Wire it at boot — fail closed

In `main` (or wherever the router is built), load from the embedded bytes and
**fail closed**: a parse/validate error is a build defect, so stop the process
rather than warn-and-continue.

```go
spec, err := dxopenapi.NewLoaderFromBytes(openapispec.SpecBytes)
if err != nil {
    logger.Fatal("load embedded OpenAPI spec", zap.Error(err))
}
```

### 6. Mount validation + docs

In the chi router builder:

```go
// Request validation — global; skips health paths; passes through routes
// absent from the spec. Mount before the routes (chi requires Use before Handle).
if spec != nil && cfg.OpenAPI.ValidateRequests {
    r.Use(dxopenapi.ValidationMiddleware(spec, cfg.OpenAPI))
}

// Swagger UI + raw spec, open — outside any auth group.
if spec != nil {
    dxopenapi.ServeUI(r, spec, cfg.OpenAPI)
}
```

Result: `GET {swagger_ui_path}` serves Swagger UI and
`GET {swagger_ui_path}/openapi.json` serves the raw document.

---

## Layout (identical in every repo)

```
<service>/
├── openapi/
│   ├── openapi.yaml     # the contract — source of truth
│   ├── spec.go          # //go:embed openapi.yaml  → SpecBytes
│   └── spec_test.go     # asserts the embedded spec loads
├── internal/config/...  # OpenAPI dxopenapi.Config
├── internal/api/router.go  # ValidationMiddleware + ServeUI
└── cmd/server/main.go   # NewLoaderFromBytes (fail closed)
```

A service with **multiple specs** (e.g. dx-community-layer-go: discussion +
challenge) embeds each as its own `SpecBytes` var, builds a loader per spec,
stacks a `ValidationMiddleware` per loader (each passes through the others'
routes), and mounts `ServeUI` per spec under a distinct path
(`/docs/discussion`, `/docs/challenge`).

---

## Exposure policy

| Setting | dev | staging | prod |
|---|---|---|---|
| `validate_requests` | true | true | true |
| `swagger_ui_enabled` | true | true | **false** (or internal-only) |

Set these per environment in `dx-gitops`. Validation is always on; the
browsable UI is dev/staging-only by default.

---

## The gateway exception

`dx-gateway-go` does **no** request-schema validation and ships **no** spec. It
is a stateless PEP that runs one image over a configurable route subset; holding
every upstream's spec would couple it to each service and break that model.
Validation lives with the service that owns the contract. See
[dx-gateway-go/ARCHITECTURE.md](../../dx-gateway-go/ARCHITECTURE.md).

---

## Who follows this

dx-marketplace-go, dx-acl-go, dx-authz-go, dx-files-connect-api-go,
dx-community-layer-go. New Go services adopt it from day one.

## Notes & future

- **Response validation** is not enforced yet (`validate_responses` is reserved;
  `ValidationMiddleware` validates requests only).
- **Code-first, not codegen.** Specs are hand-authored to match hand-written
  handlers. Spec-first generation (e.g. oapi-codegen) is a possible future
  direction but is intentionally out of scope today.
