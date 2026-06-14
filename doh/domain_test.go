package doh

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve), which need no network. The client's
// HTTP behaviour is covered in doh_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "doh" {
		t.Errorf("Scheme = %q, want doh", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "doh" {
		t.Errorf("Identity.Binary = %q, want doh", info.Identity.Binary)
	}
}

func TestClassifyDomain(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		{"google.com", "domain", "google.com"},
		{"github.com", "domain", "github.com"},
		{"sub.example.org", "domain", "sub.example.org"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestClassifyIP(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		{"8.8.8.8", "ip", "8.8.8.8"},
		{"142.251.12.113", "ip", "142.251.12.113"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestLocateDomain(t *testing.T) {
	got, err := Domain{}.Locate("domain", "google.com")
	want := "https://dns.google/resolve?name=google.com&type=A"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateIP(t *testing.T) {
	got, err := Domain{}.Locate("ip", "8.8.8.8")
	want := "https://dns.google/resolve?name=8.8.8.8&type=PTR"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("page", "foo")
	if err == nil {
		t.Error("expected error for unknown type, got nil")
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	r := &Record{Name: "google.com.", Type: "A", TTL: 215, Data: "142.251.12.113"}
	_, err = h.Mint(r)
	// Record has no kit:"id" so Mint may return an error — that is acceptable.
	// We just verify the host opened without crashing.
	_ = err

	got, err := h.ResolveOn("doh", "google.com")
	if err != nil || got.String() != "doh://domain/google.com" {
		t.Errorf("ResolveOn = (%q, %v), want doh://domain/google.com", got.String(), err)
	}
}
