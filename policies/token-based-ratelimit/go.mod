module github.com/Tharsanan1/gateway-controllers/policies/token-based-ratelimit

go 1.25.1

require (
	github.com/wso2/api-platform/sdk v0.3.7
	github.com/wso2/gateway-controllers/policies/advanced-ratelimit v0.1.1
)

replace github.com/wso2/api-platform/sdk => ../../../api-platform/sdk
replace github.com/wso2/gateway-controllers/policies/advanced-ratelimit => ../advanced-ratelimit
