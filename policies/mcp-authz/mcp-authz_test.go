/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package mcpauthz

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	policy "github.com/wso2/api-platform/sdk/gateway/policy/v1alpha"
)

func TestGetPolicyValidation(t *testing.T) {
	tests := []struct {
		name    string
		params  map[string]any
		wantErr string
	}{
		{
			name:    "missing rules",
			params:  map[string]any{},
			wantErr: "rules parameter is required",
		},
		{
			name: "rules must be array",
			params: map[string]any{
				"rules": "invalid",
			},
			wantErr: "rules must be an array",
		},
		{
			name: "rules array cannot be empty",
			params: map[string]any{
				"rules": []any{},
			},
			wantErr: "rules must contain at least one rule",
		},
		{
			name: "rule must define claims or scopes",
			params: map[string]any{
				"rules": []any{
					map[string]any{
						"attribute": map[string]any{
							"type": "tool",
						},
					},
				},
			},
			wantErr: "must define at least one of requiredClaims or requiredScopes",
		},
		{
			name: "rule must define non-empty conditions",
			params: map[string]any{
				"rules": []any{
					map[string]any{
						"attribute": map[string]any{
							"type": "tool",
						},
						"requiredClaims": map[string]any{},
						"requiredScopes": []any{},
					},
				},
			},
			wantErr: "must define at least one non-empty authorization condition",
		},
		{
			name: "valid scope-only rule",
			params: map[string]any{
				"rules": []any{
					map[string]any{
						"attribute": map[string]any{
							"type": "tool",
						},
						"requiredScopes": []any{"mcp:tool:read"},
					},
				},
			},
		},
		{
			name: "valid claim-only rule",
			params: map[string]any{
				"rules": []any{
					map[string]any{
						"attribute": map[string]any{
							"type": "resource",
						},
						"requiredClaims": map[string]any{
							"department": "finance",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := GetPolicy(policy.PolicyMetadata{}, tt.params)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if raw == nil {
				t.Fatalf("expected policy instance, got nil")
			}
		})
	}
}

func TestDefaultAttributeNameIsWildcard(t *testing.T) {
	raw, err := GetPolicy(policy.PolicyMetadata{}, map[string]any{
		"rules": []any{
			map[string]any{
				"attribute": map[string]any{
					"type": "tool",
				},
				"requiredScopes": []any{"mcp:tool:read"},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	p, ok := raw.(*McpAuthzPolicy)
	if !ok {
		t.Fatalf("expected *McpAuthzPolicy, got %T", raw)
	}

	matching := p.findMatchingRules("tool", "list_files", "tools/call")
	if len(matching) != 1 {
		t.Fatalf("expected 1 matching rule, got %d", len(matching))
	}
	if matching[0].Attribute.Name != "*" {
		t.Fatalf("expected default attribute.name '*', got %q", matching[0].Attribute.Name)
	}
}

func TestMethodRuleMatchingAndSpecificity(t *testing.T) {
	raw, err := GetPolicy(policy.PolicyMetadata{}, map[string]any{
		"rules": []any{
			map[string]any{
				"attribute": map[string]any{
					"type": "method",
					"name": "*",
				},
				"requiredScopes": []any{"mcp:all"},
			},
			map[string]any{
				"attribute": map[string]any{
					"type": "method",
					"name": "tools/call",
				},
				"requiredScopes": []any{"mcp:tool:call"},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	p := raw.(*McpAuthzPolicy)
	matching := p.findMatchingRules("tool", "list_files", "tools/call")
	if len(matching) != 2 {
		t.Fatalf("expected 2 matching method rules, got %d", len(matching))
	}
	if matching[0].Attribute.Name != "tools/call" {
		t.Fatalf("expected exact method rule first, got %q", matching[0].Attribute.Name)
	}
	if matching[1].Attribute.Name != "*" {
		t.Fatalf("expected wildcard method rule second, got %q", matching[1].Attribute.Name)
	}
}

func TestRuleRequiresClaimsAndScopesConjunction(t *testing.T) {
	p := &McpAuthzPolicy{}
	rule := Rule{
		RequiredClaims: map[string]string{
			"department": "engineering",
		},
		RequiredScopes: []string{"mcp:tool:read"},
	}

	ok, _ := p.ruleGrantsAccess(rule, jwt.MapClaims{
		"department": "engineering",
		"scope":      "mcp:tool:read mcp:tool:list",
	})
	if !ok {
		t.Fatalf("expected rule to grant access when both claims and scopes satisfy")
	}

	ok, missing := p.ruleGrantsAccess(rule, jwt.MapClaims{
		"department": "engineering",
		"scope":      "mcp:tool:list",
	})
	if ok {
		t.Fatalf("expected rule to reject when required scope is missing")
	}
	if len(missing) != 1 || missing[0] != "mcp:tool:read" {
		t.Fatalf("unexpected missing scopes: %v", missing)
	}

	ok, _ = p.ruleGrantsAccess(rule, jwt.MapClaims{
		"department": "finance",
		"scope":      "mcp:tool:read",
	})
	if ok {
		t.Fatalf("expected rule to reject when required claim mismatches")
	}
}

func TestExtractScopesFromScopeAndScpClaims(t *testing.T) {
	p := &McpAuthzPolicy{}

	got := p.extractScopes(jwt.MapClaims{
		"scope": "alpha beta",
	})
	if !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("expected scope claim extraction, got %v", got)
	}

	got = p.extractScopes(jwt.MapClaims{
		"scp": []any{"one", "two"},
	})
	if !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("expected scp claim extraction, got %v", got)
	}

	got = p.extractScopes(jwt.MapClaims{
		"scope": "primary",
		"scp":   []any{"fallback"},
	})
	if !reflect.DeepEqual(got, []string{"primary"}) {
		t.Fatalf("expected scope claim to take precedence, got %v", got)
	}
}

func TestOnRequestDenyResponseContainsExpectedStatusHeaderAndBody(t *testing.T) {
	raw, err := GetPolicy(policy.PolicyMetadata{}, map[string]any{
		"rules": []any{
			map[string]any{
				"attribute": map[string]any{
					"type": "tool",
					"name": "list_files",
				},
				"requiredScopes": []any{"mcp:tool:write"},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	p := raw.(*McpAuthzPolicy)
	ctx := newMcpRequestContext(`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"list_files"}}`)
	ctx.Metadata[MetadataValidatedClaims] = jwt.MapClaims{
		"scope": "mcp:tool:read",
	}

	action := p.OnRequest(ctx, nil)
	resp, ok := action.(policy.ImmediateResponse)
	if !ok {
		t.Fatalf("expected ImmediateResponse, got %T", action)
	}

	if resp.StatusCode != 403 {
		t.Fatalf("expected status 403, got %d", resp.StatusCode)
	}
	if resp.Headers["content-type"] != "application/json" {
		t.Fatalf("expected JSON content type, got %q", resp.Headers["content-type"])
	}

	wwwAuth := resp.Headers[WWWAuthenticateHeader]
	if !strings.Contains(wwwAuth, `resource_metadata="https://api.example.com:9443/base/.well-known/oauth-protected-resource"`) {
		t.Fatalf("unexpected WWW-Authenticate header resource metadata: %q", wwwAuth)
	}
	if !strings.Contains(wwwAuth, `scope="mcp:tool:write"`) {
		t.Fatalf("expected missing scope in WWW-Authenticate header, got %q", wwwAuth)
	}
	if !strings.Contains(wwwAuth, `error="invalid_token"`) {
		t.Fatalf("expected error in WWW-Authenticate header, got %q", wwwAuth)
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
	if body["error"] != "Forbidden" {
		t.Fatalf("expected error Forbidden, got %v", body["error"])
	}
	msg, ok := body["message"].(string)
	if !ok || !strings.Contains(msg, "insufficient permissions") {
		t.Fatalf("unexpected message: %v", body["message"])
	}
}

func newMcpRequestContext(body string) *policy.RequestContext {
	return &policy.RequestContext{
		SharedContext: &policy.SharedContext{
			RequestID:  "test-request-id",
			Metadata:   make(map[string]any),
			APIContext: "/base",
		},
		Headers:   policy.NewHeaders(nil),
		Body:      &policy.Body{Content: []byte(body), Present: true},
		Path:      "/mcp",
		Method:    "POST",
		Scheme:    "https",
		Authority: "gateway.example.com:9443",
		Vhost:     "api.example.com",
	}
}
