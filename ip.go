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
//	myNetwork := Must(IP.ParseCIDR("10.0.0.0/8"))       // Static CIDR
//	serverIP := Must(IP.FromString("192.168.1.1", V4))  // Assert IPv4
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
type IPAddr struct {
	v4 *IPv4
	v6 *IPv6
}

// IPNetwork represents an IP network (CIDR range)
type IPNetwork struct {
	ip   IPAddr
	bits int // prefix length (e.g., 24 for /24)
}

// ipNamespace provides all IP-related functions
type ipNamespace struct{}

// IP is the namespace for all IP operations
var IP = ipNamespace{}

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
		v4 := IPv4{ip4[0], ip4[1], ip4[2], ip4[3]}
		result = IPAddr{v4: &v4}
	} else {
		var v6 IPv6
		copy(v6[:], ip)
		result = IPAddr{v6: &v6}
	}
	
	// Check version if specified
	if len(version) > 0 {
		switch version[0] {
		case V4:
			if result.v4 == nil {
				return IPAddr{}, fmt.Errorf("expected IPv4, got IPv6: %s", s)
			}
		case V6:
			if result.v6 == nil {
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
		v4 := IPv4{b[0], b[1], b[2], b[3]}
		result = IPAddr{v4: &v4}
	case 16:
		var v6 IPv6
		copy(v6[:], b)
		result = IPAddr{v6: &v6}
	default:
		return IPAddr{}, fmt.Errorf("invalid byte length for IP: %d (expected 4 or 16)", len(b))
	}
	
	// Check version if specified
	if len(version) > 0 {
		switch version[0] {
		case V4:
			if result.v4 == nil {
				return IPAddr{}, fmt.Errorf("expected IPv4, got IPv6")
			}
		case V6:
			if result.v6 == nil {
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
		
		var v4 IPv4
		for i, part := range ipParts {
			switch v := part.(type) {
			case byte:
				v4[i] = v
			case int:
				if v < 0 || v > 255 {
					return IPAddr{}, fmt.Errorf("IPv4 byte out of range: %d", v)
				}
				v4[i] = byte(v)
			default:
				return IPAddr{}, fmt.Errorf("IPv4 requires byte or int values, got %T", part)
			}
		}
		return IPAddr{v4: &v4}, nil
		
	case 8:
		// IPv6: expect 8 uint16s
		if version != nil && *version != V6 {
			return IPAddr{}, fmt.Errorf("expected IPv4 with 4 values, got 8")
		}
		
		var v6 IPv6
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
			v6[i*2] = byte(val >> 8)
			v6[i*2+1] = byte(val)
		}
		return IPAddr{v6: &v6}, nil
		
	default:
		return IPAddr{}, fmt.Errorf("IP.New requires 4 values (IPv4) or 8 values (IPv6), got %d", len(ipParts))
	}
}

// NewIPv4 creates IPAddr from an IPv4 type
func (ipNamespace) NewIPv4(v4 IPv4) IPAddr {
	return IPAddr{v4: &v4}
}

// NewIPv6 creates IPAddr from an IPv6 type
func (ipNamespace) NewIPv6(v6 IPv6) IPAddr {
	return IPAddr{v6: &v6}
}

// ParseCIDR parses a CIDR notation string (e.g., "192.168.1.0/24")
// Returns the network address and prefix length
func (ipNamespace) ParseCIDR(s string) (IPNetwork, error) {
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		return IPNetwork{}, err
	}
	
	// Get the network address (first IP in range)
	var networkIP IPAddr
	if ip4 := ipnet.IP.To4(); ip4 != nil {
		v4 := IPv4{ip4[0], ip4[1], ip4[2], ip4[3]}
		networkIP = IPAddr{v4: &v4}
	} else {
		var v6 IPv6
		copy(v6[:], ipnet.IP)
		networkIP = IPAddr{v6: &v6}
	}
	
	// Calculate prefix length from mask
	ones, _ := ipnet.Mask.Size()
	
	return IPNetwork{
		ip:   networkIP,
		bits: ones,
	}, nil
}

// Version returns 4 for IPv4, 6 for IPv6
func (ip IPAddr) Version() int {
	if ip.v4 != nil {
		return 4
	}
	if ip.v6 != nil {
		return 6
	}
	panic("invalid IP: neither v4 nor v6")
}

// String returns the string representation of the IP
func (ip IPAddr) String() string {
	if ip.v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d", ip.v4[0], ip.v4[1], ip.v4[2], ip.v4[3])
	}
	if ip.v6 != nil {
		return net.IP(ip.v6[:]).String()
	}
	panic("invalid IP: neither v4 nor v6")
}

