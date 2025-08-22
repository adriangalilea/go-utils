// Package utils provides offensive programming utilities for Go.
// This file provides strong IP types that make invalid states unrepresentable.
//
// Two-level type system:
//   - IPv4/IPv6: Simple strongly-typed byte arrays
//   - IPAddr: Rich type with methods for validation and queries
//
// Usage:
//
//	// Parse from string (returns IPAddr and error)
//	addr, err := IP.FromString("192.168.1.1")
//	Check(err)  // Or use Must() for offensive programming
//	fmt.Println(addr.Version())     // 4
//	fmt.Println(addr.String())       // "192.168.1.1"
//	fmt.Println(addr.IsPrivate())   // true
//	fmt.Println(addr.IsLoopback())  // false
//
//	// Offensive programming with Must - for values that SHOULD work
//	localhost := Must(IP.FromString("127.0.0.1"))        // Static IP, panic if fails
//	serverIP := Must(IP.FromString("192.168.1.1", V4))  // Assert IPv4
//	
//	// Network operations with Net namespace
//	network := Must(Net.ParseCIDR("192.168.1.0/24"))
//	fmt.Println(network.Contains(addr))      // true
//	fmt.Println(network.First())             // 192.168.1.1
//	fmt.Println(network.Last())              // 192.168.1.254
//	fmt.Println(network.Size())              // 256
//	fmt.Println(network.UsableSize())        // 254
//	
//	// Network-aware iteration
//	for ip := network.First(); ip != network.Last(); {
//		fmt.Println(ip)
//		ip = Must(network.Next(ip))
//	}
//	
//	// Check for user/runtime errors - exits cleanly with message
//	addr, err := IP.FromString(userProvidedIP)
//	Check(err, "Invalid IP address provided")
//	
//	// Get simple strongly-typed value
//	v4 := addr.IPv4()  // Returns IPv4 ([4]byte), uses Assert internally
//	
//	// Parse with version assertion (returns error if wrong version)
//	addr, err := IP.FromString("192.168.1.1", V4)
//	addr, err := IP.FromString("2001:db8::1", V6)
//
//	// From bytes
//	addr, err := IP.FromBytes([]byte{192, 168, 1, 1})
//	addr, err := IP.FromBytes(sixteenBytes, V6)
//
//	// Direct construction
//	addr, err := IP.New(192, 168, 1, 1)                    // IPv4
//	addr, err := IP.New(0x2001, 0x0db8, 0, 0, 0, 0, 0, 1) // IPv6
//	addr, err := IP.New(192, 168, 1, 1, V4)                // With version assertion
package utils

import (
	"fmt"
	"net"
)

// IPv4 is a strongly-typed IPv4 address (exactly 4 bytes).
// Simple type with no methods, guaranteed valid by construction.
type IPv4 [4]byte

// IPv6 is a strongly-typed IPv6 address (exactly 16 bytes).
// Simple type with no methods, guaranteed valid by construction.
type IPv6 [16]byte

// Version represents IP version for optional assertions
type Version int

const (
	V4 Version = 4
	V6 Version = 6
)

// IPAddr is the rich IP type with validation and query methods.
// Contains either an IPv4 or IPv6 address.
// Uses value semantics for direct comparison with ==
type IPAddr struct {
	data [16]byte // IPv4 uses first 4 bytes, IPv6 uses all 16
	is4  bool     // true for IPv4, false for IPv6
}

// Network represents an IP network (CIDR range)
type Network struct {
	ip   IPAddr
	bits int // prefix length (e.g., 24 for /24)
}

// ipNamespace provides all IP-related functions
type ipNamespace struct{}

// netNamespace provides all network-related functions
type netNamespace struct{}

// IP is the namespace for all IP operations
var IP = ipNamespace{}

// Net is the namespace for all network operations
var Net = netNamespace{}

