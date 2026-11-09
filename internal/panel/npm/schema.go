package npm

// NPM and its forks (NPMplus) refuse any field they don't know, and their fields differ between them and
// between their versions. So Bastion reads the fields an NPM accepts from its own published schema
// (/api/schema), and sends it only those: the web app's forms fill in every field any of them use.

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// fields is what a request to one of NPM's paths may carry: its own fields, and those inside its meta
// object and its custom locations (nil where the schema doesn't limit them).
type fields struct {
	top, meta, location map[string]bool
}

type schemaCache struct {
	paths   map[string]map[string]fields // path ("/nginx/proxy-hosts/{hostID}") → method → fields
	fetched time.Time
}

// Prune takes out of a request's body any field this NPM's schema doesn't allow for it. If the schema
// can't be read, the body is sent as it is.
func (c *Client) Prune(ctx context.Context, method, path string, body map[string]any) {
	f, ok := c.fieldsFor(ctx, method, path)
	if !ok {
		return
	}
	prune(body, f.top)
	if meta, isMap := body["meta"].(map[string]any); isMap {
		prune(meta, f.meta)
	}
	if locations, isList := body["locations"].([]any); isList {
		for _, l := range locations {
			if loc, isMap := l.(map[string]any); isMap {
				prune(loc, f.location)
			}
		}
	}
}

func prune(m map[string]any, allowed map[string]bool) {
	if allowed == nil {
		return
	}
	for k := range m {
		if !allowed[k] {
			delete(m, k)
		}
	}
}

func (c *Client) fieldsFor(ctx context.Context, method, path string) (fields, bool) {
	c.mu.Lock()
	cache := c.schema
	c.mu.Unlock()
	if cache == nil || time.Since(cache.fetched) > time.Hour {
		cache = c.fetchSchema(ctx)
		if cache == nil {
			return fields{}, false
		}
		c.mu.Lock()
		c.schema = cache
		c.mu.Unlock()
	}
	// "/api/nginx/proxy-hosts/12" matches "/nginx/proxy-hosts/{hostID}".
	want := strings.Split(strings.TrimPrefix(strings.SplitN(path, "?", 2)[0], "/api"), "/")
	for p, methods := range cache.paths {
		got := strings.Split(p, "/")
		if len(got) != len(want) {
			continue
		}
		match := true
		for i := range got {
			if got[i] != want[i] && !strings.HasPrefix(got[i], "{") {
				match = false
				break
			}
		}
		if f, found := methods[strings.ToLower(method)]; match && found {
			return f, true
		}
	}
	return fields{}, false
}

func (c *Client) fetchSchema(ctx context.Context) *schemaCache {
	var doc struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema jsonSchema `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
	}
	if err := c.do(ctx, "", http.MethodGet, "/api/schema", nil, "", &doc); err != nil || len(doc.Paths) == 0 {
		return nil
	}
	cache := &schemaCache{paths: map[string]map[string]fields{}, fetched: time.Now()}
	for p, methods := range doc.Paths {
		for m, op := range methods {
			s, ok := op.RequestBody.Content["application/json"]
			if !ok || len(s.Schema.Properties) == 0 {
				continue
			}
			f := fields{top: names(s.Schema.Properties)}
			if meta, ok := s.Schema.Properties["meta"]; ok && len(meta.Properties) > 0 {
				f.meta = names(meta.Properties)
			}
			if loc, ok := s.Schema.Properties["locations"]; ok && loc.Items != nil && len(loc.Items.Properties) > 0 {
				f.location = names(loc.Items.Properties)
			}
			if cache.paths[p] == nil {
				cache.paths[p] = map[string]fields{}
			}
			cache.paths[p][m] = f
		}
	}
	return cache
}

// jsonSchema is the part of a JSON schema needed here: an object's fields, and an array's items.
type jsonSchema struct {
	Properties map[string]jsonSchema `json:"properties"`
	Items      *jsonSchema           `json:"items"`
}

func names(props map[string]jsonSchema) map[string]bool {
	set := make(map[string]bool, len(props))
	for k := range props {
		set[k] = true
	}
	return set
}
