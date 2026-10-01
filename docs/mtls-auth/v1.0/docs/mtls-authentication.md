---
title: "Overview"
---
# Mutual TLS Authentication

## Overview

The Mutual TLS Authentication policy authenticates API callers by the client certificate they present. The gateway's HTTPS listener requests a client certificate, and the policy decides whether the caller is one the API accepts. To be accepted, the certificate must chain to a client certificate authority that the API names from the gateway's client-CA pool. The API can further require the certificate to carry a given subject alternative name (SAN) or to have a given thumbprint.

When the gateway sits behind a load balancer that terminates TLS, the load balancer can relay the client certificate in a request header. The policy trusts that header only when the connection it arrived on is authenticated as a known load balancer, called a relay entry in the pool.

## Features

- Authenticates callers by the client certificate presented on the TLS connection
- Accepts certificates from the client certificate authorities an API names in `accept`, chosen from the gateway's client-CA pool. When `accept` is omitted, every client authority in the pool is accepted
- Narrows an accepted authority to certificates with specific URI or DNS SANs, or to specific SHA-256 thumbprints
- Accepts a client certificate relayed in a header by a load balancer, but only on a connection the gateway has authenticated as that load balancer
- Denies a connection whose own certificate is untrusted, expired, not yet valid or unreadable, whatever a header carries
- Controls whether `X-Forwarded-Client-Cert` reaches the backend (`forwardCertificate`)
- Returns one uniform failure response for every denial; the reason appears only in logs and traces
- Sets an authentication context (subject, issuing authority, thumbprint and certificate details) for later policies
- Re-reads the client-CA pool on every request, so pool changes take effect without redeploying the API

## Prerequisites

The policy holds no certificates. It refers to entries of the gateway's client-CA pool by name, so add those entries through the gateway controller's certificate API before deploying an API that uses the policy.

Upload a client certificate authority with `usage: downstream`. The `role` defaults to `client`.

```bash
curl -X POST http://localhost:9090/api/management/v1/certificates \
  -u admin:<password> \
  -H "Content-Type: application/json" \
  -d '{
    "name": "partner-a",
    "usage": "downstream",
    "role": "client",
    "certificate": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----"
  }'
```

When a load balancer relays client certificates in a header, upload the authority that issues the load balancer's own certificate with `role: relay`. An optional `match` narrows the relay entry to connections whose certificate carries one of the listed SANs.

```bash
curl -X POST http://localhost:9090/api/management/v1/certificates \
  -u admin:<password> \
  -H "Content-Type: application/json" \
  -d '{
    "name": "edge-lb",
    "usage": "downstream",
    "role": "relay",
    "match": { "dnsSANs": ["lb.example.com"] },
    "certificate": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----"
  }'
```

A relay entry identifies a load balancer that may forward client certificates; it is not itself an accepted caller. An `accept` entry may name only a `role: client` entry; naming a relay entry is refused at deployment.

## Configuration

The Mutual TLS Authentication policy uses a two-level configuration model. User parameters are configured per API in the API definition YAML, and system parameters are resolved from the gateway configuration.

### User Parameters (API Definition)

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `accept` | array | No | - | Client certificate authorities this policy accepts, evaluated in order; the first matching entry authenticates the request. Omit to accept a certificate issued by any client authority in the pool. An empty list is refused at deployment. |
| `accept[].ca` | string | Yes | - | Name of a `role: client` entry in the gateway's client-CA pool. |
| `accept[].match.uriSANs` | array | No | - | Accepted URI SANs. The certificate must carry at least one of them. |
| `accept[].match.dnsSANs` | array | No | - | Accepted DNS SANs. The certificate must carry at least one of them. When both lists are set, the certificate must satisfy both. A `match` must list at least one SAN. |
| `accept[].thumbprints` | array | No | - | Accepted SHA-256 certificate thumbprints: 64 hex characters, with or without colon separators and a `sha256:` prefix. The gateway stores each as 64 lowercase hex characters; one given in another form is converted, and the deploy response carries an `MTLS_THUMBPRINT_NORMALISED` warning. When set, the list must not be empty. |
| `forwardCertificate` | boolean | No | `true` | If `true`, the backend receives `X-Forwarded-Client-Cert` describing the certificate the caller authenticated with. Set to `false` to remove it. The relayed certificate header never reaches the backend. |
| `onFailureStatusCode` | integer | No | `401` | HTTP status code returned on authentication failure (400-599). |
| `errorMessageFormat` | enum | No | `"json"` | Format of the failure response. One of `json` (structured error), `plain` (plain text) or `minimal` (the status text only). Any other value is refused at deployment. |
| `errorMessage` | string | No | `"Authentication failed"` | Message included in the failure response body. |