// FromString parses a string IP address into IPAddr.
// Optionally specify version to assert the IP is that version.
// Returns error if the string is not a valid IP or version mismatch.
func (ipNamespace) FromString(s string, version ...Version) (IPAddr, error) {
	ip := net.ParseIP(s)
	if ip == nil {
		return IPAddr{}, fmt.Errorf("invalid IP address: %s", s)
	}
	
	var result IPAddr
	if ip4 := ip.To4(); ip4 != nil {
		copy(result.data[:4], ip4)
		result.is4 = true
	} else {
		copy(result.data[:], ip)
		result.is4 = false
	}
	
	// Check version if specified
	if len(version) > 0 {
		switch version[0] {
		case V4:
			if !result.is4 {
				return IPAddr{}, fmt.Errorf("expected IPv4, got IPv6: %s", s)
			}
		case V6:
			if result.is4 {
				return IPAddr{}, fmt.Errorf("expected IPv6, got IPv4: %s", s)
			}
		default:
			return IPAddr{}, fmt.Errorf("invalid version: %d", version[0])
		}
	}
	
	return result, nil
}

// FromBytes creates an IPAddr from bytes.
// 4 bytes = IPv4, 16 bytes = IPv6.
// Optionally specify version to assert.
// Returns error if byte length is invalid or version mismatch.
func (ipNamespace) FromBytes(b []byte, version ...Version) (IPAddr, error) {
	var result IPAddr
	
	switch len(b) {
	case 4:
		copy(result.data[:4], b)
		result.is4 = true
	case 16:
		copy(result.data[:], b)
		result.is4 = false
	default:
		return IPAddr{}, fmt.Errorf("invalid byte length for IP: %d (expected 4 or 16)", len(b))
	}
	
	// Check version if specified
	if len(version) > 0 {
		switch version[0] {
		case V4:
			if !result.is4 {
				return IPAddr{}, fmt.Errorf("expected IPv4, got IPv6")
			}
		case V6:
			if result.is4 {
				return IPAddr{}, fmt.Errorf("expected IPv6, got IPv4")
			}
		default:
			return IPAddr{}, fmt.Errorf("invalid version: %d", version[0])
		}
	}
	
	return result, nil
}

// New creates an IPAddr directly.
// 4 byte parameters = IPv4, 8 uint16 parameters = IPv6.
// Optionally specify version to assert the IP is that version.
// Returns error if invalid number of parts or values out of range.
func (ipNamespace) New(parts ...interface{}) (IPAddr, error) {
	// Check if last parameter is a Version
	var version *Version
	ipParts := parts
	
	if len(parts) > 0 {
		if v, ok := parts[len(parts)-1].(Version); ok {
			version = &v
			ipParts = parts[:len(parts)-1]
		}
	}
	
	switch len(ipParts) {
	case 4:
		// IPv4: expect 4 bytes
		if version != nil && *version != V4 {
			return IPAddr{}, fmt.Errorf("expected IPv6 with 8 values, got 4")
		}
		
		var result IPAddr
		result.is4 = true
		for i, part := range ipParts {
			switch v := part.(type) {
			case byte:
				result.data[i] = v
			case int:
				if v < 0 || v > 255 {
					return IPAddr{}, fmt.Errorf("IPv4 byte out of range: %d", v)
				}
				result.data[i] = byte(v)
			default:
				return IPAddr{}, fmt.Errorf("IPv4 requires byte or int values, got %T", part)
			}
		}
		return result, nil
		
	case 8:
		// IPv6: expect 8 uint16s
		if version != nil && *version != V6 {
			return IPAddr{}, fmt.Errorf("expected IPv4 with 4 values, got 8")
		}
		
		var result IPAddr
		result.is4 = false
		for i, part := range ipParts {
			var val uint16
			switch v := part.(type) {
			case uint16:
				val = v
			case int:
				if v < 0 || v > 0xFFFF {
					return IPAddr{}, fmt.Errorf("IPv6 segment out of range: %d", v)
				}
				val = uint16(v)
			default:
				return IPAddr{}, fmt.Errorf("IPv6 requires uint16 or int values, got %T", part)
			}
			result.data[i*2] = byte(val >> 8)
			result.data[i*2+1] = byte(val)
		}
		return result, nil
		
	default:
		return IPAddr{}, fmt.Errorf("IP.New requires 4 values (IPv4) or 8 values (IPv6), got %d", len(ipParts))
	}
}

