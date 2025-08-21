package utils

import (
	"fmt"
	"net"
)

// IPv4 represents a guaranteed valid IPv4 address as exactly 4 bytes.
// Once constructed, it's always valid - no need for defensive checks.
type IPv4 [4]byte

// IPv6 represents a guaranteed valid IPv6 address as exactly 16 bytes.
// Once constructed, it's always valid - no need for defensive checks.
type IPv6 [16]byte

// IP is a discriminated union that can hold either IPv4 or IPv6.
// Only one field will be non-nil at a time.
type IP struct {
	v4 *IPv4
	v6 *IPv6
}

// NewIPv4 creates an IPv4 from 4 bytes
func NewIPv4(a, b, c, d byte) IPv4 {
	return IPv4{a, b, c, d}
}

// NewIPv4FromBytes creates an IPv4 from a byte slice
func NewIPv4FromBytes(b []byte) IPv4 {
	Assert(len(b) == 4, "IPv4 requires exactly 4 bytes, got:", len(b))
	return IPv4{b[0], b[1], b[2], b[3]}
}

// ParseIPv4 parses a string into a guaranteed valid IPv4
func ParseIPv4(s string) IPv4 {
	ip := net.ParseIP(s)
	Assert(ip != nil, "invalid IP address:", s)
	
	ip4 := ip.To4()
	Assert(ip4 != nil, "not an IPv4 address:", s)
	
	return IPv4{ip4[0], ip4[1], ip4[2], ip4[3]}
}

// String returns the standard dotted decimal representation
func (ip IPv4) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
}

// ToNetIP converts to stdlib net.IP
func (ip IPv4) ToNetIP() net.IP {
	return net.IPv4(ip[0], ip[1], ip[2], ip[3])
}

// IsPrivate checks if this is a private IPv4 address (RFC 1918)
func (ip IPv4) IsPrivate() bool {
	return ip[0] == 10 ||
		(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) ||
		(ip[0] == 192 && ip[1] == 168)
}

// IsLoopback checks if this is a loopback address (127.0.0.0/8)
func (ip IPv4) IsLoopback() bool {
	return ip[0] == 127
}

// IsUnspecified checks if this is 0.0.0.0
func (ip IPv4) IsUnspecified() bool {
	return ip[0] == 0 && ip[1] == 0 && ip[2] == 0 && ip[3] == 0
}

// Bytes returns the raw byte representation
func (ip IPv4) Bytes() []byte {
	return ip[:]
}

// ParseIPv6 parses a string into a guaranteed valid IPv6
func ParseIPv6(s string) IPv6 {
	ip := net.ParseIP(s)
	Assert(ip != nil, "invalid IP address:", s)
	
	// net.ParseIP returns 16-byte representation for IPv6
	Assert(len(ip) == 16 && ip.To4() == nil, "not an IPv6 address:", s)
	
	var result IPv6
	copy(result[:], ip)
	return result
}

// String returns the standard IPv6 representation
func (ip IPv6) String() string {
	return net.IP(ip[:]).String()
}

// ToNetIP converts to stdlib net.IP
func (ip IPv6) ToNetIP() net.IP {
	return net.IP(ip[:])
}

// Bytes returns the raw byte representation
func (ip IPv6) Bytes() []byte {
	return ip[:]
}

// ParseIP parses any IP address and returns a discriminated union
func ParseIP(s string) IP {
	ip := net.ParseIP(s)
	Assert(ip != nil, "invalid IP address:", s)
	
	if ip4 := ip.To4(); ip4 != nil {
		v4 := IPv4{ip4[0], ip4[1], ip4[2], ip4[3]}
		return IP{v4: &v4}
	}
	
	var v6 IPv6
	copy(v6[:], ip)
	return IP{v6: &v6}
}

// IsV4 returns true if this IP contains an IPv4 address
func (ip IP) IsV4() bool {
	return ip.v4 != nil
}

// IsV6 returns true if this IP contains an IPv6 address
func (ip IP) IsV6() bool {
	return ip.v6 != nil
}

// V4 returns the IPv4 address or panics if not IPv4
func (ip IP) V4() IPv4 {
	Assert(ip.v4 != nil, "IP is not IPv4")
	return *ip.v4
}

// V6 returns the IPv6 address or panics if not IPv6
func (ip IP) V6() IPv6 {
	Assert(ip.v6 != nil, "IP is not IPv6")
	return *ip.v6
}

// String returns the string representation of the contained IP
func (ip IP) String() string {
	if ip.v4 != nil {
		return ip.v4.String()
	}
	if ip.v6 != nil {
		return ip.v6.String()
	}
	panic("IP has neither v4 nor v6")
}

// ToNetIP converts to stdlib net.IP
func (ip IP) ToNetIP() net.IP {
	if ip.v4 != nil {
		return ip.v4.ToNetIP()
	}
	if ip.v6 != nil {
		return ip.v6.ToNetIP()
	}
	panic("IP has neither v4 nor v6")
}

// NewIPv4List creates a list of IPv4 addresses from strings
func NewIPv4List(strs []string) []IPv4 {
	result := make([]IPv4, 0, len(strs))
	for _, s := range strs {
		if s == "" {
			continue
		}
		result = append(result, ParseIPv4(s))
	}
	return result
}

// IPv4ListToStrings converts a list of IPv4 to strings
func IPv4ListToStrings(ips []IPv4) []string {
	result := make([]string, len(ips))
	for i, ip := range ips {
		result[i] = ip.String()
	}
	return result
}