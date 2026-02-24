package basicauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	policy "github.com/wso2/api-platform/sdk/gateway/policy/v1alpha"
)

func newRequestContext(headers map[string][]string) *policy.RequestContext {
	return &policy.RequestContext{
		SharedContext: &policy.SharedContext{
			Metadata: map[string]interface{}{},
		},
		Headers: policy.NewHeaders(headers),
	}
}

func encodeBasicCredentials(username, password string) string {
	raw := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
}

func parseErrorBody(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}
	return got
}

func assertImmediateResponse(t *testing.T, action policy.RequestAction, expectedStatus int) policy.ImmediateResponse {
	t.Helper()
	ir, ok := action.(policy.ImmediateResponse)
	if !ok {
		t.Fatalf("expected ImmediateResponse, got %T", action)
	}
	if ir.StatusCode != expectedStatus {
		t.Fatalf("expected status %d, got %d", expectedStatus, ir.StatusCode)
	}
	return ir
}

func assertUpstreamRequestModifications(t *testing.T, action policy.RequestAction) {
	t.Helper()
	if _, ok := action.(policy.UpstreamRequestModifications); !ok {
		t.Fatalf("expected UpstreamRequestModifications, got %T", action)
	}
}

func TestGetPolicyReturnsSingleton(t *testing.T) {
	p1, err := GetPolicy(policy.PolicyMetadata{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p2, err := GetPolicy(policy.PolicyMetadata{}, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p1 == nil || p2 == nil {
		t.Fatalf("expected non-nil policy instances")
	}
	if p1 != p2 {
		t.Fatalf("expected singleton policy instance, got different pointers")
	}
	if _, ok := p1.(*BasicAuthPolicy); !ok {
		t.Fatalf("expected *BasicAuthPolicy, got %T", p1)
	}
}

func TestMode(t *testing.T) {
	p := &BasicAuthPolicy{}
	mode := p.Mode()

	if mode.RequestHeaderMode != policy.HeaderModeProcess {
		t.Fatalf("expected RequestHeaderMode=Process, got %v", mode.RequestHeaderMode)
	}
	if mode.RequestBodyMode != policy.BodyModeSkip {
		t.Fatalf("expected RequestBodyMode=Skip, got %v", mode.RequestBodyMode)
	}
	if mode.ResponseHeaderMode != policy.HeaderModeSkip {
		t.Fatalf("expected ResponseHeaderMode=Skip, got %v", mode.ResponseHeaderMode)
	}
	if mode.ResponseBodyMode != policy.BodyModeSkip {
		t.Fatalf("expected ResponseBodyMode=Skip, got %v", mode.ResponseBodyMode)
	}
}

func TestOnRequestInvalidConfig(t *testing.T) {
	p := &BasicAuthPolicy{}
	tests := []struct {
		name          string
		params        map[string]interface{}
		expectMessage string
	}{
		{
			name:          "missing username",
			params:        map[string]interface{}{"password": "pass"},
			expectMessage: "Invalid policy configuration: username must be a non-empty string",
		},
		{
			name:          "empty username",
			params:        map[string]interface{}{"username": "", "password": "pass"},
			expectMessage: "Invalid policy configuration: username must be a non-empty string",
		},
		{
			name:          "username wrong type",
			params:        map[string]interface{}{"username": 123, "password": "pass"},
			expectMessage: "Invalid policy configuration: username must be a non-empty string",
		},
		{
			name:          "missing password",
			params:        map[string]interface{}{"username": "user"},
			expectMessage: "Invalid policy configuration: password must be a non-empty string",
		},
		{
			name:          "empty password",
			params:        map[string]interface{}{"username": "user", "password": ""},
			expectMessage: "Invalid policy configuration: password must be a non-empty string",
		},
		{
			name:          "password wrong type",
			params:        map[string]interface{}{"username": "user", "password": 123},
			expectMessage: "Invalid policy configuration: password must be a non-empty string",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newRequestContext(nil)
			action := p.OnRequest(ctx, tc.params)
			ir := assertImmediateResponse(t, action, 500)
			if got := ir.Headers["content-type"]; got != "application/json" {
				t.Fatalf("expected content-type application/json, got %q", got)
			}
			body := parseErrorBody(t, ir.Body)
			if body["error"] != "Internal Server Error" {
				t.Fatalf("expected Internal Server Error, got %q", body["error"])
			}
			if body["message"] != tc.expectMessage {
				t.Fatalf("expected message %q, got %q", tc.expectMessage, body["message"])
			}
		})
	}
}

func TestOnRequestSuccessfulAuthentication(t *testing.T) {
	p := &BasicAuthPolicy{}
	ctx := newRequestContext(map[string][]string{
		"Authorization": {encodeBasicCredentials("admin", "secret")},
	})

	action := p.OnRequest(ctx, map[string]interface{}{
		"username": "admin",
		"password": "secret",
	})

	assertUpstreamRequestModifications(t, action)
	if got, ok := ctx.Metadata[MetadataKeyAuthSuccess].(bool); !ok || !got {
		t.Fatalf("expected auth.success=true, got %#v", ctx.Metadata[MetadataKeyAuthSuccess])
	}
	if got := ctx.Metadata[MetadataKeyAuthUser]; got != "admin" {
		t.Fatalf("expected auth.username=admin, got %#v", got)
	}
	if got := ctx.Metadata[MetadataKeyAuthMethod]; got != "basic" {
		t.Fatalf("expected auth.method=basic, got %#v", got)
	}
}

func TestOnRequestAuthenticationFailuresReturn401(t *testing.T) {
	p := &BasicAuthPolicy{}
	baseParams := map[string]interface{}{
		"username": "admin",
		"password": "secret",
	}

	tests := []struct {
		name    string
		headers map[string][]string
		params  map[string]interface{}
	}{
		{
			name:    "missing authorization header",
			headers: map[string][]string{},
			params:  baseParams,
		},
		{
			name: "invalid authorization scheme",
			headers: map[string][]string{
				"authorization": {"Bearer token"},
			},
			params: baseParams,
		},
		{
			name: "lowercase basic scheme rejected",
			headers: map[string][]string{
				"authorization": {"basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret"))},
			},
			params: baseParams,
		},
		{
			name: "invalid base64",
			headers: map[string][]string{
				"authorization": {"Basic @@@@"},
			},
			params: baseParams,
		},
		{
			name: "invalid credentials format no colon",
			headers: map[string][]string{
				"authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte("admin-only"))},
			},
			params: baseParams,
		},
		{
			name: "invalid credentials",
			headers: map[string][]string{
				"authorization": {encodeBasicCredentials("admin", "wrong")},
			},
			params: baseParams,
		},
		{
			name: "only first authorization header is used",
			headers: map[string][]string{
				"authorization": {"Bearer token", encodeBasicCredentials("admin", "secret")},
			},
			params: baseParams,
		},
		{
			name: "invalid allowUnauthenticated type defaults to false",
			headers: map[string][]string{
				"authorization": {"Bearer token"},
			},
			params: map[string]interface{}{
				"username":             "admin",
				"password":             "secret",
				"allowUnauthenticated": "true",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newRequestContext(tc.headers)
			action := p.OnRequest(ctx, tc.params)
			ir := assertImmediateResponse(t, action, 401)

			if got := ir.Headers["content-type"]; got != "application/json" {
				t.Fatalf("expected content-type application/json, got %q", got)
			}
			if got := ir.Headers["www-authenticate"]; got != `Basic realm="Restricted"` {
				t.Fatalf("expected default www-authenticate realm, got %q", got)
			}

			body := parseErrorBody(t, ir.Body)
			if body["error"] != "Unauthorized" {
				t.Fatalf("expected Unauthorized error, got %q", body["error"])
			}
			if body["message"] != "Authentication required" {
				t.Fatalf("expected Authentication required message, got %q", body["message"])
			}

			if got, ok := ctx.Metadata[MetadataKeyAuthSuccess].(bool); !ok || got {
				t.Fatalf("expected auth.success=false, got %#v", ctx.Metadata[MetadataKeyAuthSuccess])
			}
			if got := ctx.Metadata[MetadataKeyAuthMethod]; got != "basic" {
				t.Fatalf("expected auth.method=basic, got %#v", got)
			}
			if _, exists := ctx.Metadata[MetadataKeyAuthUser]; exists {
				t.Fatalf("did not expect auth.username metadata on failure")
			}
		})
	}
}