SAN values are compared exactly; there is no wildcard or pattern matching. A malformed `accept` list or `forwardCertificate` value is refused at deployment; nothing is silently ignored.

### System Parameters (config.toml)

These parameters are set by the administrator in the gateway configuration and apply to every API on the gateway. They cannot be set in an API definition. When the section isn't in your `config.toml`, add it; a key you leave out keeps its default.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `headerName` | string | No | `"X-WSO2-CLIENT-CERTIFICATE"` | HTTP header in which a trusted front proxy relays the client certificate. Read from the `name` key of the `[router.downstream_tls.client_certificate_header]` section in `config.toml`. |
| `trustAny` | boolean | No | `false` | If `true`, the certificate header is trusted on any connection whose own certificate was not rejected, not only on a connection from a `role: relay` entry. **Enable it only when nothing but a trusted front proxy can reach the gateway.** Read from the `trust_any` key of the same section. |

> **Warning:** With `trust_any` enabled, any client that can open a connection to the gateway can present any certificate in the header and be authenticated as its subject. Enable it only when the gateway is reachable from nothing but a trusted front proxy. While it is on, the gateway logs a warning at startup and every `mtls-auth` deployment carries a `HEADER_CERT_BYPASS_ACTIVE` warning.

#### Sample System Configuration

```toml
[router.downstream_tls.client_certificate_header]
name = "X-WSO2-CLIENT-CERTIFICATE"
trust_any = false
```

**Note:**

Inside the `gateway/build.yaml`, ensure the policy module is added under `policies:`:

```yaml
- name: mtls-auth
  gomodule: github.com/wso2/gateway-controllers/policies/mtls-auth@v1
```

## Reference Scenarios

### Example 1: Accept Any Pooled Client Authority

With no parameters, the policy accepts a certificate issued by any `role: client` entry in the pool.

```yaml
apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata:
  name: mtls-auth-basic-api
spec:
  displayName: mTLS Auth Basic API
  version: v1.0
  context: /mtls-basic/$version
  vhosts:
    main: mtls-basic.example.com
  upstream:
    main:
      url: http://sample-backend:9080/api/v1
  policies:
    - name: mtls-auth
      version: v1
  operations:
    - method: GET
      path: /orders
```

### Example 2: One Authority Narrowed by URI SAN

Accept only certificates issued by `partner-a` that carry the URI SAN `urn:partner-a:payments`.

```yaml
apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata:
  name: mtls-auth-san-api
spec:
  displayName: mTLS Auth SAN API
  version: v1.0
  context: /mtls-san/$version
  vhosts:
    main: mtls-san.example.com
  upstream:
    main:
      url: http://sample-backend:9080/api/v1
  policies:
    - name: mtls-auth
      version: v1
      params:
        accept:
          - ca: partner-a
            match:
              uriSANs:
                - "urn:partner-a:payments"
  operations:
    - method: POST
      path: /payments
```

### Example 3: Several Partners on One API

Entries are tried in order and the first one the certificate satisfies authenticates the request. Here `partner-a` is narrowed to certificates carrying the DNS SAN `orders.partner-a.example`, and any certificate issued by `partner-b` is accepted. The deploy response carries an `MTLS_ACCEPT_UNNARROWED` warning for the `partner-b` entry, because it accepts every certificate that authority issues.

```yaml
apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata:
  name: mtls-auth-partners-api
spec:
  displayName: mTLS Auth Partners API
  version: v1.0
  context: /mtls-partners/$version
  vhosts:
    main: mtls-partners.example.com
  upstream:
    main:
      url: http://sample-backend:9080/api/v1
  policies:
    - name: mtls-auth
      version: v1
      params:
        accept:
          - ca: partner-a
            match:
              dnsSANs:
                - "orders.partner-a.example"
          - ca: partner-b
  operations:
    - method: GET
      path: /orders
```

### Example 4: Thumbprint Pinning

Accept only the listed certificates from `partner-b`. When a pinned caller renews its certificate, list the new thumbprint alongside the old one, let the caller switch, then remove the old one.

To get a certificate's thumbprint in the form the gateway stores:

```bash
openssl x509 -in client.pem -noout -fingerprint -sha256 | cut -d= -f2 | tr -d : | tr A-F a-f
```

```yaml
apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata:
  name: mtls-auth-pinned-api
spec:
  displayName: mTLS Auth Pinned API
  version: v1.0
  context: /mtls-pinned/$version
  vhosts:
    main: mtls-pinned.example.com
  upstream:
    main:
      url: http://sample-backend:9080/api/v1
  policies:
    - name: mtls-auth
      version: v1
      params:
        accept:
          - ca: partner-b
            thumbprints:
              - "5b0d9c2f7e4a1b8c3d6e9f0a2b4c6d8e0f1a3b5c7d9e1f2a4b6c8d0e2f4a6b8c"
  operations:
    - method: GET
      path: /reports
```

