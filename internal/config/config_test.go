package config

import (
	"net/netip"
	"testing"
)

func TestProxyPrefixesEnv(t *testing.T) {
	t.Setenv("TYPE_REGISTRY_TEST_TRUSTED_PROXIES", "127.0.0.1/32, 10.0.0.5, 2001:db8::/32")

	got, err := proxyPrefixesEnv("TYPE_REGISTRY_TEST_TRUSTED_PROXIES")
	if err != nil {
		t.Fatalf("proxy prefixes: %v", err)
	}

	want := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("10.0.0.5/32"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	if len(got) != len(want) {
		t.Fatalf("prefix count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("prefix[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestProxyPrefixesEnvRejectsInvalidValue(t *testing.T) {
	t.Setenv("TYPE_REGISTRY_TEST_TRUSTED_PROXIES", "127.0.0.1/32,not-an-ip")

	if _, err := proxyPrefixesEnv("TYPE_REGISTRY_TEST_TRUSTED_PROXIES"); err == nil {
		t.Fatal("expected invalid trusted proxy error")
	}
}
