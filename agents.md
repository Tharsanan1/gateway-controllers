# agents.md — WSO2 Gateway Controllers (Policy Hub)

## Product Overview

Gateway Controllers is the centralized repository for storing, versioning, and managing reusable gateway policies for the WSO2 API Platform Policy Hub. Each policy is an independent Go module that plugs into the API Platform Gateway via the policy SDK. Policies handle cross-cutting concerns like authentication, rate limiting, content moderation, request/response transformation, logging, and AI/LLM guardrails.

This is a **library repository** — it does not run as a standalone service. Policies are compiled into the gateway at build time by referencing them in the gateway's `build.yaml`.

## Architecture

### System Architecture

The repository follows a **modular monorepo** pattern where each policy is a self-contained Go module with its own `go.mod`, source, tests, and `policy-definition.yaml`. Policies are independently versioned and released via git tags.

```
gateway-controllers/
├── policies/                    # 37 independent Go policy modules
│   ├── api-key-auth/            #   Each contains:
│   │   ├── apikey.go            #     - Implementation (Go)
│   │   ├── apikey_test.go       #     - Unit tests
│   │   ├── go.mod               #     - Go module definition
│   │   ├── go.sum               #     - Dependency lock
│   │   └── policy-definition.yaml  # - Parameter schema & version
│   ├── jwt-auth/
│   ├── cors/
│   └── ... (37 total)
└── docs/                        # Versioned policy documentation
    ├── api-key-auth/v0.8/
    │   ├── metadata.json        #   - Policy metadata
    │   └── docs/apikey-authentication.md  # - User-facing docs
    └── ...
```

**How policies integrate with the gateway:**

Policies are loaded into the API Platform Gateway via Go module imports in `gateway/build.yaml` (in the `wso2/api-platform` repository):
```yaml
- name: api-key-auth
  gomodule: github.com/wso2/gateway-controllers/policies/api-key-auth@v0
```

The Gateway Builder compiles these into the gateway binary. At runtime, the Policy Engine evaluates them using CEL expressions via gRPC ext_proc.

### Module/Component Map

| Module | Path | Responsibility |
|--------|------|----------------|
| api-key-auth | `policies/api-key-auth/` | Validates API keys from headers or query params |
| basic-auth | `policies/basic-auth/` | HTTP Basic Authentication |
| jwt-auth | `policies/jwt-auth/` | JWT access token validation with JWKS |
| subscription-validation | `policies/subscription-validation/` | Validates API subscriptions |
| mcp-auth | `policies/mcp-auth/` | MCP (Model Context Protocol) authentication |
| mcp-authz | `policies/mcp-authz/` | MCP authorization |
| mcp-acl-list | `policies/mcp-acl-list/` | MCP access control lists |
| mcp-rewrite | `policies/mcp-rewrite/` | MCP request rewriting |
| cors | `policies/cors/` | Cross-Origin Resource Sharing headers |
| basic-ratelimit | `policies/basic-ratelimit/` | Simple request-count rate limiting |
| advanced-ratelimit | `policies/advanced-ratelimit/` | CEL-based rate limiting with Redis backend |
| token-based-ratelimit | `policies/token-based-ratelimit/` | LLM token-count rate limiting |
| llm-cost-based-ratelimit | `policies/llm-cost-based-ratelimit/` | LLM cost-based rate limiting |
| llm-cost | `policies/llm-cost/` | LLM usage cost calculation |
| semantic-cache | `policies/semantic-cache/` | Semantic similarity caching for LLM responses |
| semantic-prompt-guard | `policies/semantic-prompt-guard/` | Prompt injection detection via embeddings |
| pii-masking-regex | `policies/pii-masking-regex/` | Regex-based PII masking |
| json-schema-guardrail | `policies/json-schema-guardrail/` | JSON Schema validation guardrail |
| regex-guardrail | `policies/regex-guardrail/` | Regex pattern matching guardrail |
| content-length-guardrail | `policies/content-length-guardrail/` | Content length enforcement |
| url-guardrail | `policies/url-guardrail/` | URL pattern validation |
| sentence-count-guardrail | `policies/sentence-count-guardrail/` | Sentence count enforcement |
| word-count-guardrail | `policies/word-count-guardrail/` | Word count enforcement |
| aws-bedrock-guardrail | `policies/aws-bedrock-guardrail/` | AWS Bedrock guardrail integration |
| azure-content-safety-content-moderation | `policies/azure-content-safety-content-moderation/` | Azure Content Safety API integration |
| request-rewrite | `policies/request-rewrite/` | Request path/header/body rewriting |
| json-xml-mediator | `policies/json-xml-mediator/` | JSON ↔ XML transformation |
| prompt-template | `policies/prompt-template/` | LLM prompt templating |
| prompt-decorator | `policies/prompt-decorator/` | LLM prompt decoration (system/user prefix/suffix) |
| dynamic-endpoint | `policies/dynamic-endpoint/` | Dynamic upstream endpoint routing |
| set-headers | `policies/set-headers/` | Add/set HTTP headers |
| remove-headers | `policies/remove-headers/` | Remove HTTP headers |
| analytics-header-filter | `policies/analytics-header-filter/` | Filter headers for analytics collection |
| log-message | `policies/log-message/` | Structured request/response logging |
| model-round-robin | `policies/model-round-robin/` | Round-robin LLM model selection |
| model-weighted-round-robin | `policies/model-weighted-round-robin/` | Weighted round-robin LLM model selection |
| respond | `policies/respond/` | Return immediate static responses |