// Bytes returns the raw byte representation
func (ip IPAddr) Bytes() []byte {
	if ip.v4 != nil {
		return ip.v4[:]
	}
	if ip.v6 != nil {
		return ip.v6[:]
	}
	panic("invalid IP: neither v4 nor v6")
}

// IsPrivate checks if this is a private IP address
func (ip IPAddr) IsPrivate() bool {
	if ip.v4 != nil {
		return ip.v4[0] == 10 ||
			(ip.v4[0] == 172 && ip.v4[1] >= 16 && ip.v4[1] <= 31) ||
			(ip.v4[0] == 192 && ip.v4[1] == 168)
	}
	if ip.v6 != nil {
		// IPv6 private ranges:
		// fc00::/7 - Unique local addresses (fc00:: to fdff::)
		// fe80::/10 - Link-local addresses
		// fec0::/10 - Site-local addresses (deprecated but still exists)
		return (ip.v6[0] == 0xfc || ip.v6[0] == 0xfd) || // fc00::/7 (unique local)
			(ip.v6[0] == 0xfe && (ip.v6[1] & 0xc0) == 0x80) || // fe80::/10 (link-local)
			(ip.v6[0] == 0xfe && (ip.v6[1] & 0xc0) == 0xc0) // fec0::/10 (site-local, deprecated)
	}
	panic("invalid IP: neither v4 nor v6")
}

// IsLoopback checks if this is a loopback address
func (ip IPAddr) IsLoopback() bool {
	if ip.v4 != nil {
		return ip.v4[0] == 127
	}
	if ip.v6 != nil {
		// IPv6 loopback is ::1
		for i := 0; i < 15; i++ {
			if ip.v6[i] != 0 {
				return false
			}
		}
		return ip.v6[15] == 1
	}
	panic("invalid IP: neither v4 nor v6")
}

// IsUnspecified checks if this is an unspecified address (0.0.0.0 or ::)
// These addresses mean "any address" and are used for binding to all interfaces
func (ip IPAddr) IsUnspecified() bool {
	if ip.v4 != nil {
		return ip.v4[0] == 0 && ip.v4[1] == 0 && ip.v4[2] == 0 && ip.v4[3] == 0
	}
	if ip.v6 != nil {
		for i := 0; i < 16; i++ {
			if ip.v6[i] != 0 {
				return false
			}
		}
		return true
	}
	panic("invalid IP: neither v4 nor v6")
}

// ToNetIP converts to stdlib net.IP for compatibility
func (ip IPAddr) ToNetIP() net.IP {
	if ip.v4 != nil {
		return net.IPv4(ip.v4[0], ip.v4[1], ip.v4[2], ip.v4[3])
	}
	if ip.v6 != nil {
		return net.IP(ip.v6[:])
	}
	panic("invalid IP: neither v4 nor v6")
}

// IsV4 returns true if this is an IPv4 address
func (ip IPAddr) IsV4() bool {
	return ip.v4 != nil
}

// IsV6 returns true if this is an IPv6 address
func (ip IPAddr) IsV6() bool {
	return ip.v6 != nil
}

// IPv4 returns the simple IPv4 type (panics if not v4)
func (ip IPAddr) IPv4() IPv4 {
	Assert(ip.v4 != nil, "IP is not IPv4")
	return *ip.v4
}

