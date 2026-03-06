---
title: "Overview"
---
# JSON XML Mediator

## Overview

The JSON XML Mediator policy mediates request and response payloads between the format expected by downstream clients and the format expected by the upstream service.

Use this when backend and client payload formats differ, and you want one policy to convert requests on the way in and responses on the way out.

## Features

- Separately configures downstream and upstream payload formats
- Converts request bodies from `downsteamPayloadFormat` to `upstreamPayloadFormat`
- Converts response bodies from `upstreamPayloadFormat` to `downsteamPayloadFormat`
- Requires both formats to be configured explicitly
- Rejects configurations where `upstreamPayloadFormat` and `downsteamPayloadFormat` are the same
- Updates `content-type` and `content-length` after transformation
- Returns `500` with JSON error payload when conversion fails

## Configuration

### User Parameters (API Definition)

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `upstreamPayloadFormat` | string | Yes | - | Specifies the payload format expected by the upstream service. Supported values: `xml`, `json`. |
| `downsteamPayloadFormat` | string | Yes | - | Specifies the payload format expected by downstream clients. Supported values: `xml`, `json`. Requests are converted from this format to `upstreamPayloadFormat`, and responses are converted back to this format. This value must differ from `upstreamPayloadFormat`. |

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
        downsteamPayloadFormat: json
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
        downsteamPayloadFormat: xml
```

## Notes

- The request payload must match `downsteamPayloadFormat`, and the upstream response payload must match `upstreamPayloadFormat`.
- `upstreamPayloadFormat` and `downsteamPayloadFormat` must be different values.
- Unsupported or invalid payload formats result in a `500` response.
- Empty or absent payloads pass through unchanged.