// NewIPv4 creates IPAddr from an IPv4 type
func (ipNamespace) NewIPv4(v4 IPv4) IPAddr {
	var result IPAddr
	copy(result.data[:4], v4[:])
	result.is4 = true
	return result
}

// NewIPv6 creates IPAddr from an IPv6 type
func (ipNamespace) NewIPv6(v6 IPv6) IPAddr {
	var result IPAddr
	copy(result.data[:], v6[:])
	result.is4 = false
	return result
}

// FromNetIP creates an IPAddr from stdlib net.IP
func (ipNamespace) FromNetIP(ip net.IP) (IPAddr, error) {
	if ip == nil {
		return IPAddr{}, fmt.Errorf("nil net.IP")
	}
	
	var result IPAddr
	
	// Try to get IPv4 representation
	if ip4 := ip.To4(); ip4 != nil {
		copy(result.data[:4], ip4)
		result.is4 = true
		return result, nil
	}
	
	// Otherwise treat as IPv6
	ip6 := ip.To16()
	if ip6 == nil {
		return IPAddr{}, fmt.Errorf("invalid net.IP: not IPv4 or IPv6")
	}
	
	copy(result.data[:], ip6)
	result.is4 = false
	return result, nil
}

// ParseCIDR parses a CIDR notation string (e.g., "192.168.1.0/24")
// Returns the network address and prefix length
func (netNamespace) ParseCIDR(s string) (Network, error) {
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		return Network{}, err
	}
	
	// Get the network address (first IP in range)
	var networkIP IPAddr
	if ip4 := ipnet.IP.To4(); ip4 != nil {
		copy(networkIP.data[:4], ip4)
		networkIP.is4 = true
	} else {
		copy(networkIP.data[:], ipnet.IP)
		networkIP.is4 = false
	}
	
	// Calculate prefix length from mask
	ones, _ := ipnet.Mask.Size()
	
	return Network{
		ip:   networkIP,
		bits: ones,
	}, nil
}

// Version returns 4 for IPv4, 6 for IPv6
func (ip IPAddr) Version() int {
	if ip.is4 {
		return 4
	}
	return 6
}

// String returns the string representation of the IP
func (ip IPAddr) String() string {
	if ip.is4 {
		return fmt.Sprintf("%d.%d.%d.%d", ip.data[0], ip.data[1], ip.data[2], ip.data[3])
	}
	return net.IP(ip.data[:]).String()
}

// Bytes returns the raw byte representation
func (ip IPAddr) Bytes() []byte {
	if ip.is4 {
		return ip.data[:4]
	}
	return ip.data[:]
}

// IsPrivate checks if this is a private IP address
func (ip IPAddr) IsPrivate() bool {
	if ip.is4 {
		return ip.data[0] == 10 ||
			(ip.data[0] == 172 && ip.data[1] >= 16 && ip.data[1] <= 31) ||
			(ip.data[0] == 192 && ip.data[1] == 168)
	}
	// IPv6 private ranges:
	// fc00::/7 - Unique local addresses (fc00:: to fdff::)
	// fe80::/10 - Link-local addresses
	// fec0::/10 - Site-local addresses (deprecated but still exists)
	return (ip.data[0] == 0xfc || ip.data[0] == 0xfd) || // fc00::/7 (unique local)
		(ip.data[0] == 0xfe && (ip.data[1] & 0xc0) == 0x80) || // fe80::/10 (link-local)
		(ip.data[0] == 0xfe && (ip.data[1] & 0xc0) == 0xc0) // fec0::/10 (site-local, deprecated)
}