### Key Abstractions & Patterns

All policies implement the **Policy interface** from `github.com/wso2/api-platform/sdk/gateway/policy/v1alpha`:

```go
type Policy interface {
    Mode() ProcessingMode                                              // Declare what the policy needs (headers, body)
    OnRequest(ctx *RequestContext, params map[string]interface{}) RequestAction    // Process incoming request
    OnResponse(ctx *ResponseContext, params map[string]interface{}) ResponseAction // Process outgoing response
}
```

**Key patterns:**
- **ProcessingMode declaration**: Each policy declares what it needs via `Mode()` — whether it processes request/response headers and/or body. This enables the gateway to skip unnecessary processing.
- **Parameter schema**: Policy parameters are defined in `policy-definition.yaml` using JSON Schema with WSO2 extensions (`x-wso2-policy-advanced-param` for UI grouping, `wso2/defaultValue` for system parameter resolution from `config.toml`).
- **Two parameter types**: User parameters (per-API, set by developer) and system parameters (global, resolved from gateway `config.toml` at startup).
- **Action-based return**: Policies return actions (`RequestContinue`, `RequestError`, `ImmediateResponse`) rather than modifying state directly.
- **Custom mocks over frameworks**: Tests use hand-written mock structs (embedding the interface, overriding needed methods) rather than mock generation frameworks.
- **Independent versioning**: Each policy has its own semantic version in `policy-definition.yaml`, released via git tags (`policies/<name>/v<X.Y.Z>`).

### Inter-Component Communication

This repository does not contain running services. Policies communicate with the gateway runtime through:

| Interface | Protocol | Details |
|-----------|----------|---------|
| Policy Engine → Policy | Go function call | Policies are compiled into the Policy Engine binary and invoked directly |
| Policy → External services | HTTP/HTTPS | Some policies call external APIs (JWKS endpoints, Azure Content Safety, AWS Bedrock, embedding providers) |
| Policy → Redis | TCP | Rate limiting policies (advanced-ratelimit, token-based-ratelimit) use Redis for distributed state |
| Policy → Vector DB | gRPC/HTTP | Semantic policies (semantic-cache, semantic-prompt-guard) use Milvus for vector operations |

## Feature Inventory

| Feature | Documentation | Related Policies |
|---------|---------------|-----------------|
| API Key Authentication | `docs/api-key-auth/v0.8/docs/apikey-authentication.md` | api-key-auth |
| Basic Authentication | `docs/basic-auth/v0.8/docs/basic-auth.md` | basic-auth |
| JWT Authentication | `docs/jwt-auth/v0.8/docs/jwt-auth.md` | jwt-auth |
| Subscription Validation | `docs/subscription-validation/v0.3/docs/subscription-validation.md` | subscription-validation |
| MCP Authentication & Authorization | `docs/mcp-auth/`, `docs/mcp-authz/`, `docs/mcp-acl-list/` | mcp-auth, mcp-authz, mcp-acl-list |
| CORS | `docs/cors/v0.8/docs/cors.md` | cors |
| Basic Rate Limiting | `docs/basic-ratelimit/v0.8/docs/basic-ratelimit.md` | basic-ratelimit |
| Advanced Rate Limiting | `docs/advanced-ratelimit/v0.3/docs/advanced-ratelimit.md` | advanced-ratelimit |
| Token-Based Rate Limiting | `docs/token-based-ratelimit/v0.2/docs/token-based-ratelimit.md` | token-based-ratelimit |
| LLM Cost Tracking | `docs/llm-cost/v0.3/docs/llm-cost.md` | llm-cost |
| LLM Cost-Based Rate Limiting | `docs/llm-cost-based-ratelimit/v0.2/docs/llm-cost-based-ratelimit.md` | llm-cost-based-ratelimit |
| Semantic Caching | `docs/semantic-cache/v0.2/docs/semantic-cache.md` | semantic-cache |
| Prompt Guard | `docs/semantic-prompt-guard/v0.2/docs/semantic-prompt-guard.md` | semantic-prompt-guard |
| PII Masking | `docs/pii-masking-regex/v0.8/docs/pii-masking-regex.md` | pii-masking-regex |
| Content Guardrails | `docs/json-schema-guardrail/`, `docs/regex-guardrail/`, `docs/content-length-guardrail/`, `docs/url-guardrail/`, `docs/sentence-count-guardrail/`, `docs/word-count-guardrail/` | Various guardrail policies |
| AWS Bedrock Guardrail | `docs/aws-bedrock-guardrail/v0.2/docs/aws-bedrock-guardrail.md` | aws-bedrock-guardrail |
| Azure Content Safety | `docs/azure-content-safety-content-moderation/v0.8/docs/azure-content-safety-content-moderation.md` | azure-content-safety-content-moderation |
| Request Rewriting | `docs/request-rewrite/v0.3/docs/request-rewrite.md` | request-rewrite |
| JSON/XML Mediation | `docs/json-xml-mediator/v0.2/docs/json-xml-mediator.md` | json-xml-mediator |
| Prompt Templating | `docs/prompt-template/v0.8/docs/prompt-template.md` | prompt-template |
| Header Management | `docs/set-headers/`, `docs/remove-headers/` | set-headers, remove-headers |
| Logging | `docs/log-message/v0.2/docs/log-message.md` | log-message |
| Model Load Balancing | `docs/model-round-robin/`, `docs/model-weighted-round-robin/` | model-round-robin, model-weighted-round-robin |

