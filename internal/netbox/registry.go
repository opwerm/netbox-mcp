package netbox

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Registry maps an object type such as "dcim.device" onto the API path that
// serves it, "/dcim/devices/".
//
// It is discovered from NetBox at startup rather than hardcoded. NetBox's API
// root lists its apps and each app lists its endpoints, so the mapping can be
// read from the instance being talked to -- which means it matches that
// instance's version, and picks up whatever plugins it has, instead of going
// stale in a table that has to be updated by hand for every NetBox release.
type Registry struct {
	once  sync.Once
	err   error
	byKey map[string]string // "dcim.device" and "dcim/devices" both map to the path
	types []string          // dotted names, sorted, for listing and for suggestions
	mu    sync.RWMutex
}

// apps NetBox does not serve as data, or that must never be reachable through
// a tool. See the package doc on the server side for why each is excluded.
var skipApps = map[string]bool{
	"users": true, // tokens, permissions: credentials, not infrastructure
}

// Load discovers the registry once. Subsequent calls return the same result.
func (r *Registry) Load(ctx context.Context, c *Client) error {
	r.once.Do(func() { r.err = r.load(ctx, c) })

	return r.err
}

func (r *Registry) load(ctx context.Context, c *Client) error {
	var root map[string]any

	if err := c.do(ctx, http.MethodGet, "/", nil, nil, &root); err != nil {
		return fmt.Errorf("read API root: %w", err)
	}

	byKey := map[string]string{}
	seen := map[string]bool{}

	for app := range root {
		if skipApps[app] {
			continue
		}

		var index map[string]any
		if err := c.do(ctx, http.MethodGet, "/"+app+"/", nil, nil, &index); err != nil {
			// An app can be present in the root and refuse a listing -- a
			// plugin with its own permissions, most often. Skipping it loses
			// that app, not the whole registry.
			continue
		}

		for endpoint := range index {
			path := "/" + app + "/" + endpoint + "/"

			byKey[app+"/"+endpoint] = path

			dotted := app + "." + singular(endpoint)
			if !seen[dotted] {
				seen[dotted] = true
				byKey[dotted] = path
			}
		}
	}

	if len(byKey) == 0 {
		return fmt.Errorf("no endpoints discovered; is this a NetBox API?")
	}

	types := make([]string, 0, len(seen))
	for t := range seen {
		types = append(types, t)
	}

	sort.Strings(types)

	r.mu.Lock()
	r.byKey, r.types = byKey, types
	r.mu.Unlock()

	return nil
}

// Path resolves an object type to its API path.
//
// Both spellings work: the dotted "dcim.device" that NetBox itself uses for
// content types, and the endpoint form "dcim/devices" straight out of a URL.
// The dotted form is derived by de-pluralising, which is a guess; accepting
// the endpoint form as well means a caller is never stuck when the guess is
// wrong for some model.
func (r *Registry) Path(objectType string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := strings.Trim(strings.ToLower(strings.TrimSpace(objectType)), "/")

	if p, ok := r.byKey[key]; ok {
		return p, nil
	}

	return "", fmt.Errorf("unknown object type %q%s", objectType, r.suggest(key))
}

// Types lists the known dotted object types.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]string, len(r.types))
	copy(out, r.types)

	return out
}

// suggest names the near misses, so a wrong guess costs one call rather than a
// listing of six hundred types.
func (r *Registry) suggest(key string) string {
	stem := key
	if i := strings.IndexAny(stem, "./"); i >= 0 {
		stem = stem[i+1:]
	}

	if len(stem) < 3 {
		return ""
	}

	var near []string

	for _, t := range r.types {
		if strings.Contains(t, stem) {
			near = append(near, t)
			if len(near) == 8 {
				break
			}
		}
	}

	if len(near) == 0 {
		return ". Call netbox_object_types to list what this NetBox serves"
	}

	return ". Did you mean: " + strings.Join(near, ", ")
}

// singular turns an endpoint name into the model name NetBox uses in a dotted
// content type: "ip-addresses" becomes "ipaddress", "device-types" becomes
// "devicetype".
func singular(endpoint string) string {
	s := strings.ReplaceAll(endpoint, "-", "")

	switch {
	case strings.HasSuffix(s, "ies"):
		return strings.TrimSuffix(s, "ies") + "y"
	case strings.HasSuffix(s, "sses"), strings.HasSuffix(s, "xes"), strings.HasSuffix(s, "ches"):
		return strings.TrimSuffix(s, "es")
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return strings.TrimSuffix(s, "s")
	}

	return s
}
