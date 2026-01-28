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

// GetPolicy initializes the token-based rate limit policy.
func GetPolicy(
	metadata policy.PolicyMetadata,
	params map[string]interface{},
) (policy.Policy, error) {
	return &TokenBasedRateLimitPolicy{
		metadata: metadata,
	}, nil
}

// Mode returns the processing mode. We need to buffer the response body
// to extract token usage information.
func (p *TokenBasedRateLimitPolicy) Mode() policy.ProcessingMode {
	return policy.ProcessingMode{
		RequestHeaderMode:  policy.HeaderModeProcess,
		RequestBodyMode:    policy.BodyModeSkip,
		ResponseHeaderMode: policy.HeaderModeProcess,
		ResponseBodyMode:   policy.BodyModeBuffered,
	}
}

// resolveDelegate ensures an advanced-ratelimit instance exists for the given provider.
func (p *TokenBasedRateLimitPolicy) resolveDelegate(ctx *policy.RequestContext, providerName string, params map[string]interface{}) (policy.Policy, error) {
	if delegate, ok := p.delegates.Load(providerName); ok {
		return delegate.(policy.Policy), nil
	}

	// Fetch template from LazyResourceStore
	store := policy.GetLazyResourceStoreInstance()
	templateResource, err := store.GetResourceByIDAndType(providerName, ResourceTypeLlmProviderTemplate)
	if err != nil {
		return nil, err
	}

	// Transform LLM limits into advanced-ratelimit quotas using JSON paths from the template
	rlParams := transformToRatelimitParams(params, templateResource.Resource)

	// Create the delegate instance
	delegate, err := ratelimit.GetPolicy(p.metadata, rlParams)
	if err != nil {
		return nil, err
	}

	p.delegates.Store(providerName, delegate)
	return delegate, nil
}

// OnRequest dynamically resolves the delegate and executes the rate limit check.
func (p *TokenBasedRateLimitPolicy) OnRequest(
	ctx *policy.RequestContext,
	params map[string]interface{},
) policy.RequestAction {
	providerName, ok := ctx.SharedContext.Metadata[MetadataKeyProviderName].(string)
	if !ok || providerName == "" {
		slog.WarnContext(ctx.Context(), "Provider name not found in metadata; skipping token-based rate limit",
			"request_id", ctx.SharedContext.RequestID)
		return policy.ContinueRequest()
	}

	delegate, err := p.resolveDelegate(ctx, providerName, params)
	if err != nil {
		slog.WarnContext(ctx.Context(), "Failed to resolve rate limit delegate for provider; skipping",
			"provider", providerName,
			"error", err)
		return policy.ContinueRequest()
	}

	return delegate.OnRequest(ctx, params)
}

func (p *TokenBasedRateLimitPolicy) OnResponse(
	ctx *policy.ResponseContext,
	params map[string]interface{},
) policy.ResponseAction {
	providerName, ok := ctx.SharedContext.Metadata[MetadataKeyProviderName].(string)
	if !ok || providerName == "" {
		return nil
	}

	// We don't use resolveDelegate here because OnResponse should only happen
	// if OnRequest succeeded and found a provider.
	if delegate, ok := p.delegates.Load(providerName); ok {
		return delegate.(policy.Policy).OnResponse(ctx, params)
	}

	return nil
}

// transformToRatelimitParams converts LLM token limits into advanced-ratelimit quotas.
func transformToRatelimitParams(params map[string]interface{}, template map[string]interface{}) map[string]interface{} {
	var quotas []interface{}

	// Helper to add a quota
	addQuota := func(name string, limitsKey string, jsonPathKey string) {
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

		// Resolve JSON path from template if available
		// Expected template structure: spec.usage.prompt_tokens, etc.
		if template != nil {
			if spec, ok := template["spec"].(map[string]interface{}); ok {
				if usage, ok := spec["usage"].(map[string]interface{}); ok {
					if path, ok := usage[jsonPathKey].(string); ok {
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

	addQuota("prompt_tokens", "promptTokenLimits", "prompt_tokens")
	addQuota("completion_tokens", "completionTokenLimits", "completion_tokens")
	addQuota("total_tokens", "totalTokenLimits", "total_tokens")

	rlParams := map[string]interface{}{
		"quotas": quotas,
	}

	// Pass through system parameters
	for _, key := range []string{"algorithm", "backend", "redis", "memory"} {
		if val, ok := params[key]; ok {
			rlParams[key] = val
		}
	}

	return rlParams
}

// convertLimits transforms our {count, duration} schema to advanced-ratelimit's {limit, duration}
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
