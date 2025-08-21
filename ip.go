// Package utils provides offensive programming utilities for Go.
// This file provides strong IP types that make invalid states unrepresentable.
//
// Two-level type system:
//   - IPv4/IPv6: Simple strongly-typed byte arrays
//   - IPAddr: Rich type with methods for validation and queries
//
// Usage:
//
//	// Parse from string (returns rich IPAddr with methods)
//	addr := IP.FromString("192.168.1.1")
//	fmt.Println(addr.Version())     // 4
//	fmt.Println(addr.String())       // "192.168.1.1"
//	fmt.Println(addr.IsPrivate())   // true
//	fmt.Println(addr.IsLoopback())  // false
//
//	// Get simple strongly-typed value
//	v4 := addr.IPv4()  // Returns IPv4 ([4]byte)
//	
//	// Parse with version assertion (panics if wrong)
//	addr := IP.FromString("192.168.1.1", V4)
//	addr := IP.FromString("2001:db8::1", V6)
//
//	// From bytes
//	addr := IP.FromBytes([]byte{192, 168, 1, 1})
//	addr := IP.FromBytes(sixteenBytes, V6)
//
//	// Direct construction
//	addr := IP.New(192, 168, 1, 1)                    // IPv4
//	addr := IP.New(0x2001, 0x0db8, 0, 0, 0, 0, 0, 1) // IPv6
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

// ipNamespace provides all IP-related functions
type ipNamespace struct{}

// IP is the namespace for all IP operations
var IP = ipNamespace{}

// FromString parses a string IP address into IPAddr.
// Optionally specify version to assert the IP is that version (panics if not).
func (ipNamespace) FromString(s string, version ...Version) IPAddr {
	ip := net.ParseIP(s)
	Assert(ip != nil, "invalid IP address:", s)
	
	var result IPAddr
	if ip4 := ip.To4(); ip4 != nil {
		v4 := IPv4{ip4[0], ip4[1], ip4[2], ip4[3]}
		result = IPAddr{v4: &v4}
	} else {
		var v6 IPv6
		copy(v6[:], ip)
		result = IPAddr{v6: &v6}
	}
	
	// Assert version if specified
	if len(version) > 0 {
		switch version[0] {
		case V4:
			Assert(result.v4 != nil, "expected IPv4, got IPv6:", s)
		case V6:
			Assert(result.v6 != nil, "expected IPv6, got IPv4:", s)
		default:
			Assert(false, "invalid version:", version[0])
		}
	}
	
	return result
}

// FromBytes creates an IPAddr from bytes.
// 4 bytes = IPv4, 16 bytes = IPv6.
// Optionally specify version to assert (panics if mismatch).
func (ipNamespace) FromBytes(b []byte, version ...Version) IPAddr {
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
		Assert(false, "invalid byte length for IP:", len(b), "expected 4 or 16")
	}
	
	// Assert version if specified
	if len(version) > 0 {
		switch version[0] {
		case V4:
			Assert(result.v4 != nil, "expected IPv4, got IPv6")
		case V6:
			Assert(result.v6 != nil, "expected IPv6, got IPv4")
		default:
			Assert(false, "invalid version:", version[0])
		}
	}
	
	return result
}

// New creates an IPAddr directly.
// 4 byte parameters = IPv4, 8 uint16 parameters = IPv6.
func (ipNamespace) New(parts ...interface{}) IPAddr {
	switch len(parts) {
	case 4:
		// IPv4: expect 4 bytes
		var v4 IPv4
		for i, part := range parts {
			switch v := part.(type) {
			case byte:
				v4[i] = v
			case int:
				Assert(v >= 0 && v <= 255, "IPv4 byte out of range:", v)
				v4[i] = byte(v)
			default:
				Assert(false, "IPv4 requires byte or int values")
			}
		}
		return IPAddr{v4: &v4}
		
	case 8:
		// IPv6: expect 8 uint16s
		var v6 IPv6
		for i, part := range parts {
			var val uint16
			switch v := part.(type) {
			case uint16:
				val = v
			case int:
				Assert(v >= 0 && v <= 0xFFFF, "IPv6 segment out of range:", v)
				val = uint16(v)
			default:
				Assert(false, "IPv6 requires uint16 or int values")
			}
			v6[i*2] = byte(val >> 8)
			v6[i*2+1] = byte(val)
		}
		return IPAddr{v6: &v6}
		
	default:
		Assert(false, "IP.New requires 4 values (IPv4) or 8 values (IPv6), got:", len(parts))
		return IPAddr{}
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
		// IPv6 private ranges: fc00::/7
		return ip.v6[0] == 0xfc || ip.v6[0] == 0xfd
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