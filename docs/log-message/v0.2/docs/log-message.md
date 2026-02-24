---
title: "Overview"
---
# Log Message

## Overview

Log Message logs request and response payloads and headers for observability without modifying traffic.
Use `request` and `response` sections to configure each flow independently.

## Features

- Logs request and response data independently.
- Masks `Authorization` header values automatically.
- Excludes selected headers per flow.
- Preserves request/response body and headers.
- Emits structured JSON logs via `slog`.

## Configuration

### User Parameters (API Definition)

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `request` | object | No* | - | Configure request-flow logging. |
| `request.payload` | boolean | No | `false` | Log request payload. |
| `request.headers` | boolean | No | `false` | Log request headers. |
| `request.excludeHeaders` | array<string> | No | `[]` | Exclude request headers from logs (case-insensitive). |
| `response` | object | No* | - | Configure response-flow logging. |
| `response.payload` | boolean | No | `false` | Log response payload. |
| `response.headers` | boolean | No | `false` | Log response headers. |
| `response.excludeHeaders` | array<string> | No | `[]` | Exclude response headers from logs (case-insensitive). |

\* At least one of `request` or `response` must be configured.

Inside `gateway/build.yaml`, ensure the policy module is included:

```yaml
- name: log-message
  gomodule: github.com/wso2/gateway-controllers/policies/log-message@v0
```

## Examples

### Example 1: Log request and response payloads

```yaml
apiVersion: gateway.api-platform.wso2.com/v1alpha1
kind: RestApi
metadata:
  name: user-api-v1.0
spec:
  displayName: User API
  version: v1.0
  context: /users/$version
  upstream:
    main:
      url: http://user-service:8080
  policies:
    - name: log-message
      version: v0
      params:
        request:
          payload: true
        response:
          payload: true
```

### Example 2: Log request headers and exclude sensitive values

```yaml
apiVersion: gateway.api-platform.wso2.com/v1alpha1
kind: RestApi
metadata:
  name: payment-api-v1.0
spec:
  displayName: Payment API
  version: v1.0
  context: /payments/$version
  upstream:
    main:
      url: http://payment-service:8080
  policies:
    - name: log-message
      version: v0
      params:
        request:
          headers: true
          excludeHeaders:
            - x-api-key
            - x-payment-token
```

### Example 3: Log response headers only

```yaml
apiVersion: gateway.api-platform.wso2.com/v1alpha1
kind: RestApi
metadata:
  name: response-api-v1.0
spec:
  displayName: Response API
  version: v1.0
  context: /response/$version
  upstream:
    main:
      url: http://backend-service:8080
  policies:
    - name: log-message
      version: v0
      params:
        response:
          headers: true
          excludeHeaders:
            - set-cookie
            - x-internal-token
```
