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
	slog.Debug("OnRequest: processing token-based rate limit",
		"route", p.metadata.RouteName,
		"params", params)

	providerName, ok := ctx.SharedContext.Metadata[MetadataKeyProviderName].(string)
	if !ok || providerName == "" {
		slog.Debug("OnRequest: provider name not found in metadata; skipping token-based rate limit",
			"route", p.metadata.RouteName)
		return nil
	}

	slog.Debug("OnRequest: resolved provider",
		"route", p.metadata.RouteName,
		"provider", providerName)

	delegate, err := p.resolveDelegate(providerName, params)
	if err != nil {
		slog.Warn("OnRequest: failed to resolve rate limit delegate for provider",
			"route", p.metadata.RouteName,
			"provider", providerName,
			"error", err)
		return nil
	}

	slog.Debug("OnRequest: delegating to advanced-ratelimit",
		"route", p.metadata.RouteName,
		"provider", providerName)

	return delegate.OnRequest(ctx, params)
}

// OnResponse processes the response phase by delegating to the same provider-specific instance.
func (p *TokenBasedRateLimitPolicy) OnResponse(
	ctx *policy.ResponseContext,
	params map[string]interface{},
) policy.ResponseAction {
	slog.Debug("OnResponse: processing token-based rate limit",
		"route", p.metadata.RouteName)

	providerName, ok := ctx.SharedContext.Metadata[MetadataKeyProviderName].(string)
	if !ok || providerName == "" {
		slog.Debug("OnResponse: provider name not found in metadata; skipping",
			"route", p.metadata.RouteName)
		return nil
	}

	slog.Debug("OnResponse: looking up delegate",
		"route", p.metadata.RouteName,
		"provider", providerName)

	if delegate, ok := p.delegates.Load(providerName); ok {
		slog.Debug("OnResponse: delegating to advanced-ratelimit",
			"route", p.metadata.RouteName,
			"provider", providerName)
		return delegate.(policy.Policy).OnResponse(ctx, params)
	}

	slog.Debug("OnResponse: no delegate found for provider",
		"route", p.metadata.RouteName,
		"provider", providerName)

	return nil
}

// resolveDelegate ensures an advanced-ratelimit instance exists for the given provider.
// This method is thread-safe and uses LoadOrStore to prevent race conditions when
// multiple goroutines attempt to create a delegate for the same provider simultaneously.
func (p *TokenBasedRateLimitPolicy) resolveDelegate(providerName string, params map[string]interface{}) (policy.Policy, error) {
	slog.Debug("resolveDelegate: checking for existing delegate",
		"route", p.metadata.RouteName,
		"provider", providerName)

	// Fast path: check if delegate already exists
	if val, ok := p.delegates.Load(providerName); ok {
		slog.Debug("resolveDelegate: found existing delegate (fast path)",
			"route", p.metadata.RouteName,
			"provider", providerName)
		return val.(policy.Policy), nil
	}

	slog.Debug("resolveDelegate: creating new delegate (slow path)",
		"route", p.metadata.RouteName,
		"provider", providerName)

	// Slow path: create the delegate (expensive operation)
	delegate, err := p.createDelegate(providerName, params)
	if err != nil {
		slog.Error("resolveDelegate: failed to create delegate",
			"route", p.metadata.RouteName,
			"provider", providerName,
			"error", err)
		return nil, err
	}

	// Atomically store if not exists, or return the existing one
	// This ensures only one delegate is created per provider even with concurrent access
	if existing, loaded := p.delegates.LoadOrStore(providerName, delegate); loaded {
		// Another goroutine already stored a delegate, use that one
		slog.Debug("resolveDelegate: another goroutine created delegate, using existing",
			"route", p.metadata.RouteName,
			"provider", providerName)
		return existing.(policy.Policy), nil
	}

	slog.Debug("resolveDelegate: successfully created and stored new delegate",
		"route", p.metadata.RouteName,
		"provider", providerName)

	return delegate, nil
}

