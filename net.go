// Network math the stdlib's net/netip leaves out.
//
// For the types themselves USE THE STDLIB - netip.Addr and netip.Prefix
// already make invalid states unrepresentable:
//
//	addr := Must(netip.ParseAddr("192.168.1.7"))
//	prefix := Must(netip.ParsePrefix("192.168.1.0/24"))
//	prefix.Contains(addr)               // true
//	addr.Next(), addr.Prev()            // iteration is built in
//
// Net adds the range/sizing helpers netip lacks:
//
//	Net.First(prefix)                   // 192.168.1.1
//	Net.Last(prefix)                    // 192.168.1.254
//	Net.Broadcast(prefix)               // 192.168.1.255
//	Net.Size(prefix)                    // 256
//	Net.UsableSize(prefix)              // 254
//
//	for ip := Net.First(prefix); prefix.Contains(ip); ip = ip.Next() {
//		fmt.Println(ip)
//	}
package utils

import "net/netip"

type netOps struct{}

// Net provides network range math over netip.Prefix.
var Net = netOps{}

// Broadcast returns the highest address in the prefix. For IPv6 there is
// no broadcast semantically - this is simply the last address.
func (netOps) Broadcast(p netip.Prefix) netip.Addr {
	Assert(p.IsValid(), "invalid prefix:", p)

	addr := p.Masked().Addr()
	bytes := addr.AsSlice()

	hostBits := len(bytes)*8 - p.Bits()
	for i := len(bytes) - 1; i >= 0 && hostBits > 0; i-- {
		if hostBits >= 8 {
			bytes[i] = 0xFF
			hostBits -= 8
		} else {
			bytes[i] |= (1 << hostBits) - 1
			hostBits = 0
		}
	}

	result, ok := netip.AddrFromSlice(bytes)
	Assert(ok, "broadcast computation produced invalid address")
	return result
}

// First returns the first usable address. For /31, /32, /127 and /128
// every address is usable, so it's the network address itself.
func (n netOps) First(p netip.Prefix) netip.Addr {
	Assert(p.IsValid(), "invalid prefix:", p)

	if n.UsableSize(p) == n.Size(p) {
		return p.Masked().Addr()
	}
	return p.Masked().Addr().Next()
}

// Last returns the last usable address. For IPv4 networks larger than
// /31 that's broadcast - 1; everywhere else the last address itself.
func (n netOps) Last(p netip.Prefix) netip.Addr {
	Assert(p.IsValid(), "invalid prefix:", p)

	if n.UsableSize(p) == n.Size(p) {
		return n.Broadcast(p)
	}
	return n.Broadcast(p).Prev()
}

// Size returns the total number of addresses in the prefix.
// Saturates at max uint64 for IPv6 prefixes wider than /64.
func (netOps) Size(p netip.Prefix) uint64 {
	Assert(p.IsValid(), "invalid prefix:", p)

	hostBits := 128 - p.Bits()
	if p.Addr().Is4() {
		hostBits = 32 - p.Bits()
	}
	if hostBits >= 64 {
		return ^uint64(0)
	}
	return 1 << hostBits
}

// UsableSize returns the number of host-assignable addresses: IPv4
// networks larger than /31 lose network and broadcast, everything else
// (IPv6, /31, /32) uses all of them.
func (n netOps) UsableSize(p netip.Prefix) uint64 {
	size := n.Size(p)
	if p.Addr().Is4() && p.Bits() < 31 {
		return size - 2
	}
	return size
}
