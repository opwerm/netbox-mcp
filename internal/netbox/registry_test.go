package netbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The dotted names are derived by de-pluralising an endpoint, which is a
// guess. These are the shapes NetBox actually uses, including the ones a naive
// "strip the s" gets wrong.
func TestSingular(t *testing.T) {
	cases := map[string]string{
		"devices":                "device",
		"device-types":           "devicetype",
		"ip-addresses":           "ipaddress",
		"ip-ranges":              "iprange",
		"vlans":                  "vlan",
		"prefixes":               "prefix",
		"racks":                  "rack",
		"console-ports":          "consoleport",
		"power-feeds":            "powerfeed",
		"object-changes":         "objectchange",
		"virtual-machines":       "virtualmachine",
		"circuit-groups":         "circuitgroup",
		"fhrp-group-assignments": "fhrpgroupassignment",
	}

	for endpoint, want := range cases {
		if got := singular(endpoint); got != want {
			t.Errorf("singular(%q) = %q, want %q", endpoint, got, want)
		}
	}
}

func netboxStub(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dcim": "x", "ipam": "x", "users": "x",
			})
		case "/api/dcim/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"devices": "x", "device-types": "x", "racks": "x",
			})
		case "/api/ipam/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ip-addresses": "x", "prefixes": "x",
			})
		case "/api/users/":
			t.Error("the users app was indexed; it must never be reachable")
			_ = json.NewEncoder(w).Encode(map[string]any{"tokens": "x"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

// The registry is read from the instance rather than hardcoded, so the thing
// worth testing is that discovery produces the mapping a caller will use.
func TestRegistryDiscovers(t *testing.T) {
	srv := netboxStub(t)

	r := &Registry{}
	if err := r.Load(context.Background(), New(srv.URL, "t")); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, tc := range []struct{ objectType, want string }{
		{"dcim.device", "/dcim/devices/"},
		{"dcim.devicetype", "/dcim/device-types/"},
		{"ipam.ipaddress", "/ipam/ip-addresses/"},
		{"ipam.prefix", "/ipam/prefixes/"},
		{"dcim/devices", "/dcim/devices/"},  // endpoint form
		{"DCIM.Device", "/dcim/devices/"},   // case
		{" dcim.device ", "/dcim/devices/"}, // whitespace
	} {
		got, err := r.Path(tc.objectType)
		if err != nil {
			t.Errorf("Path(%q): %v", tc.objectType, err)
			continue
		}

		if got != tc.want {
			t.Errorf("Path(%q) = %q, want %q", tc.objectType, got, tc.want)
		}
	}
}

// Credentials are not infrastructure. The users app must not be reachable even
// for reading -- the stub fails the test if it is indexed at all.
func TestRegistrySkipsUsers(t *testing.T) {
	srv := netboxStub(t)

	r := &Registry{}
	if err := r.Load(context.Background(), New(srv.URL, "t")); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, bad := range []string{"users.token", "users/tokens", "users.user"} {
		if _, err := r.Path(bad); err == nil {
			t.Errorf("Path(%q) resolved; the users app must not be reachable", bad)
		}
	}
}

// A wrong guess should cost one call, not a listing of every type.
func TestUnknownTypeSuggests(t *testing.T) {
	srv := netboxStub(t)

	r := &Registry{}
	_ = r.Load(context.Background(), New(srv.URL, "t"))

	_, err := r.Path("dcim.devices")
	if err == nil {
		t.Fatal("expected an error for a plural type")
	}

	if !contains(err.Error(), "dcim.device") {
		t.Errorf("error %q does not suggest the right name", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}

	return -1
}
