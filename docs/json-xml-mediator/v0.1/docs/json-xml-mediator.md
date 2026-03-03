---
title: "Overview"
---
# JSON XML Mediator

## Overview

The JSON XML Mediator policy converts request payloads in one direction and automatically applies the reverse conversion in the response flow.

Use this when backend and client payload formats differ, and you want one policy to keep request and response formats symmetric.

## Features

- Converts request payloads based on one parameter
- Automatically applies the inverse conversion on responses
- Supports both directions:
  - `upstreamPayloadFormat: xml` converts request `JSON -> XML` and response `XML -> JSON`
  - `upstreamPayloadFormat: json` converts request `XML -> JSON` and response `JSON -> XML`
- Updates `content-type` and `content-length` after transformation
- Returns `500` with JSON error payload when conversion fails

## Configuration

### User Parameters (API Definition)

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `upstreamPayloadFormat` | string | Yes | - | Specifies the payload format expected by upstream. Supported values: `xml`, `json`. Response conversion is applied in reverse automatically. |

**Note:**

Inside the `gateway/build.yaml`, ensure the policy module is added under `policies:`:

```yaml
- name: json-xml-mediator
  gomodule: github.com/wso2/gateway-controllers/policies/json-xml-mediator@v0
```

## Reference Scenarios

### Example 1: Request JSON to XML, Response XML to JSON

```yaml
apiVersion: gateway.api-platform.wso2.com/v1alpha1
kind: RestApi
metadata:
  name: mediation-api-v1.0
spec:
  displayName: Mediation API
  version: v1.0
  context: /mediation/$version
  upstream:
    main:
      url: http://legacy-xml-backend:8080
  policies:
    - name: json-xml-mediator
      version: v0
      params:
        upstreamPayloadFormat: xml
```

### Example 2: Request XML to JSON, Response JSON to XML

```yaml
apiVersion: gateway.api-platform.wso2.com/v1alpha1
kind: RestApi
metadata:
  name: mediation-api-v1.0
spec:
  displayName: Mediation API
  version: v1.0
  context: /mediation/$version
  upstream:
    main:
      url: http://json-backend:8080
  policies:
    - name: json-xml-mediator
      version: v0
      params:
        upstreamPayloadFormat: json
```

## Notes

- The request and response payloads must match the expected source format for each conversion direction.
- Unsupported or invalid payload formats result in a `500` response.
- Empty or absent payloads pass through unchanged.