## Deployment

### Prerequisites

- **Go 1.25.1+** (check individual policy's `go.mod` — versions range from 1.23.0 to 1.25.1)
- **Docker & Docker Compose** (only needed for running integration tests against the full gateway stack)
- **Git** (for cloning and tagging releases)

### Build & Run

This is a library repository — there is no standalone build or run. Each policy is built and tested independently:

```bash
# Build/test a specific policy
cd policies/<policy-name>
go build ./...
go test -v -race ./...

# Example: test the jwt-auth policy
cd policies/jwt-auth
go test -v -race ./...
```

**Integration testing against the full gateway stack** (requires the `wso2/api-platform` repository):

To test a policy change against the real gateway locally, use the dev-policies workflow in the `api-platform` repo. See `gateway/dev-policies/README.md` in that repo for the full procedure. In short:

1. Copy your policy into `api-platform/gateway/dev-policies/<policy-name>/`
2. Copy `policy-definition.yaml` to `api-platform/gateway/gateway-controller/default-policies/<policy-name>.yaml`
3. Register it in `api-platform/gateway/build.yaml` with a `filePath` entry
4. Build the gateway runtime: `cd api-platform/gateway/gateway-runtime && make build`
5. Run integration tests or the full stack via docker compose

Refer to the **api-platform** repository's `agents.md > Testing > Testing Dev Policies Locally` section for detailed steps and cleanup instructions.

In CI, this is automated by `.github/workflows/gateway-integration-test.yml` which copies policies into api-platform, builds coverage images, and runs the gateway integration test suite.

### Configuration

**Per-policy configuration is defined in `policy-definition.yaml`:**

Each policy declares its parameters and system parameters in this file. The schema uses JSON Schema with WSO2 extensions:

- `parameters` — user-configurable per API (set in API definition YAML)
- `systemParameters` — resolved from the gateway's `config.toml` at startup, referenced via `wso2/defaultValue: "${config.section.key}"`
- `x-wso2-policy-advanced-param: true/false` — controls UI grouping (basic vs advanced params)

### Health Check

Not applicable — this is a library, not a running service. Policy health is validated through unit tests and integration tests against the full gateway stack.

## Testing

### Test Framework & Conventions

- **Framework:** Go standard `testing` package. No external assertion libraries in most policies (some use `stretchr/testify`). Integration tests in the gateway use `cucumber/godog` (BDD/Gherkin), but those live in the `api-platform` repo.
- **Naming convention:** `Test{PolicyName}_{Scenario}` or `Test{PolicyName}Policy_{Action}_{Condition}` (e.g., `TestBasicAuthPolicy_OnRequest_ValidCredentials`, `TestJWTAuthPolicy_ValidToken`, `TestLogMessagePolicy_Mode`). Sub-tests via `t.Run("descriptive name", ...)`.
- **Unit test location:** Co-located with source — `policies/<name>/<name>_test.go` alongside `<name>.go`.
- **Integration test location:** Full gateway integration tests live in `wso2/api-platform` at `gateway/it/`. This repo's CI triggers those tests via `.github/workflows/gateway-integration-test.yml`.

### Running Tests

```bash
# Run tests for a single policy
cd policies/<policy-name>
go test -v -race ./...

# Run tests with coverage
cd policies/<policy-name>
go test -v -race -cover -coverprofile=coverage.txt ./...

# Run a specific test function
cd policies/<policy-name>
go test -v -race -run TestFunctionName ./...

# There is no root-level "test all" command — each policy is an independent Go module.
# To test all policies, iterate:
for dir in policies/*/; do
  (cd "$dir" && go test -v -race ./...)
done
```

### Test Infrastructure

- **Custom mock structs**: Policies use hand-written mocks that embed the interface and override only needed methods. No mock generation frameworks.
  ```go
  type mockEmbeddingProvider struct {
      getEmbeddingFn func(input string) ([]float32, error)
  }
  ```
- **Context factory helpers**: Test files define helpers like `newBasicRequestContext()`, `createMockRequestContext()` to build `policy.RequestContext` and `policy.ResponseContext` objects.
- **Assertion helpers**: Custom assertion functions like `assertImmediateResponse(t, action, expectedStatus)`.
- **HTTP test servers**: For policies that call external endpoints (e.g., jwt-auth uses `httptest.NewServer` for mock JWKS endpoints).
- **Test data files**: Complex policies store fixtures in `testdata/` subdirectories (e.g., `policies/llm-cost/testdata/model_prices.json`).
- **Table-driven tests**: Standard Go pattern with `[]struct{ name string; ... }` and `t.Run()`.
- **Race detection**: All tests run with `-race` flag in CI.
- **Integration test mocks** (in `api-platform` repo): mock-jwks, mock-azure-content-safety, mock-aws-bedrock-guardrail, mock-embedding-provider, mock-analytics-collector.

## Coding Conventions

### Style & Formatting

- **Go standard formatting**: `gofmt` / `goimports`. No `.golangci.yml` in this repo — relies on developer discipline and PR review.
- **Copyright header**: All Go files must start with the Apache 2.0 copyright block:
  ```go
  /*
   *  Copyright (c) 2025, WSO2 LLC. (http://www.wso2.org) All Rights Reserved.
   *
   *  Licensed under the Apache License, Version 2.0 ...
   */
  ```
- **Import ordering**: Standard library first, then third-party (SDK, external packages). Use import aliases for clarity (e.g., `policy "github.com/wso2/api-platform/sdk/gateway/policy/v1alpha"`).
- **Logging**: Use `log/slog` for structured logging (not `fmt.Println` or `log.Printf`).
- **Package naming**: Short, lowercase, single-word (e.g., `package apikey`, `package cors`).
- **File naming**: Snake-case matching the policy name (e.g., `apikey.go`, `basicauth.go`, `logmessage.go`).

### Commit Message Format

Imperative mood, short title. No strict Conventional Commits enforcement, but common patterns:

- `Add <feature>` — new functionality
- `Fix <issue>` — bug fix
- `Update <component>` — modifications
- `refactor <scope>` — code restructuring

Version update commits: `Update <policy-name> policy version to v<X.Y.Z>`

### Branch Naming

- `feature/<description>` — new features (e.g., `feature/llm-improvements`)
- `fix/<description>` — bug fixes
- `codex/<description>` — experimental/AI-assisted work
- Descriptive kebab-case names

## Contribution Guidelines

### PR Process

- All `go.mod` and `go.sum` changes require approval from code owners: @pubudu538, @malinthaprasan, @Krishanx92 (defined in `.github/CODEOWNERS`).
- Integration tests run automatically on PRs that touch `policies/**` (`.github/workflows/gateway-integration-test.yml`).
- Releases are manual via the `release-policy.yml` workflow (workflow_dispatch with policy name and version inputs).
- Release workflow validates: input format, policy structure (`go.mod` + `policy-definition.yaml` exist), version consistency, tests pass (`go test -v -race ./...`), clean git state, version uniqueness.
- Git tags follow: `policies/<policy-name>/v<X.Y.Z>`

### PR Template Location

`pull_request_template.md` (at repository root)

### Labels & Categories

No structured label system in this repository (unlike api-platform). Issues use free-form labels suggested by the reporter in the issue template.

## References

- **Policy SDK**: `github.com/wso2/api-platform/sdk/gateway/policy/v1alpha` — the interface all policies implement
- **Common utilities**: `github.com/wso2/api-platform/common` — shared logger, config, errors, models
- **Gateway integration**: Policies are registered in `gateway/build.yaml` in the `wso2/api-platform` repo
- **Documentation creation guide**: `.github/skills/doc-create/SKILL.md` — detailed guide for writing policy docs
- **Policy documentation template**: Each policy doc must include: Overview, Features, Configuration (with `build.yaml` snippet), Reference Scenarios
- **Policy metadata format**: `docs/<name>/v<X.Y>/metadata.json` — name, displayName, version, provider, categories, description
- **Release workflow**: `.github/workflows/release-policy.yml`
- **Integration test workflow**: `.github/workflows/gateway-integration-test.yml`
- **License**: Apache 2.0
