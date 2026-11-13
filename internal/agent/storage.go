package agent

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

// Volumes, images and clean-up, from one `docker system df -v`: what each takes up, what uses it, and
// what nothing uses (so clean-up can show what it would free before deleting anything).

type diskUsage struct {
	Images []struct {
		ID, Repository, Tag, Size, UniqueSize, Containers, CreatedSince string
	}
	Containers []struct{ Names, Mounts, Labels string }
	Volumes    []struct{ Name, Driver, Links, Size, Labels string }
	BuildCache []struct {
		Size   string
		InUse  string // "true" or "false"
		Shared string
	}
}

// `docker system df -v` measures every volume and image, which takes a while on a server with big ones (a
// game world, a photo library): tens of seconds. So the last measurement is kept and given at once, and
// measured again in the background once it's a minute old; after clean-up, the next one waits for a fresh
// measurement. The first after the agent starts is made straight away (warmDiskUsage).
var duCache struct {
	sync.Mutex
	du         *diskUsage
	at         time.Time
	refreshing bool
	stale      bool
}

const (
	duFresh = time.Minute      // younger than this is given as it is
	duOld   = 15 * time.Minute // older than this is measured again before answering
)

func readDiskUsage(ctx context.Context) (*diskUsage, error) {
	duCache.Lock()
	du, age, stale := duCache.du, time.Since(duCache.at), duCache.stale
	if du != nil && !stale && age < duOld {
		if age > duFresh && !duCache.refreshing {
			duCache.refreshing = true
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				_, _ = measureDiskUsage(ctx)
			}()
		}
		duCache.Unlock()
		return du, nil
	}
	duCache.Unlock()
	return measureDiskUsage(ctx)
}

// forgetDiskUsage makes the next reading a fresh one: after clean-up, what was there has changed.
func forgetDiskUsage() {
	duCache.Lock()
	duCache.stale = true
	duCache.Unlock()
}

// warmDiskUsage measures once when the agent starts, so the first look at volumes is quick too.
func warmDiskUsage() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, _ = measureDiskUsage(ctx)
}

func measureDiskUsage(ctx context.Context) (*diskUsage, error) {
	du, err := runDiskUsage(ctx)
	duCache.Lock()
	defer duCache.Unlock()
	duCache.refreshing = false
	if err != nil {
		return nil, err
	}
	duCache.du, duCache.at, duCache.stale = du, time.Now(), false
	return du, nil
}

func runDiskUsage(ctx context.Context) (*diskUsage, error) {
	out, err := docker(ctx, "", "system", "df", "-v", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	// Docker's versions differ in how they write some values (InUse is true in some, "true" in others; a
	// count may be 1 or "1"), so every value is made text before reading.
	var raw any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	normal, _ := json.Marshal(asText(raw))
	var du diskUsage
	return &du, json.Unmarshal(normal, &du)
}

// asText turns every number, true or false in a JSON value into text, leaving its shape alone.
func asText(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, e := range v {
			v[k] = asText(e)
		}
		return v
	case []any:
		for i, e := range v {
			v[i] = asText(e)
		}
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return v
}

// parseSize reads Docker's sizes: "12.2MB", "0B", "1.5GB" in powers of 1000, and docker stats's "1.2GiB".
func parseSize(s string) uint64 {
	units := []struct {
		suffix string
		mult   float64
	}{{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3}, {"B", 1}}
	for _, u := range units {
		if n, ok := strings.CutSuffix(s, u.suffix); ok {
			f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
			if err != nil {
				return 0
			}
			return uint64(f * u.mult)
		}
	}
	return 0
}

func labelOf(labels, key string) string {
	for _, kv := range strings.Split(labels, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}

func listVolumes(ctx context.Context) ([]protocol.Volume, error) {
	du, err := readDiskUsage(ctx)
	if err != nil {
		return nil, err
	}
	usedBy := map[string][]string{}
	for _, c := range du.Containers {
		for _, m := range strings.Split(c.Mounts, ",") {
			if m = strings.TrimSpace(m); m != "" {
				usedBy[m] = append(usedBy[m], c.Names)
			}
		}
	}
	volumes := make([]protocol.Volume, 0, len(du.Volumes))
	for _, v := range du.Volumes {
		links, _ := strconv.Atoi(v.Links)
		volumes = append(volumes, protocol.Volume{
			Name: v.Name, Driver: v.Driver, Size: parseSize(v.Size), Containers: links,
			UsedBy: usedBy[v.Name], Stack: labelOf(v.Labels, "com.docker.compose.project"),
		})
	}
	return volumes, nil
}

func listImages(ctx context.Context) ([]protocol.Image, error) {
	du, err := readDiskUsage(ctx)
	if err != nil {
		return nil, err
	}
	images := make([]protocol.Image, 0, len(du.Images))
	for _, i := range du.Images {
		containers, _ := strconv.Atoi(i.Containers)
		images = append(images, protocol.Image{
			ID: strings.TrimPrefix(i.ID, "sha256:")[:12], Repository: i.Repository, Tag: i.Tag,
			Size: parseSize(i.Size), UniqueSize: parseSize(i.UniqueSize), Containers: containers, Created: i.CreatedSince,
		})
	}
	return images, nil
}

// cleanupPreview is what clean-up would remove, and how much each part frees.
func cleanupPreview(ctx context.Context) (protocol.Cleanup, error) {
	du, err := readDiskUsage(ctx)
	if err != nil {
		return protocol.Cleanup{}, err
	}
	var c protocol.Cleanup
	for _, i := range du.Images {
		if i.Containers == "0" {
			name := i.Repository + ":" + i.Tag
			if i.Repository == "<none>" {
				name = strings.TrimPrefix(i.ID, "sha256:")[:12] + " (untagged)"
			}
			c.Images = append(c.Images, protocol.CleanupItem{Name: name, Size: parseSize(i.Size)})
			c.ImagesSize += parseSize(i.UniqueSize)
		}
	}
	for _, v := range du.Volumes {
		if v.Links == "0" {
			c.Volumes = append(c.Volumes, protocol.CleanupItem{Name: v.Name, Size: parseSize(v.Size)})
			c.VolumesSize += parseSize(v.Size)
		}
	}
	for _, b := range du.BuildCache {
		if b.InUse != "true" && b.Shared != "true" {
			c.BuildCacheSize += parseSize(b.Size)
		}
	}
	return c, nil
}

// cleanup removes what nothing uses: images, and if asked volumes (which may hold data) and build cache.
func cleanup(ctx context.Context, args protocol.CleanupArgs) (protocol.ActionResult, error) {
	defer forgetDiskUsage() // what's there has changed
	var out []string
	run := func(cmd ...string) error {
		text, err := docker(ctx, "", cmd...)
		out = append(out, text)
		return err
	}
	if args.Images {
		if err := run("image", "prune", "--all", "--force"); err != nil {
			return protocol.ActionResult{Output: strings.Join(out, "\n")}, err
		}
	}
	if args.Volumes {
		if err := run("volume", "prune", "--all", "--force"); err != nil {
			return protocol.ActionResult{Output: strings.Join(out, "\n")}, err
		}
	}
	if args.BuildCache {
		if err := run("builder", "prune", "--force"); err != nil {
			return protocol.ActionResult{Output: strings.Join(out, "\n")}, err
		}
	}
	return protocol.ActionResult{Output: strings.TrimSpace(strings.Join(out, "\n"))}, nil
}
