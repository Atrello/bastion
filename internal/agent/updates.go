package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Image updates: for each running container's image, the registry's current digest (asked with a HEAD
// request, which Docker Hub doesn't count against pull limits) compared with the image the server has.
// Images pinned by digest never update; images on registries that need signing in are skipped.

type updates struct {
	mu      sync.Mutex
	newer   map[string]bool // image reference → the registry has a newer one
	checked time.Time
}

var imageUpdates = &updates{newer: map[string]bool{}}

func (u *updates) available(image string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.newer[image]
}

// check asks the registries about every image in use. It's run every 6 hours, and when asked.
func (u *updates) check(ctx context.Context, images []string) {
	client := &http.Client{Timeout: 20 * time.Second}
	found := map[string]bool{}
	for _, image := range images {
		if _, done := found[image]; done || strings.Contains(image, "@") {
			continue
		}
		remote, err := remoteDigest(ctx, client, image)
		if err != nil {
			continue
		}
		local, err := docker(ctx, "", "image", "inspect", "--format", "{{json .RepoDigests}}", image)
		if err != nil {
			continue
		}
		var digests []string
		_ = json.Unmarshal([]byte(local), &digests)
		current := false
		for _, d := range digests {
			if strings.HasSuffix(d, "@"+remote) {
				current = true
			}
		}
		found[image] = !current && len(digests) > 0
	}
	u.mu.Lock()
	u.newer, u.checked = found, time.Now()
	u.mu.Unlock()
}

var refPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:-]*$`)

// parseRef splits an image reference into its registry, repository and tag.
func parseRef(image string) (registry, repo, tag string, err error) {
	if !refPattern.MatchString(image) {
		return "", "", "", errors.New("not an image reference")
	}
	repo, tag = image, "latest"
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo, tag = repo[:i], repo[i+1:]
	}
	registry = "registry-1.docker.io"
	if first, rest, ok := strings.Cut(repo, "/"); ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		registry, repo = first, rest
	}
	if registry == "docker.io" {
		registry = "registry-1.docker.io"
	}
	if registry == "registry-1.docker.io" && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	return registry, repo, tag, nil
}

const manifestTypes = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json"

// remoteDigest asks the image's registry for the digest its tag points at now, with an anonymous token
// if the registry wants one.
func remoteDigest(ctx context.Context, client *http.Client, image string) (string, error) {
	registry, repo, tag, err := parseRef(image)
	if err != nil {
		return "", err
	}
	manifest := "https://" + registry + "/v2/" + repo + "/manifests/" + url.PathEscape(tag)
	head := func(token string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodHead, manifest, nil)
		req.Header.Set("Accept", manifestTypes)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return client.Do(req)
	}
	res, err := head("")
	if err != nil {
		return "", err
	}
	res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		token, err := anonymousToken(ctx, client, res.Header.Get("WWW-Authenticate"))
		if err != nil {
			return "", err
		}
		if res, err = head(token); err != nil {
			return "", err
		}
		res.Body.Close()
	}
	digest := res.Header.Get("Docker-Content-Digest")
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(digest, "sha256:") {
		return "", errors.New("the registry didn't say")
	}
	return digest, nil
}

// anonymousToken follows a registry's "Bearer realm=...,service=...,scope=..." challenge.
func anonymousToken(ctx context.Context, client *http.Client, challenge string) (string, error) {
	params, ok := strings.CutPrefix(challenge, "Bearer ")
	if !ok {
		return "", errors.New("the registry wants signing in")
	}
	values := url.Values{}
	var realm string
	for _, part := range regexp.MustCompile(`(\w+)="([^"]*)"`).FindAllStringSubmatch(params, -1) {
		if part[1] == "realm" {
			realm = part[2]
		} else {
			values.Set(part[1], part[2])
		}
	}
	if !strings.HasPrefix(realm, "https://") {
		return "", errors.New("the registry's sign-in isn't HTTPS")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, realm+"?"+values.Encode(), nil)
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, res.Body, 64<<10)).Decode(&body); err != nil || res.StatusCode != http.StatusOK {
		return "", errors.New("the registry wants signing in")
	}
	if body.Token != "" {
		return body.Token, nil
	}
	return body.AccessToken, nil
}
