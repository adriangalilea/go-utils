package tests

import (
	"testing"

	. "github.com/adriangalilea/go-utils"
)

func TestIPAddrValueSemantics(t *testing.T) {
	// Test that == works for IPAddr
	addr1 := Must(IP.FromString("192.168.1.100"))
	addr2 := Must(IP.FromString("192.168.1.100"))
	addr3 := Must(IP.FromString("192.168.1.101"))

	// Test equality with ==
	if addr1 != addr2 {
		t.Errorf("Expected addr1 == addr2, but they were not equal")
	}

	if addr1 == addr3 {
		t.Errorf("Expected addr1 != addr3, but they were equal")
	}

	// Test IPv6
	addr6a := Must(IP.FromString("2001:db8::1"))
	addr6b := Must(IP.FromString("2001:db8::1"))
	addr6c := Must(IP.FromString("2001:db8::2"))

	if addr6a != addr6b {
		t.Errorf("Expected IPv6 addresses to be equal")
	}

	if addr6a == addr6c {
		t.Errorf("Expected IPv6 addresses to be different")
	}

	// Test mixed versions
	if addr1 == addr6a {
		t.Errorf("IPv4 and IPv6 should never be equal")
	}
}

func TestNetworkValueSemantics(t *testing.T) {
	// Test that == works for Network
	net1 := Must(Net.ParseCIDR("192.168.1.0/24"))
	net2 := Must(Net.ParseCIDR("192.168.1.0/24"))
	net3 := Must(Net.ParseCIDR("192.168.2.0/24"))
	net4 := Must(Net.ParseCIDR("192.168.1.0/25"))

	if net1 != net2 {
		t.Errorf("Expected identical networks to be equal")
	}

	if net1 == net3 {
		t.Errorf("Different network addresses should not be equal")
	}

	if net1 == net4 {
		t.Errorf("Different prefix lengths should not be equal")
	}
}

func TestSimpleTypeComparison(t *testing.T) {
	// Test IPv4 simple type
	addr := Must(IP.FromString("192.168.1.1"))
	ip1 := addr.IPv4()
	ip2 := addr.IPv4()

	if ip1 != ip2 {
		t.Errorf("Expected IPv4 simple types to be equal")
	}

	// Direct construction
	ip3 := IPv4{192, 168, 1, 1}
	if ip1 != ip3 {
		t.Errorf("Expected IPv4 values to be equal")
	}

	// Test IPv6 simple type
	addr6 := Must(IP.FromString("2001:db8::1"))
	ipv6_1 := addr6.IPv6()
	ipv6_2 := addr6.IPv6()

	if ipv6_1 != ipv6_2 {
		t.Errorf("Expected IPv6 simple types to be equal")
	}
}

func TestZeroValue(t *testing.T) {
	// Test that zero value is valid
	var addr IPAddr
	// Zero value should be IPv6 "::" (all zeros, is4=false)
	
	if addr.IsV4() {
		t.Errorf("Zero value should be IPv6")
	}
	
	if !addr.IsV6() {
		t.Errorf("Zero value should be IPv6")
	}
	
	if !addr.IsUnspecified() {
		t.Errorf("Zero value should be unspecified (::)")
	}
	
	// Should be comparable
	var addr2 IPAddr
	if addr != addr2 {
		t.Errorf("Zero values should be equal")
	}
}

func TestNetworkIteration(t *testing.T) {
	network := Must(Net.ParseCIDR("192.168.1.0/30"))
	
	// Test First and Last
	first := network.First()
	last := network.Last()
	
	expectedFirst := Must(IP.FromString("192.168.1.1"))
	expectedLast := Must(IP.FromString("192.168.1.2"))
	
	if first != expectedFirst {
		t.Errorf("First IP incorrect: got %s, want %s", first.String(), expectedFirst.String())
	}
	
	if last != expectedLast {
		t.Errorf("Last IP incorrect: got %s, want %s", last.String(), expectedLast.String())
	}
	
	// Test iteration
	current := first
	count := 0
	for {
		count++
		if current == last {
			break
		}
		var err error
		current, err = network.Next(current)
		if err != nil {
			t.Fatalf("Unexpected error during iteration: %v", err)
		}
		if count > 10 {
			t.Fatal("Iteration exceeded expected count")
		}
	}
	
	if count != 2 {
		t.Errorf("Expected 2 usable IPs, got %d", count)
	}
}