// IsLoopback checks if this is a loopback address
func (ip IPAddr) IsLoopback() bool {
	if ip.is4 {
		return ip.data[0] == 127
	}
	// IPv6 loopback is ::1
	for i := 0; i < 15; i++ {
		if ip.data[i] != 0 {
			return false
		}
	}
	return ip.data[15] == 1
}

// IsUnspecified checks if this is an unspecified address (0.0.0.0 or ::)
// These addresses mean "any address" and are used for binding to all interfaces
func (ip IPAddr) IsUnspecified() bool {
	if ip.is4 {
		return ip.data[0] == 0 && ip.data[1] == 0 && ip.data[2] == 0 && ip.data[3] == 0
	}
	for i := 0; i < 16; i++ {
		if ip.data[i] != 0 {
			return false
		}
	}
	return true
}

// ToNetIP converts to stdlib net.IP for compatibility
func (ip IPAddr) ToNetIP() net.IP {
	if ip.is4 {
		return net.IPv4(ip.data[0], ip.data[1], ip.data[2], ip.data[3])
	}
	return net.IP(ip.data[:])
}

// IsV4 returns true if this is an IPv4 address
func (ip IPAddr) IsV4() bool {
	return ip.is4
}

// IsV6 returns true if this is an IPv6 address
func (ip IPAddr) IsV6() bool {
	return !ip.is4
}

// IPv4 returns the simple IPv4 type (panics if not v4)
func (ip IPAddr) IPv4() IPv4 {
	Assert(ip.is4, "IP is not IPv4")
	return IPv4{ip.data[0], ip.data[1], ip.data[2], ip.data[3]}
}

// IPv6 returns the simple IPv6 type (panics if not v6)
func (ip IPAddr) IPv6() IPv6 {
	Assert(!ip.is4, "IP is not IPv6")
	var v6 IPv6
	copy(v6[:], ip.data[:])
	return v6
}

// Methods for simple IPv4 type

// String returns the dotted decimal representation
func (ip IPv4) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
}

// ToNetIP converts to stdlib net.IP
func (ip IPv4) ToNetIP() net.IP {
	return net.IPv4(ip[0], ip[1], ip[2], ip[3])
}

// Bytes returns the 4-byte slice
func (ip IPv4) Bytes() []byte {
	return ip[:]
}

// Methods for simple IPv6 type

// String returns the standard IPv6 representation
func (ip IPv6) String() string {
	return net.IP(ip[:]).String()
}

// ToNetIP converts to stdlib net.IP
func (ip IPv6) ToNetIP() net.IP {
	return net.IP(ip[:])
}

// Bytes returns the 16-byte slice
func (ip IPv6) Bytes() []byte {
	return ip[:]
}

// Methods for Network type

// Contains checks if the given IP is within this network
func (n Network) Contains(ip IPAddr) bool {
	// Both must be same version
	if n.ip.is4 != ip.is4 {
		return false
	}
	
	if n.ip.is4 {
		// IPv4 comparison
		mask := uint32(0xFFFFFFFF << (32 - n.bits))
		netAddr := uint32(n.ip.data[0])<<24 | uint32(n.ip.data[1])<<16 | uint32(n.ip.data[2])<<8 | uint32(n.ip.data[3])
		ipAddr := uint32(ip.data[0])<<24 | uint32(ip.data[1])<<16 | uint32(ip.data[2])<<8 | uint32(ip.data[3])
		return (netAddr & mask) == (ipAddr & mask)
	}
	
	// IPv6 comparison
	bytesToCheck := n.bits / 8
	bitsRemaining := n.bits % 8
	
	// Check full bytes
	for i := 0; i < bytesToCheck; i++ {
		if n.ip.data[i] != ip.data[i] {
			return false
		}
	}
	
	// Check remaining bits if any
	if bitsRemaining > 0 && bytesToCheck < 16 {
		mask := byte(0xFF << (8 - bitsRemaining))
		if (n.ip.data[bytesToCheck] & mask) != (ip.data[bytesToCheck] & mask) {
			return false
		}
	}
	
	return true
}

