package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		remoteAddr     string
		trustedProxies []netip.Prefix
		forwardedFor   []string
		want           string
	}{
		{
			name:         "direct connection ignores spoofed forwarded header",
			remoteAddr:   "203.0.113.20:4321",
			forwardedFor: []string{"198.51.100.10"},
			want:         "203.0.113.20",
		},
		{
			name:           "trusted proxy uses forwarded client",
			remoteAddr:     "127.0.0.1:8080",
			trustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
			forwardedFor:   []string{"198.51.100.10"},
			want:           "198.51.100.10",
		},
		{
			name:       "trusted proxy chain skips trusted addresses from right",
			remoteAddr: "10.0.0.9:8080",
			trustedProxies: []netip.Prefix{
				netip.MustParsePrefix("10.0.0.0/24"),
			},
			forwardedFor: []string{"198.51.100.11, 10.0.0.7"},
			want:         "198.51.100.11",
		},
		{
			name:       "multiple forwarded headers are treated as one list",
			remoteAddr: "10.0.0.9:8080",
			trustedProxies: []netip.Prefix{
				netip.MustParsePrefix("10.0.0.0/24"),
			},
			forwardedFor: []string{"198.51.100.12", "10.0.0.7"},
			want:         "198.51.100.12",
		},
		{
			name:         "ipv6 direct connection",
			remoteAddr:   "[2001:db8::10]:443",
			forwardedFor: []string{"198.51.100.10"},
			want:         "2001:db8::10",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api := &API{trustedProxies: tt.trustedProxies}
			request := httptest.NewRequest("GET", "http://registry.local/healthz", nil)
			request.RemoteAddr = tt.remoteAddr
			for _, value := range tt.forwardedFor {
				request.Header.Add("X-Forwarded-For", value)
			}

			if got := api.clientIP(request); got != tt.want {
				t.Fatalf("client IP = %q, want %q", got, tt.want)
			}
		})
	}
}