func TestOnRequestRealmOverrideAndEscaping(t *testing.T) {
	p := &BasicAuthPolicy{}
	ctx := newRequestContext(nil)
	realm := `my "realm"\name`

	action := p.OnRequest(ctx, map[string]interface{}{
		"username": "admin",
		"password": "secret",
		"realm":    realm,
	})
	ir := assertImmediateResponse(t, action, 401)

	expectedEscaped := strings.ReplaceAll(strings.ReplaceAll(realm, "\\", "\\\\"), "\"", "\\\"")
	expected := `Basic realm="` + expectedEscaped + `"`
	if got := ir.Headers["www-authenticate"]; got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestOnRequestInvalidRealmFallsBackToDefault(t *testing.T) {
	p := &BasicAuthPolicy{}
	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name: "empty realm",
			params: map[string]interface{}{
				"username": "admin",
				"password": "secret",
				"realm":    "",
			},
		},
		{
			name: "non-string realm",
			params: map[string]interface{}{
				"username": "admin",
				"password": "secret",
				"realm":    123,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newRequestContext(nil)
			action := p.OnRequest(ctx, tc.params)
			ir := assertImmediateResponse(t, action, 401)
			if got := ir.Headers["www-authenticate"]; got != `Basic realm="Restricted"` {
				t.Fatalf("expected default realm, got %q", got)
			}
		})
	}
}

func TestOnRequestAllowUnauthenticated(t *testing.T) {
	p := &BasicAuthPolicy{}
	ctx := newRequestContext(nil)

	action := p.OnRequest(ctx, map[string]interface{}{
		"username":             "admin",
		"password":             "secret",
		"allowUnauthenticated": true,
	})

	assertUpstreamRequestModifications(t, action)
	if got, ok := ctx.Metadata[MetadataKeyAuthSuccess].(bool); !ok || got {
		t.Fatalf("expected auth.success=false, got %#v", ctx.Metadata[MetadataKeyAuthSuccess])
	}
	if got := ctx.Metadata[MetadataKeyAuthMethod]; got != "basic" {
		t.Fatalf("expected auth.method=basic, got %#v", got)
	}
	if _, exists := ctx.Metadata[MetadataKeyAuthUser]; exists {
		t.Fatalf("did not expect auth.username metadata for unauthenticated pass-through")
	}
}

func TestOnResponseReturnsNil(t *testing.T) {
	p := &BasicAuthPolicy{}
	action := p.OnResponse(&policy.ResponseContext{}, map[string]interface{}{})
	if action != nil {
		t.Fatalf("expected nil response action, got %T", action)
	}
}
