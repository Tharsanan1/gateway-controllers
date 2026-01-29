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
	ResourceTypeLlmProviderTemplate     = "LlmProviderTemplate"
	ResourceTypeProviderTemplateMapping = "ProviderTemplateMapping"
	MetadataKeyProviderName             = "provider_name"
)

// TokenBasedRateLimitPolicy delegates LLM token-based rate limiting to advanced-ratelimit
// by dynamically resolving cost extraction paths from provider templates.
type TokenBasedRateLimitPolicy struct {
	metadata  policy.PolicyMetadata
	delegates sync.Map // map[string]policy.Policy (providerName -> advanced-ratelimit instance)
}

// GetPolicy creates and initializes the token-based rate limit policy.
func GetPolicy(
	metadata policy.PolicyMetadata,
	params map[string]interface{},
) (policy.Policy, error) {
	return &TokenBasedRateLimitPolicy{
		metadata: metadata,
	}, nil
}

// Mode returns the processing mode for this policy.
func (p *TokenBasedRateLimitPolicy) Mode() policy.ProcessingMode {
	return policy.ProcessingMode{
		RequestHeaderMode:  policy.HeaderModeProcess,
		RequestBodyMode:    policy.BodyModeSkip,
		ResponseHeaderMode: policy.HeaderModeProcess,
		ResponseBodyMode:   policy.BodyModeBuffer,
	}
}

// OnRequest processes the request phase by delegating to a provider-specific ratelimit instance.
func (p *TokenBasedRateLimitPolicy) OnRequest(
	ctx *policy.RequestContext,
	params map[string]interface{},
) policy.RequestAction {
	providerName, ok := ctx.SharedContext.Metadata[MetadataKeyProviderName].(string)
	if !ok || providerName == "" {
		slog.Debug("Provider name not found in metadata; skipping token-based rate limit")
		return nil
	}

	delegate, err := p.resolveDelegate(providerName, params)
	if err != nil {
		slog.Warn("Failed to resolve rate limit delegate for provider",
			"provider", providerName, "error", err)
		return nil
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
// This method is thread-safe and uses LoadOrStore to prevent race conditions when
// multiple goroutines attempt to create a delegate for the same provider simultaneously.
func (p *TokenBasedRateLimitPolicy) resolveDelegate(providerName string, params map[string]interface{}) (policy.Policy, error) {
	// Fast path: check if delegate already exists
	if val, ok := p.delegates.Load(providerName); ok {
		return val.(policy.Policy), nil
	}

	// Slow path: create the delegate (expensive operation)
	delegate, err := p.createDelegate(providerName, params)
	if err != nil {
		return nil, err
	}

	// Atomically store if not exists, or return the existing one
	// This ensures only one delegate is created per provider even with concurrent access
	if existing, loaded := p.delegates.LoadOrStore(providerName, delegate); loaded {
		// Another goroutine already stored a delegate, use that one
		return existing.(policy.Policy), nil
	}

	return delegate, nil
}

// createDelegate creates a new advanced-ratelimit delegate for the given provider.
// This involves fetching resources from the store and transforming parameters.
func (p *TokenBasedRateLimitPolicy) createDelegate(providerName string, params map[string]interface{}) (policy.Policy, error) {
	store := policy.GetLazyResourceStoreInstance()

	// 1. Get Provider-to-Template Mapping
	mappingResource, err := store.GetResourceByIDAndType(providerName, ResourceTypeProviderTemplateMapping)
	if err != nil {
		return nil, err
	}

	templateHandle, ok := mappingResource.Resource["template_handle"].(string)
	if !ok || templateHandle == "" {
		return nil, err
	}

	// 2. Get the Actual Template
	templateResource, err := store.GetResourceByIDAndType(templateHandle, ResourceTypeLlmProviderTemplate)
	if err != nil {
		return nil, err
	}

	// 3. Transform LLM limits into advanced-ratelimit parameters
	rlParams := transformToRatelimitParams(params, templateResource.Resource)

	// 4. Create the delegate instance
	delegate, err := ratelimit.GetPolicy(p.metadata, rlParams)
	if err != nil {
		return nil, err
	}

	return delegate, nil
}

// transformToRatelimitParams converts LLM-specific parameters to the advanced-ratelimit structure.
func transformToRatelimitParams(params map[string]interface{}, template map[string]interface{}) map[string]interface{} {
	var quotas []interface{}

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

		if template != nil {
			if spec, ok := template["configuration"].(map[string]interface{}); ok {
				if specData, ok := spec["spec"].(map[string]interface{}); ok {
					if usage, ok := specData[templateKey].(map[string]interface{}); ok {
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
		}
		quotas = append(quotas, quota)
	}

	addQuota("prompt_tokens", "promptTokenLimits", "promptTokens")
	addQuota("completion_tokens", "completionTokenLimits", "completionTokens")
	addQuota("total_tokens", "totalTokenLimits", "totalTokens")

	rlParams := map[string]interface{}{
		"quotas": quotas,
	}

	for _, key := range []string{"algorithm", "backend", "redis", "memory"} {
		if val, ok := params[key]; ok {
			rlParams[key] = val
		}
	}

	return rlParams
}

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
