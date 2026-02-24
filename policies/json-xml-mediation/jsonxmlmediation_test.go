package jsonxmlmediation

import (
	"encoding/json"
	"strings"
	"testing"

	policy "github.com/wso2/api-platform/sdk/gateway/policy/v1alpha"
)

func createHeaders(key, value string) *policy.Headers {
	h := map[string][]string{}
	h[key] = []string{value}
	return policy.NewHeaders(h)
}

func parseErrorJSON(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("failed to unmarshal error body: %v", err)
	}
	return out
}

func TestGetPolicy(t *testing.T) {
	p1, err := GetPolicy(policy.PolicyMetadata{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p2, err := GetPolicy(policy.PolicyMetadata{}, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("expected singleton instance")
	}
}

func TestMode(t *testing.T) {
	p := &JSONXMLMediationPolicy{}
	mode := p.Mode()
	expected := policy.ProcessingMode{
		RequestHeaderMode:  policy.HeaderModeProcess,
		RequestBodyMode:    policy.BodyModeBuffer,
		ResponseHeaderMode: policy.HeaderModeProcess,
		ResponseBodyMode:   policy.BodyModeBuffer,
	}
	if mode != expected {
		t.Fatalf("unexpected mode: %+v", mode)
	}
}

func TestOnRequest_JSONToXML_Success(t *testing.T) {
	p := &JSONXMLMediationPolicy{}
	ctx := &policy.RequestContext{
		Body:    &policy.Body{Content: []byte(`{"name":"John","age":30}`), Present: true},
		Headers: createHeaders("content-type", "application/json"),
	}

	result := p.OnRequest(ctx, map[string]interface{}{"upstreamFormat": "xml"})
	mods, ok := result.(policy.UpstreamRequestModifications)
	if !ok {
		t.Fatalf("expected UpstreamRequestModifications, got %T", result)
	}
	if mods.Body == nil || !strings.Contains(string(mods.Body), "<name>John</name>") {
		t.Fatalf("expected transformed XML body, got: %s", string(mods.Body))
	}
	if mods.SetHeaders["content-type"] != "application/xml" {
		t.Fatalf("unexpected content-type: %s", mods.SetHeaders["content-type"])
	}
	if mods.SetHeaders["content-length"] == "" {
		t.Fatalf("expected content-length header")
	}
}

func TestOnRequest_XMLToJSON_Success(t *testing.T) {
	p := &JSONXMLMediationPolicy{}
	ctx := &policy.RequestContext{
		Body:    &policy.Body{Content: []byte(`<root><name>John</name><age>30</age></root>`), Present: true},
		Headers: createHeaders("content-type", "text/xml; charset=utf-8"),
	}

	result := p.OnRequest(ctx, map[string]interface{}{"upstreamFormat": "json"})
	mods, ok := result.(policy.UpstreamRequestModifications)
	if !ok {
		t.Fatalf("expected UpstreamRequestModifications, got %T", result)
	}
	if mods.Body == nil {
		t.Fatalf("expected transformed JSON body")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(mods.Body, &parsed); err != nil {
		t.Fatalf("expected valid JSON output, got error: %v", err)
	}
	if mods.SetHeaders["content-type"] != "application/json" {
		t.Fatalf("unexpected content-type: %s", mods.SetHeaders["content-type"])
	}
}

func TestOnResponse_Reverse_JSONToXML_RequestMeans_XMLToJSON_Response(t *testing.T) {
	p := &JSONXMLMediationPolicy{}
	ctx := &policy.ResponseContext{
		ResponseBody:    &policy.Body{Content: []byte(`<root><status>ok</status></root>`), Present: true},
		ResponseHeaders: createHeaders("content-type", "application/xml"),
	}

	result := p.OnResponse(ctx, map[string]interface{}{"upstreamFormat": "xml"})
	mods, ok := result.(policy.UpstreamResponseModifications)
	if !ok {
		t.Fatalf("expected UpstreamResponseModifications, got %T", result)
	}
	if mods.Body == nil {
		t.Fatalf("expected transformed JSON response body")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(mods.Body, &parsed); err != nil {
		t.Fatalf("expected valid JSON output, got error: %v", err)
	}
	if mods.SetHeaders["content-type"] != "application/json" {
		t.Fatalf("unexpected content-type: %s", mods.SetHeaders["content-type"])
	}
}

func TestOnResponse_Reverse_XMLToJSON_RequestMeans_JSONToXML_Response(t *testing.T) {
	p := &JSONXMLMediationPolicy{}
	ctx := &policy.ResponseContext{
		ResponseBody:    &policy.Body{Content: []byte(`{"status":"ok"}`), Present: true},
		ResponseHeaders: createHeaders("content-type", "application/json; charset=utf-8"),
	}

	result := p.OnResponse(ctx, map[string]interface{}{"upstreamFormat": "json"})
	mods, ok := result.(policy.UpstreamResponseModifications)
	if !ok {
		t.Fatalf("expected UpstreamResponseModifications, got %T", result)
	}
	if mods.Body == nil || !strings.Contains(string(mods.Body), "<status>ok</status>") {
		t.Fatalf("expected transformed XML response body, got: %s", string(mods.Body))
	}
	if mods.SetHeaders["content-type"] != "application/xml" {
		t.Fatalf("unexpected content-type: %s", mods.SetHeaders["content-type"])
	}
}

func TestInvalidUpstreamFormatConfig(t *testing.T) {
	p := &JSONXMLMediationPolicy{}
	ctxReq := &policy.RequestContext{Body: &policy.Body{Content: []byte(`{"x":1}`), Present: true}, Headers: createHeaders("content-type", "application/json")}
	ctxResp := &policy.ResponseContext{ResponseBody: &policy.Body{Content: []byte(`<x>1</x>`), Present: true}, ResponseHeaders: createHeaders("content-type", "application/xml")}

	cases := []map[string]interface{}{
		{},
		{"upstreamFormat": ""},
		{"upstreamFormat": "invalid"},
		{"upstreamFormat": true},
	}

	for i, params := range cases {
		reqResult := p.OnRequest(ctxReq, params)
		immediate, ok := reqResult.(policy.ImmediateResponse)
		if !ok {
			t.Fatalf("case %d: expected ImmediateResponse, got %T", i, reqResult)
		}
		if immediate.StatusCode != 500 {
			t.Fatalf("case %d: expected status 500, got %d", i, immediate.StatusCode)
		}

		respResult := p.OnResponse(ctxResp, params)
		respMods, ok := respResult.(policy.UpstreamResponseModifications)
		if !ok {
			t.Fatalf("case %d: expected UpstreamResponseModifications, got %T", i, respResult)
		}
		if respMods.StatusCode == nil || *respMods.StatusCode != 500 {
			t.Fatalf("case %d: expected response status 500, got %#v", i, respMods.StatusCode)
		}
	}
}

func TestOnRequest_ContentTypeAndPayloadErrors(t *testing.T) {
	p := &JSONXMLMediationPolicy{}

	wrongTypeCtx := &policy.RequestContext{
		Body:    &policy.Body{Content: []byte(`{"name":"x"}`), Present: true},
		Headers: createHeaders("content-type", "application/xml"),
	}
	res := p.OnRequest(wrongTypeCtx, map[string]interface{}{"upstreamFormat": "xml"})
	immediate, ok := res.(policy.ImmediateResponse)
	if !ok || immediate.StatusCode != 500 {
		t.Fatalf("expected 500 ImmediateResponse for wrong content type, got %T %#v", res, res)
	}

	invalidJSONCtx := &policy.RequestContext{
		Body:    &policy.Body{Content: []byte(`{"name":`), Present: true},
		Headers: createHeaders("content-type", "application/json"),
	}
	res = p.OnRequest(invalidJSONCtx, map[string]interface{}{"upstreamFormat": "xml"})
	immediate, ok = res.(policy.ImmediateResponse)
	if !ok || immediate.StatusCode != 500 {
		t.Fatalf("expected 500 ImmediateResponse for invalid JSON, got %T %#v", res, res)
	}

	invalidXMLCtx := &policy.RequestContext{
		Body:    &policy.Body{Content: []byte(`<root><name>x</root>`), Present: true},
		Headers: createHeaders("content-type", "application/xml"),
	}
	res = p.OnRequest(invalidXMLCtx, map[string]interface{}{"upstreamFormat": "json"})
	immediate, ok = res.(policy.ImmediateResponse)
	if !ok || immediate.StatusCode != 500 {
		t.Fatalf("expected 500 ImmediateResponse for invalid XML, got %T %#v", res, res)
	}

	errBody := parseErrorJSON(t, immediate.Body)
	if errBody["error"] != "Internal Server Error" {
		t.Fatalf("expected internal server error body, got %#v", errBody)
	}
}

func TestOnResponse_ContentTypeAndPayloadErrors(t *testing.T) {
	p := &JSONXMLMediationPolicy{}

	wrongTypeCtx := &policy.ResponseContext{
		ResponseBody:    &policy.Body{Content: []byte(`<root/>`), Present: true},
		ResponseHeaders: createHeaders("content-type", "application/json"),
	}
	res := p.OnResponse(wrongTypeCtx, map[string]interface{}{"upstreamFormat": "xml"})
	mods, ok := res.(policy.UpstreamResponseModifications)
	if !ok || mods.StatusCode == nil || *mods.StatusCode != 500 {
		t.Fatalf("expected 500 UpstreamResponseModifications for wrong content type, got %T %#v", res, res)
	}

	invalidJSONCtx := &policy.ResponseContext{
		ResponseBody:    &policy.Body{Content: []byte(`{"x":`), Present: true},
		ResponseHeaders: createHeaders("content-type", "application/json"),
	}
	res = p.OnResponse(invalidJSONCtx, map[string]interface{}{"upstreamFormat": "json"})
	mods, ok = res.(policy.UpstreamResponseModifications)
	if !ok || mods.StatusCode == nil || *mods.StatusCode != 500 {
		t.Fatalf("expected 500 UpstreamResponseModifications for invalid JSON, got %T %#v", res, res)
	}

	invalidXMLCtx := &policy.ResponseContext{
		ResponseBody:    &policy.Body{Content: []byte(`<root><x></root>`), Present: true},
		ResponseHeaders: createHeaders("content-type", "application/xml"),
	}
	res = p.OnResponse(invalidXMLCtx, map[string]interface{}{"upstreamFormat": "xml"})
	mods, ok = res.(policy.UpstreamResponseModifications)
	if !ok || mods.StatusCode == nil || *mods.StatusCode != 500 {
		t.Fatalf("expected 500 UpstreamResponseModifications for invalid XML, got %T %#v", res, res)
	}

	errBody := parseErrorJSON(t, mods.Body)
	if errBody["error"] != "Internal Server Error" {
		t.Fatalf("expected internal server error body, got %#v", errBody)
	}
}

func TestNoBodyPassThrough(t *testing.T) {
	p := &JSONXMLMediationPolicy{}

	reqCtx := &policy.RequestContext{
		Body:    &policy.Body{Content: []byte{}, Present: false},
		Headers: createHeaders("content-type", "application/json"),
	}
	reqResult := p.OnRequest(reqCtx, map[string]interface{}{"upstreamFormat": "xml"})
	reqMods, ok := reqResult.(policy.UpstreamRequestModifications)
	if !ok {
		t.Fatalf("expected UpstreamRequestModifications, got %T", reqResult)
	}
	if reqMods.Body != nil {
		t.Fatalf("expected nil body for request pass-through, got %s", string(reqMods.Body))
	}

	respCtx := &policy.ResponseContext{
		ResponseBody:    &policy.Body{Content: []byte{}, Present: false},
		ResponseHeaders: createHeaders("content-type", "application/xml"),
	}
	respResult := p.OnResponse(respCtx, map[string]interface{}{"upstreamFormat": "xml"})
	respMods, ok := respResult.(policy.UpstreamResponseModifications)
	if !ok {
		t.Fatalf("expected UpstreamResponseModifications, got %T", respResult)
	}
	if respMods.Body != nil {
		t.Fatalf("expected nil body for response pass-through, got %s", string(respMods.Body))
	}
}

func TestConversionHelpers(t *testing.T) {
	p := &JSONXMLMediationPolicy{}

	xmlData, err := p.convertJSONBytesToXML([]byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("convertJSONBytesToXML failed: %v", err)
	}
	if !strings.Contains(string(xmlData), "<a>1</a>") {
		t.Fatalf("unexpected XML: %s", xmlData)
	}

	jsonData, err := p.convertXMLToJSON([]byte(`<root><a>1</a></root>`))
	if err != nil {
		t.Fatalf("convertXMLToJSON failed: %v", err)
	}
	if !json.Valid(jsonData) {
		t.Fatalf("expected valid JSON output: %s", jsonData)
	}
}