// NetworkAddress returns the first IP in the network (network address)
func (n Network) NetworkAddress() IPAddr {
	return n.ip
}

// BroadcastAddress returns the last IP in the network (broadcast address)
func (n Network) BroadcastAddress() IPAddr {
	if n.ip.is4 {
		// IPv4 broadcast
		hostBits := 32 - n.bits
		if hostBits == 0 {
			return n.ip // Single host
		}
		
		mask := uint32(0xFFFFFFFF << hostBits)
		netAddr := uint32(n.ip.data[0])<<24 | uint32(n.ip.data[1])<<16 | uint32(n.ip.data[2])<<8 | uint32(n.ip.data[3])
		broadcast := netAddr | ^mask
		
		var result IPAddr
		result.is4 = true
		result.data[0] = byte(broadcast >> 24)
		result.data[1] = byte(broadcast >> 16)
		result.data[2] = byte(broadcast >> 8)
		result.data[3] = byte(broadcast)
		return result
	}
	
	// IPv6 broadcast (though IPv6 doesn't use broadcast, this returns the last address)
	var result IPAddr
	copy(result.data[:], n.ip.data[:])
	result.is4 = false
	
	// Set all host bits to 1
	hostBits := 128 - n.bits
	if hostBits == 0 {
		return n.ip // Single host
	}
	
	// Set bits from the end
	for i := 15; i >= 0 && hostBits > 0; i-- {
		if hostBits >= 8 {
			result.data[i] = 0xFF
			hostBits -= 8
		} else {
			mask := byte((1 << hostBits) - 1)
			result.data[i] = n.ip.data[i] | mask
			hostBits = 0
		}
	}
	
	return result
}

// First returns the first usable IP in the network (network address + 1)
// For /31 and /32 (or /127 and /128 for IPv6), returns the network address itself
func (n Network) First() IPAddr {
	// For very small networks, the network address is usable
	if n.ip.is4 && n.bits >= 31 {
		return n.NetworkAddress()
	}
	if !n.ip.is4 && n.bits >= 127 {
		return n.NetworkAddress()
	}
	
	// Otherwise, network + 1
	return n.addToIP(n.NetworkAddress(), 1)
}

// Last returns the last usable IP in the network (broadcast address - 1)
// For /31 and /32 (or /127 and /128 for IPv6), returns the broadcast address itself
func (n Network) Last() IPAddr {
	// For very small networks, the broadcast address is usable
	if n.ip.is4 && n.bits >= 31 {
		return n.BroadcastAddress()
	}
	if !n.ip.is4 && n.bits >= 127 {
		return n.BroadcastAddress()
	}
	
	// Otherwise, broadcast - 1
	return n.addToIP(n.BroadcastAddress(), -1)
}

// Size returns the total number of IP addresses in this network
func (n Network) Size() uint64 {
	if n.ip.is4 {
		hostBits := 32 - n.bits
		if hostBits > 31 {
			// Avoid overflow for /0
			return 1 << 32
		}
		return 1 << uint(hostBits)
	}
	
	// IPv6
	hostBits := 128 - n.bits
	if hostBits > 63 {
		// For very large IPv6 networks, return max uint64
		// (actual size would overflow uint64)
		return ^uint64(0)
	}
	return 1 << uint(hostBits)
}

// UsableSize returns the number of usable host IPs in this network
// (excludes network and broadcast addresses for IPv4 networks larger than /31)
func (n Network) UsableSize() uint64 {
	size := n.Size()
	
	// For IPv4 networks larger than /31, subtract network and broadcast
	if n.ip.is4 && n.bits < 31 && size > 2 {
		return size - 2
	}
	
	// For IPv6 or small IPv4 networks, all IPs are usable
	return size
}

