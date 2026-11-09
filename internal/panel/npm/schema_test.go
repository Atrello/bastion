package npm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Fields this NPM doesn't list in its schema are taken out; the rest are kept, in meta and locations too.
func TestPrune(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"paths":{"/nginx/proxy-hosts/{hostID}":{"put":{"requestBody":{"content":{"application/json":{"schema":{"properties":{
			"domain_names":{},"forward_port":{},"npmplus_http3_support":{},
			"meta":{"properties":{"dns_challenge":{}}},
			"locations":{"items":{"properties":{"path":{},"npmplus_access_list_type":{}}}}}}}}}}}}}`))
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL}
	body := map[string]any{
		"domain_names": []any{"a"}, "forward_port": 80, "npmplus_http3_support": true, "http2_support": true, "caching_enabled": true,
		"meta":      map[string]any{"dns_challenge": false, "letsencrypt_email": "robin@example.com"},
		"locations": []any{map[string]any{"path": "/api", "npmplus_access_list_type": "global", "caching_enabled": true}},
	}
	c.Prune(context.Background(), http.MethodPut, "/api/nginx/proxy-hosts/12", body)
	if _, ok := body["http2_support"]; ok || body["npmplus_http3_support"] != true || body["forward_port"] != 80 {
		t.Fatalf("top level: %v", body)
	}
	if meta := body["meta"].(map[string]any); len(meta) != 1 {
		t.Fatalf("meta: %v", meta)
	}
	if loc := body["locations"].([]any)[0].(map[string]any); len(loc) != 2 {
		t.Fatalf("location: %v", loc)
	}
	// A path the schema doesn't describe is sent as it is.
	other := map[string]any{"anything": 1}
	c.Prune(context.Background(), http.MethodPost, "/api/nginx/streams", other)
	if len(other) != 1 {
		t.Fatalf("unknown path was pruned: %v", other)
	}
}
