/*
 * Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
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

package tokenbasedratelimit

import (
	"log/slog"
	"sync"

	policy "github.com/wso2/api-platform/sdk/gateway/policy/v1alpha"
	ratelimit "github.com/wso2/gateway-controllers/policies/advanced-ratelimit"
)

const (
	ResourceTypeLlmProviderTemplate = "LlmProviderTemplate"
	MetadataKeyProviderName         = "provider_name"
)

// TokenBasedRateLimitPolicy delegates LLM token-based rate limiting to advanced-ratelimit
// by dynamically resolving cost extraction paths from provider templates.
type TokenBasedRateLimitPolicy struct {
	metadata  policy.PolicyMetadata
	delegates sync.Map // map[string]policy.Policy (providerName -> advanced-ratelimit instance)
}

// GetPolicy creates and initializes the token-based rate limit policy.
// Note: Actual delegates are created lazily in OnRequest because the 
// extraction paths depend on the provider resolved at runtime.
func GetPolicy(
	metadata policy.PolicyMetadata,
	params map[string]interface{},
) (policy.Policy, error) {
	return &TokenBasedRateLimitPolicy{
		metadata: metadata,
	}, nil
}

// Mode returns the processing mode for this policy.
// We buffer the response body to allow the delegate to extract token usage information.
func (p *TokenBasedRateLimitPolicy) Mode() policy.ProcessingMode {
	return policy.ProcessingMode{
		RequestHeaderMode:  policy.HeaderModeProcess,
		RequestBodyMode:    policy.BodyModeSkip,
		ResponseHeaderMode: policy.HeaderModeProcess,
		ResponseBodyMode:   policy.BodyModeBuffered,
	}
}

// OnRequest processes the request phase by delegating to a provider-specific ratelimit instance.
func (p *TokenBasedRateLimitPolicy) OnRequest(
	ctx *policy.RequestContext,
	params map[string]interface{},
) policy.RequestAction {
	providerName, ok := ctx.SharedContext.Metadata[MetadataKeyProviderName].(string)
	if !ok || providerName == "" {
		slog.DebugContext(ctx.Context(), "Provider name not found in metadata; skipping token-based rate limit")
		return policy.ContinueRequest()
	}

	delegate, err := p.resolveDelegate(providerName, params)
	if err != nil {
		slog.WarnContext(ctx.Context(), "Failed to resolve rate limit delegate for provider",
			"provider", providerName, "error", err)
		return policy.ContinueRequest()
	}

	return delegate.OnRequest(ctx, params)
}

// OnResponse processes the response phase by delegating to the same provider-specific instance.
func (p *TokenBasedRateLimitPolicy) OnResponse(
	ctx *policy.ResponseContext,
	params map[string]interface{},
) policy.ResponseAction {
	providerName, ok := ctx.SharedContext.Metadata[MetadataKeyProviderName].(string)
	if !ok || providerName == "" {
		return nil
	}

	if delegate, ok := p.delegates.Load(providerName); ok {
		return delegate.(policy.Policy).OnResponse(ctx, params)
	}

	return nil
}

// resolveDelegate ensures an advanced-ratelimit instance exists for the given provider.
func (p *TokenBasedRateLimitPolicy) resolveDelegate(providerName string, params map[string]interface{}) (policy.Policy, error) {
	if val, ok := p.delegates.Load(providerName); ok {
		return val.(policy.Policy), nil
	}

	// Fetch provider template to get extraction paths
	store := policy.GetLazyResourceStoreInstance()
	templateResource, err := store.GetResourceByIDAndType(providerName, ResourceTypeLlmProviderTemplate)
	if err != nil {
		return nil, err
	}

	// Transform our simplified LLM params into advanced-ratelimit parameters
	rlParams := transformToRatelimitParams(params, templateResource.Resource)

	// Create the delegate instance using the advanced-ratelimit logic
	delegate, err := ratelimit.GetPolicy(p.metadata, rlParams)
	if err != nil {
		return nil, err
	}

	p.delegates.Store(providerName, delegate)
	return delegate, nil
}

// transformToRatelimitParams converts LLM-specific parameters to the advanced-ratelimit structure.
func transformToRatelimitParams(params map[string]interface{}, template map[string]interface{}) map[string]interface{} {
	var quotas []interface{}

	// Helper to create a quota for a specific token type
	addQuota := func(name string, limitsKey string, templateKey string) {
		limits := params[limitsKey]
		if limits == nil {
			return
		}

		quota := map[string]interface{}{
			"name":   name,
			"limits": convertLimits(limits),
			"keyExtraction": []interface{}{
				map[string]interface{}{"type": "routename"},
			},
		}

		// Dynamically inject the cost extraction JSON path from the provider template
		if template != nil {
			if spec, ok := template["spec"].(map[string]interface{}); ok {
				if usage, ok := spec[templateKey].(map[string]interface{}); ok {
					if path, ok := usage["identifier"].(string); ok && path != "" {
						quota["costExtraction"] = map[string]interface{}{
							"enabled": true,
							"sources": []interface{}{
								map[string]interface{}{
									"type":     "response_body",
									"jsonPath": path,
								},
							},
						}
					}
				}
			}
		}
		quotas = append(quotas, quota)
	}

	addQuota("prompt_tokens", "promptTokenLimits", "promptTokens")
	addQuota("completion_tokens", "completionTokenLimits", "completionTokens")
	addQuota("total_tokens", "totalTokenLimits", "totalTokens")

	rlParams := map[string]interface{}{
		"quotas": quotas,
	}

	// Pass through standard system parameters
	for _, key := range []string{"algorithm", "backend", "redis", "memory"} {
		if val, ok := params[key]; ok {
			rlParams[key] = val
		}
	}

	return rlParams
}

// convertLimits transforms the user-facing {count, duration} to advanced-ratelimit's {limit, duration}
func convertLimits(rawLimits interface{}) []interface{} {
	items, ok := rawLimits.([]interface{})
	if !ok {
		return nil
	}
	var converted []interface{}
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			converted = append(converted, map[string]interface{}{
				"limit":    m["count"],
				"duration": m["duration"],
			})
		}
	}
	return converted
}