### Example 5: Keep the Certificate Away From the Backend

By default the backend receives an `X-Forwarded-Client-Cert` header describing the certificate the caller authenticated with, for example (abbreviated):

```http
X-Forwarded-Client-Cert: Hash=5b0d9c2f...;Cert="-----BEGIN%20CERTIFICATE-----...";Subject="CN=payments,O=Partner A";URI=urn:partner-a:payments
```

Set `forwardCertificate: false` when the backend must not see it. The policy then removes `X-Forwarded-Client-Cert`; the relayed certificate header never reaches the backend in either case.

```yaml
apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata:
  name: mtls-auth-private-api
spec:
  displayName: mTLS Auth Private API
  version: v1.0
  context: /mtls-private/$version
  vhosts:
    main: mtls-private.example.com
  upstream:
    main:
      url: http://sample-backend:9080/api/v1
  policies:
    - name: mtls-auth
      version: v1
      params:
        accept:
          - ca: partner-a
        forwardCertificate: false
  operations:
    - method: GET
      path: /reports
```

### Example 6: Behind a Load Balancer

The load balancer connects with its own certificate, issued by the authority pooled as the `edge-lb` relay entry in Prerequisites, and relays the caller's certificate in the certificate header (`X-WSO2-CLIENT-CERTIFICATE` by default). The API definition is the same as without a load balancer; the relay entry in the pool is what allows the header to be trusted.

```yaml
apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata:
  name: mtls-auth-relay-api
spec:
  displayName: mTLS Auth Relay API
  version: v1.0
  context: /mtls-relay/$version
  vhosts:
    main: mtls-relay.example.com
  upstream:
    main:
      url: http://sample-backend:9080/api/v1
  policies:
    - name: mtls-auth
      version: v1
      params:
        accept:
          - ca: partner-a
  operations:
    - method: GET
      path: /orders
```

The relayed header may carry the certificate as PEM (plain, URL-encoded, or with line breaks replaced by spaces) or as base64-encoded DER. It must carry exactly one value.

### Example 7: Protect One Operation Only

Attach the policy to an operation instead of the API to require a certificate for that operation alone. Attach it at one level only; the same policy at both the API and an operation is refused at deployment.

```yaml
apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata:
  name: mtls-auth-operation-api
spec:
  displayName: mTLS Auth Operation API
  version: v1.0
  context: /mtls-operation/$version
  vhosts:
    main: mtls-operation.example.com
  upstream:
    main:
      url: http://sample-backend:9080/api/v1
  operations:
    - method: GET
      path: /catalog
    - method: POST
      path: /orders
      policies:
        - name: mtls-auth
          version: v1
          params:
            accept:
              - ca: partner-a
```

### Example 8: Certificate and Token Together

Mutual TLS authenticates the caller; it does not authorize or identify an application. To require a token as well, attach `jwt-auth` after `mtls-auth`. Both must pass. Placing another authentication policy before `mtls-auth` is allowed, but the deploy response carries an `MTLS_AUTH_NOT_FIRST` warning.

```yaml
apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata:
  name: mtls-auth-token-api
spec:
  displayName: mTLS Auth Token API
  version: v1.0
  context: /mtls-token/$version
  vhosts:
    main: mtls-token.example.com
  upstream:
    main:
      url: http://sample-backend:9080/api/v1
  policies:
    - name: mtls-auth
      version: v1
      params:
        accept:
          - ca: partner-a
    - name: jwt-auth
      version: v0
      params:
        issuers:
          - PrimaryIDP
  operations:
    - method: GET
      path: /orders
```

### Example 9: Custom Failure Response

Change the status code, the body format and the message the caller receives on a denial. The response is still identical for every cause; the cause is recorded in telemetry only.

```yaml
apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata:
  name: mtls-auth-custom-error-api
spec:
  displayName: mTLS Auth Custom Error API
  version: v1.0
  context: /mtls-custom-error/$version
  vhosts:
    main: mtls-custom-error.example.com
  upstream:
    main:
      url: http://sample-backend:9080/api/v1
  policies:
    - name: mtls-auth
      version: v1
      params:
        accept:
          - ca: partner-a
        onFailureStatusCode: 403
        errorMessageFormat: plain
        errorMessage: "A client certificate issued by Partner A is required"
  operations:
    - method: GET
      path: /orders
```

The caller receives:

```http
HTTP/1.1 403 Forbidden
Content-Type: text/plain

A client certificate issued by Partner A is required
```

## How it Works

