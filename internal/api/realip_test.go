package api

import "testing"

func TestClientAddrTrustsForwardedForOnlyFromProxies(t *testing.T) {
	trusted, err := parseCIDRs(DefaultTrustedProxies)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, remote, xff, want string }{
		{"direct client, no header", "203.0.113.9:5555", "", "203.0.113.9"},
		{"direct client cannot spoof", "203.0.113.9:5555", "198.51.100.1", "203.0.113.9"},
		{"proxy on loopback", "127.0.0.1:4000", "203.0.113.9", "203.0.113.9"},
		{"docker network proxy", "172.18.0.4:4000", "203.0.113.9", "203.0.113.9"},
		{"client supplied a fake first hop", "127.0.0.1:4000", "1.2.3.4, 203.0.113.9", "203.0.113.9"},
		{"chain of our own proxies", "127.0.0.1:4000", "203.0.113.9, 10.0.0.5, 172.18.0.2", "203.0.113.9"},
		{"malformed header is ignored", "127.0.0.1:4000", "not-an-ip", "127.0.0.1"},
		{"all hops internal", "127.0.0.1:4000", "10.0.0.5", "127.0.0.1"},
		{"ipv6 client", "[::1]:4000", "2001:db8::7", "2001:db8::7"},
	} {
		if got := clientAddr(c.remote, c.xff, trusted); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestParseCIDRsAcceptsBareAddresses(t *testing.T) {
	n, err := parseCIDRs("10.1.2.3, 2001:db8::1")
	if err != nil || len(n) != 2 {
		t.Fatalf("%v %v", n, err)
	}
	if _, err := parseCIDRs("nonsense"); err == nil {
		t.Fatal("must reject garbage")
	}
}
