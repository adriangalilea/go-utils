package tests

import (
	"net/netip"
	"testing"

	. "github.com/adriangalilea/go-utils" //nolint:staticcheck
)

func TestNetRangeMath(t *testing.T) {
	p := Must(netip.ParsePrefix("192.168.1.0/24"))

	if got := Net.First(p).String(); got != "192.168.1.1" {
		t.Errorf("First: %s", got)
	}
	if got := Net.Last(p).String(); got != "192.168.1.254" {
		t.Errorf("Last: %s", got)
	}
	if got := Net.Broadcast(p).String(); got != "192.168.1.255" {
		t.Errorf("Broadcast: %s", got)
	}
	if got := Net.Size(p); got != 256 {
		t.Errorf("Size: %d", got)
	}
	if got := Net.UsableSize(p); got != 254 {
		t.Errorf("UsableSize: %d", got)
	}
}

func TestNetSmallNetworks(t *testing.T) {
	p31 := Must(netip.ParsePrefix("10.0.0.0/31"))
	if Net.First(p31).String() != "10.0.0.0" || Net.Last(p31).String() != "10.0.0.1" {
		t.Errorf("/31: first=%s last=%s", Net.First(p31), Net.Last(p31))
	}
	if Net.UsableSize(p31) != 2 {
		t.Errorf("/31 usable: %d", Net.UsableSize(p31))
	}

	p32 := Must(netip.ParsePrefix("10.0.0.7/32"))
	if Net.First(p32) != Net.Last(p32) || Net.First(p32).String() != "10.0.0.7" {
		t.Errorf("/32: first=%s last=%s", Net.First(p32), Net.Last(p32))
	}
}

func TestNetIPv6(t *testing.T) {
	p := Must(netip.ParsePrefix("2001:db8::/120"))

	if got := Net.Size(p); got != 256 {
		t.Errorf("v6 Size: %d", got)
	}
	if got := Net.UsableSize(p); got != 256 {
		t.Errorf("v6 UsableSize: %d", got)
	}
	if got := Net.First(p).String(); got != "2001:db8::" {
		t.Errorf("v6 First: %s", got)
	}
	if got := Net.Last(p).String(); got != "2001:db8::ff" {
		t.Errorf("v6 Last: %s", got)
	}

	wide := Must(netip.ParsePrefix("2001:db8::/32"))
	if got := Net.Size(wide); got != ^uint64(0) {
		t.Errorf("v6 wide Size should saturate: %d", got)
	}
}

func TestNetIteration(t *testing.T) {
	p := Must(netip.ParsePrefix("10.0.0.0/30"))

	var ips []string
	for ip := Net.First(p); p.Contains(ip) && len(ips) < 10; ip = ip.Next() {
		ips = append(ips, ip.String())
	}
	// First=.1, then .2, .3 (broadcast) still Contains, then .4 leaves the prefix
	if len(ips) != 3 || ips[0] != "10.0.0.1" || ips[2] != "10.0.0.3" {
		t.Errorf("iteration: %v", ips)
	}
}