Two certificates can take part in a request: the **connection certificate**, presented on the TLS handshake to the gateway, and the **header certificate**, carried in the relay header by a front proxy. The policy runs in the request-header phase and decides in this order.

1. **Envoy checks the connection certificate.** The HTTPS listener asks the client for a certificate and verifies it against the whole client-CA pool. If the certificate is untrusted, expired, not yet valid or unreadable, Envoy still lets the request through to the policy, and the policy denies it. A header cannot save a request whose connection certificate failed this check.

2. **The policy checks the connection certificate against `accept`.** For each `accept` entry, in order, it asks two questions: was this certificate issued by that entry's authority, and does it satisfy the entry's SAN or thumbprint narrowing? The first entry that answers yes to both wins. The request is allowed, the caller's identity is taken from the certificate, and any header certificate is ignored.

3. **If no entry matched, the header certificate may be checked.** This happens only when the connection can be trusted to relay: its certificate matches a `role: relay` pool entry (including that entry's `match`) and is within its validity period, or the `trustAny` system parameter is on. The header must carry exactly one certificate. That certificate is then checked against `accept` in the same way as in step 2. If the connection cannot be trusted to relay, the header is ignored.

4. **Everything else is denied.** No certificate, a certificate the API does not accept with no trusted header, or a header certificate that fails `accept`: each gets the same failure response, described under Error Responses.

### After a request is allowed

The request continues through the rest of the policy chain to the backend, and the policy leaves three things behind it.

* **For later policies in the chain:** an authentication context of type `mtls`:

  | Field | Value |
  |-------|-------|
  | Subject | The matched SAN when the entry narrows by SAN; otherwise the first URI SAN, then the first DNS SAN, then the subject DN |
  | Issuer | Name of the pool entry that accepted the certificate |
  | Credential ID | SHA-256 thumbprint of the certificate |
  | Properties | `subjectDN`, `issuerDN`, `serialNumber`, `notAfter`, `matchedEntry` (index into `accept`), `source` (`handshake`, `header` or `bypass`), and `relayedBy` and `relaySubject` when a relay delivered the certificate |

* **For analytics:** the authentication context's subject is recorded as the user id of the request.

* **For the backend:** at most one certificate header, `X-Forwarded-Client-Cert`, and it always describes the certificate the caller authenticated with. When the caller authenticated through the header certificate, the policy rewrites `X-Forwarded-Client-Cert` to describe that certificate, in the format Envoy uses. The relay header itself never reaches the backend. With `forwardCertificate: false`, `X-Forwarded-Client-Cert` is removed as well.

## Error Responses

Every denial returns the same response, whatever the cause. The response has no `WWW-Authenticate` header.

```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"error":"Unauthorized","message":"Authentication failed"}
```

With `errorMessageFormat: plain` the body is `errorMessage` as `text/plain`; with `minimal` it is `Unauthorized`. The cause (for example `no_certificate`, `expired`, `untrusted_chain`, `authority_not_accepted`, `san_mismatch` or `thumbprint_mismatch`) is recorded as the `mtls_auth.reason` span attribute and in the debug log, never in the response.

## Notes

* **Give each API its own hostname.** Every example sets `vhosts.main`, so the gateway asks for a client certificate only on connections to that hostname, and callers of other APIs, browsers included, aren't asked. An API without `vhosts` is served on the gateway's default hostname. It still works, but the deploy response carries an `MTLS_HOSTNAME_NOT_SCOPED` warning and every connection to the gateway is asked for a certificate. With `mtls_requires_dedicated_hostname` set in the gateway configuration, such an API is refused at deployment.
* **Pool changes apply on the next request.** The accept list is evaluated against the current pool on every request, so adding, replacing or narrowing a pool entry takes effect without redeploying the API.
* **A missing authority never authenticates anyone.** If an `accept` entry names an authority that is not in the pool, the policy skips that entry and tries the rest. If none of the entries is in the pool, the API denies every request with its usual response, and the gateway writes one warning to its log rather than one per request. The gateway refuses to deploy an API that names a missing authority and refuses to delete an authority an API still names, so this happens only when a pool entry cannot be read or in the moment before a pool change has reached the gateway.
* **Load balancer as a caller.** If the load balancer's authority is also pooled as a `role: client` entry that the API accepts, the load balancer's own certificate authenticates every request and the relayed header is ignored, so callers behind it are no longer authenticated individually. The deploy response warns when an accepted authority is also pooled as a relay.
* **Unreadable pool entries.** A pool entry that cannot be read is skipped and logged; the rest of the pool keeps working.

## Related Policies

- **JWT Auth**: Combine with mutual TLS when callers must present both a certificate and a token
- **API Key Auth**: Use for callers that cannot present a client certificate