// Next returns the next IP address within the network bounds
// Returns error if the IP is not in the network or if it's the last IP
func (n Network) Next(ip IPAddr) (IPAddr, error) {
	if !n.Contains(ip) {
		return IPAddr{}, fmt.Errorf("IP %s is not in network %s", ip.String(), n.String())
	}
	
	next := n.addToIP(ip, 1)
	
	// Check if still in network
	if !n.Contains(next) {
		return IPAddr{}, fmt.Errorf("no next IP: %s is the last IP in network", ip.String())
	}
	
	return next, nil
}

// Prev returns the previous IP address within the network bounds
// Returns error if the IP is not in the network or if it's the first IP
func (n Network) Prev(ip IPAddr) (IPAddr, error) {
	if !n.Contains(ip) {
		return IPAddr{}, fmt.Errorf("IP %s is not in network %s", ip.String(), n.String())
	}
	
	prev := n.addToIP(ip, -1)
	
	// Check if still in network
	if !n.Contains(prev) {
		return IPAddr{}, fmt.Errorf("no previous IP: %s is the first IP in network", ip.String())
	}
	
	return prev, nil
}

// addToIP is a helper to add/subtract from an IP address
func (n Network) addToIP(ip IPAddr, delta int) IPAddr {
	if ip.is4 {
		// Convert to uint32, add, convert back
		val := uint32(ip.data[0])<<24 | uint32(ip.data[1])<<16 | uint32(ip.data[2])<<8 | uint32(ip.data[3])
		if delta < 0 {
			val -= uint32(-delta)
		} else {
			val += uint32(delta)
		}
		var result IPAddr
		result.is4 = true
		result.data[0] = byte(val >> 24)
		result.data[1] = byte(val >> 16)
		result.data[2] = byte(val >> 8)
		result.data[3] = byte(val)
		return result
	}
	
	// For IPv6, handle byte by byte with carry
	var result IPAddr
	copy(result.data[:], ip.data[:])
	result.is4 = false
	
	if delta > 0 {
		carry := delta
		for i := 15; i >= 0 && carry > 0; i-- {
			sum := int(result.data[i]) + carry
			result.data[i] = byte(sum & 0xFF)
			carry = sum >> 8
		}
	} else {
		borrow := -delta
		for i := 15; i >= 0 && borrow > 0; i-- {
			if int(result.data[i]) >= borrow {
				result.data[i] -= byte(borrow)
				borrow = 0
			} else {
				result.data[i] = byte(256 + int(result.data[i]) - borrow)
				borrow = 1
			}
		}
	}
	return result
}

// String returns the CIDR notation string
func (n Network) String() string {
	return fmt.Sprintf("%s/%d", n.ip.String(), n.bits)
}

// ToNetIPNet converts to stdlib *net.IPNet for compatibility
func (n Network) ToNetIPNet() *net.IPNet {
	// Calculate the mask
	var mask net.IPMask
	if n.ip.is4 {
		mask = net.CIDRMask(n.bits, 32)
	} else {
		mask = net.CIDRMask(n.bits, 128)
	}
	
	return &net.IPNet{
		IP:   n.ip.ToNetIP(),
		Mask: mask,
	}
}

// FromNetIPNet creates a Network from stdlib *net.IPNet
func (netNamespace) FromNetIPNet(ipnet *net.IPNet) Network {
	// Convert IP
	var ip IPAddr
	if ip4 := ipnet.IP.To4(); ip4 != nil {
		copy(ip.data[:4], ip4)
		ip.is4 = true
	} else {
		copy(ip.data[:], ipnet.IP.To16())
		ip.is4 = false
	}
	
	// Get prefix length from mask
	ones, _ := ipnet.Mask.Size()
	
	return Network{
		ip:   ip,
		bits: ones,
	}
}