// createDelegate creates a new advanced-ratelimit delegate for the given provider.
// This involves fetching resources from the store and transforming parameters.
func (p *TokenBasedRateLimitPolicy) createDelegate(providerName string, params map[string]interface{}) (policy.Policy, error) {
	slog.Debug("createDelegate: starting delegate creation",
		"route", p.metadata.RouteName,
		"provider", providerName)

	store := policy.GetLazyResourceStoreInstance()

	// 1. Get Provider-to-Template Mapping
	slog.Debug("createDelegate: fetching provider template mapping",
		"route", p.metadata.RouteName,
		"provider", providerName,
		"resourceType", ResourceTypeProviderTemplateMapping)

	mappingResource, err := store.GetResourceByIDAndType(providerName, ResourceTypeProviderTemplateMapping)
	if err != nil {
		slog.Error("createDelegate: failed to get provider template mapping",
			"route", p.metadata.RouteName,
			"provider", providerName,
			"error", err)
		return nil, err
	}

	if mappingResource == nil {
		slog.Error("createDelegate: provider template mapping not found",
			"route", p.metadata.RouteName,
			"provider", providerName)
		return nil, nil
	}

	templateHandle, ok := mappingResource.Resource["template_handle"].(string)
	if !ok || templateHandle == "" {
		slog.Error("createDelegate: template_handle not found or empty in mapping",
			"route", p.metadata.RouteName,
			"provider", providerName,
			"hasTemplateHandle", ok)
		return nil, nil
	}

	slog.Debug("createDelegate: resolved template handle",
		"route", p.metadata.RouteName,
		"provider", providerName,
		"templateHandle", templateHandle)

	// 2. Get the Actual Template
	slog.Debug("createDelegate: fetching LLM provider template",
		"route", p.metadata.RouteName,
		"provider", providerName,
		"templateHandle", templateHandle,
		"resourceType", ResourceTypeLlmProviderTemplate)

	templateResource, err := store.GetResourceByIDAndType(templateHandle, ResourceTypeLlmProviderTemplate)
	if err != nil {
		slog.Error("createDelegate: failed to get LLM provider template",
			"route", p.metadata.RouteName,
			"provider", providerName,
			"templateHandle", templateHandle,
			"error", err)
		return nil, err
	}

	if templateResource == nil {
		slog.Error("createDelegate: LLM provider template not found",
			"route", p.metadata.RouteName,
			"provider", providerName,
			"templateHandle", templateHandle)
		return nil, nil
	}

	// 3. Transform LLM limits into advanced-ratelimit parameters
	slog.Debug("createDelegate: transforming parameters",
		"route", p.metadata.RouteName,
		"provider", providerName,
		"templateHandle", templateHandle)

	rlParams := transformToRatelimitParams(params, templateResource.Resource)

	slog.Debug("createDelegate: parameters transformed",
		"route", p.metadata.RouteName,
		"provider", providerName,
		"quotasCount", len(rlParams["quotas"].([]interface{})))

	// 4. Create the delegate instance
	slog.Debug("createDelegate: creating advanced-ratelimit policy",
		"route", p.metadata.RouteName,
		"provider", providerName)

	delegate, err := ratelimit.GetPolicy(p.metadata, rlParams)
	if err != nil {
		slog.Error("createDelegate: failed to create advanced-ratelimit policy",
			"route", p.metadata.RouteName,
			"provider", providerName,
			"error", err)
		return nil, err
	}

	slog.Debug("createDelegate: successfully created delegate",
		"route", p.metadata.RouteName,
		"provider", providerName)

	return delegate, nil
}

// transformToRatelimitParams converts LLM-specific parameters to the advanced-ratelimit structure.
func transformToRatelimitParams(params map[string]interface{}, template map[string]interface{}) map[string]interface{} {
	slog.Debug("transformToRatelimitParams: starting parameter transformation",
		"params", params)

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

	slog.Debug("transformToRatelimitParams: completed transformation",
		"quotasCount", len(quotas),
		"hasAlgorithm", rlParams["algorithm"] != nil,
		"hasBackend", rlParams["backend"] != nil)

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