// IPv6 returns the simple IPv6 type (panics if not v6)
func (ip IPAddr) IPv6() IPv6 {
	Assert(ip.v6 != nil, "IP is not IPv6")
	return *ip.v6
}

// Equal checks if two IPs are equal
func (ip IPAddr) Equal(other IPAddr) bool {
	if ip.v4 != nil && other.v4 != nil {
		return *ip.v4 == *other.v4
	}
	if ip.v6 != nil && other.v6 != nil {
		return *ip.v6 == *other.v6
	}
	return false
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

// Methods for IPNetwork type

// Contains checks if the given IP is within this network
func (n IPNetwork) Contains(ip IPAddr) bool {
	// Both must be same version
	if (n.ip.v4 != nil) != (ip.v4 != nil) {
		return false
	}
	
	if n.ip.v4 != nil {
		// IPv4 comparison
		mask := uint32(0xFFFFFFFF << (32 - n.bits))
		netAddr := uint32(n.ip.v4[0])<<24 | uint32(n.ip.v4[1])<<16 | uint32(n.ip.v4[2])<<8 | uint32(n.ip.v4[3])
		ipAddr := uint32(ip.v4[0])<<24 | uint32(ip.v4[1])<<16 | uint32(ip.v4[2])<<8 | uint32(ip.v4[3])
		return (netAddr & mask) == (ipAddr & mask)
	}
	
	// IPv6 comparison
	bytesToCheck := n.bits / 8
	bitsRemaining := n.bits % 8
	
	// Check full bytes
	for i := 0; i < bytesToCheck; i++ {
		if n.ip.v6[i] != ip.v6[i] {
			return false
		}
	}
	
	// Check remaining bits if any
	if bitsRemaining > 0 && bytesToCheck < 16 {
		mask := byte(0xFF << (8 - bitsRemaining))
		if (n.ip.v6[bytesToCheck] & mask) != (ip.v6[bytesToCheck] & mask) {
			return false
		}
	}
	
	return true
}

// NetworkAddress returns the first IP in the network (network address)
func (n IPNetwork) NetworkAddress() IPAddr {
	return n.ip
}

// BroadcastAddress returns the last IP in the network (broadcast address)
func (n IPNetwork) BroadcastAddress() IPAddr {
	if n.ip.v4 != nil {
		// IPv4 broadcast
		hostBits := 32 - n.bits
		if hostBits == 0 {
			return n.ip // Single host
		}
		
		mask := uint32(0xFFFFFFFF << hostBits)
		netAddr := uint32(n.ip.v4[0])<<24 | uint32(n.ip.v4[1])<<16 | uint32(n.ip.v4[2])<<8 | uint32(n.ip.v4[3])
		broadcast := netAddr | ^mask
		
		v4 := IPv4{
			byte(broadcast >> 24),
			byte(broadcast >> 16),
			byte(broadcast >> 8),
			byte(broadcast),
		}
		return IPAddr{v4: &v4}
	}
	
	// IPv6 broadcast (though IPv6 doesn't use broadcast, this returns the last address)
	if n.ip.v6 != nil {
		var v6 IPv6
		copy(v6[:], n.ip.v6[:])
		
		// Set all host bits to 1
		hostBits := 128 - n.bits
		if hostBits == 0 {
			return n.ip // Single host
		}
		
		// Set bits from the end
		for i := 15; i >= 0 && hostBits > 0; i-- {
			if hostBits >= 8 {
				v6[i] = 0xFF
				hostBits -= 8
			} else {
				mask := byte((1 << hostBits) - 1)
				v6[i] = n.ip.v6[i] | mask
				hostBits = 0
			}
		}
		
		return IPAddr{v6: &v6}
	}
	
	panic("invalid network: neither v4 nor v6")
}

// String returns the CIDR notation string
func (n IPNetwork) String() string {
	return fmt.Sprintf("%s/%d", n.ip.String(), n.bits)
}