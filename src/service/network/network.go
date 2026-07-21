package network

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"

	"github.com/apimgr/api/src/service/parse"
)

// Service provides network utilities: caller IP/header inspection,
// user-agent parsing, MAC vendor lookup, subnet calculation, IPv6
// unique-local-address generation, and random port suggestion.
type Service struct {
	parser *parse.Service
}

// New creates a new Network service
func New() *Service {
	return &Service{parser: parse.New()}
}

// Errors
var (
	ErrInvalidMAC  = fmt.Errorf("invalid MAC address")
	ErrInvalidCIDR = fmt.Errorf("invalid CIDR notation")
)

// CallerInfo describes the caller's resolved IP address, port, and the
// request headers most relevant to identifying/inspecting the caller.
type CallerInfo struct {
	IP      string            `json:"ip"`
	Port    string            `json:"port,omitempty"`
	Headers map[string]string `json:"headers"`
}

// CallerInfo extracts the caller IP/port and inspects caller-identifying
// request headers.
//
// r.RemoteAddr is used as the source of truth for the IP: this server's
// middleware chain (chi's RealIP, see src/server/server.go) is responsible
// for resolving RemoteAddr from a trusted proxy chain when one is
// configured. This service intentionally does NOT re-parse X-Forwarded-For
// itself, since blindly trusting a client-supplied header would let any
// caller spoof its reported IP; it only reflects the already-resolved
// RemoteAddr and surfaces the raw headers for transparency/inspection.
func (s *Service) CallerInfo(r *http.Request) *CallerInfo {
	ip := r.RemoteAddr
	port := ""
	if host, p, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
		port = p
	}

	headerNames := []string{
		"X-Forwarded-For",
		"X-Real-IP",
		"User-Agent",
		"Accept",
		"Accept-Language",
		"Accept-Encoding",
		"Referer",
		"Origin",
		"Host",
	}
	headers := make(map[string]string, len(headerNames))
	for _, name := range headerNames {
		if v := r.Header.Get(name); v != "" {
			headers[name] = v
		}
	}

	return &CallerInfo{IP: ip, Port: port, Headers: headers}
}

// ParseUserAgent parses a User-Agent string into browser/OS/device
// components, delegating to the shared parse service so the logic lives
// in one place.
func (s *Service) ParseUserAgent(ua string) *parse.UserAgent {
	return s.parser.ParseUserAgent(ua)
}

// ouiTable is a small, best-effort, curated table of well-known IEEE OUI
// (Organizationally Unique Identifier) prefixes mapped to vendor names.
// It is intentionally NOT exhaustive (the full IEEE registry has tens of
// thousands of entries) — this stays keyless/offline per IDEA.md and
// covers common virtualization, single-board-computer, and vendor
// prefixes likely to be looked up. Unknown prefixes return "Unknown".
var ouiTable = map[string]string{
	"00:00:0C": "Cisco Systems, Inc.",
	"00:0C:29": "VMware, Inc.",
	"00:50:56": "VMware, Inc.",
	"08:00:27": "PCS Systemtechnik GmbH (Oracle VirtualBox)",
	"B8:27:EB": "Raspberry Pi Foundation",
	"DC:A6:32": "Raspberry Pi Trading Ltd",
	"E4:5F:01": "Raspberry Pi Trading Ltd",
	"00:15:5D": "Microsoft Corporation",
	"00:50:F2": "Microsoft Corporation",
	"3C:5A:B4": "Google, Inc.",
	"F4:F5:D8": "Google, Inc.",
	"A4:77:33": "Amazon Technologies Inc.",
	"FC:65:DE": "Amazon Technologies Inc.",
	"3C:97:0E": "Intel Corporate",
	"E4:CE:8F": "Intel Corporate",
	"00:1B:21": "Intel Corporate",
	"F0:18:98": "Apple, Inc.",
	"AC:BC:32": "Apple, Inc.",
	"3C:15:C2": "Apple, Inc.",
	"00:1A:11": "Google, Inc.",
	"F8:1A:67": "Huawei Technologies Co., Ltd.",
	"34:CE:00": "Xiaomi Communications Co Ltd",
	"18:65:71": "Samsung Electronics Co., Ltd.",
	"5C:0A:5B": "Samsung Electronics Co., Ltd.",
}

// MACVendor looks up the vendor for a MAC address by its OUI (first three
// octets) against the embedded offline table. A syntactically invalid MAC
// returns an error; a valid but unrecognized MAC returns vendor "Unknown"
// with no error.
func (s *Service) MACVendor(mac string) (string, error) {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) < 3 {
		return "", ErrInvalidMAC
	}

	oui := strings.ToUpper(fmt.Sprintf("%02X:%02X:%02X", hw[0], hw[1], hw[2]))
	if vendor, ok := ouiTable[oui]; ok {
		return vendor, nil
	}
	return "Unknown", nil
}

// SubnetInfo describes the result of a subnet calculation for a CIDR block.
type SubnetInfo struct {
	CIDR             string `json:"cidr"`
	Version          int    `json:"version"`
	NetworkAddress   string `json:"network_address"`
	BroadcastAddress string `json:"broadcast_address,omitempty"`
	SubnetMask       string `json:"subnet_mask,omitempty"`
	FirstHost        string `json:"first_host,omitempty"`
	LastHost         string `json:"last_host,omitempty"`
	PrefixLength     int    `json:"prefix_length"`
	TotalAddresses   string `json:"total_addresses"`
	UsableHosts      string `json:"usable_hosts"`
}

// SubnetCalculate computes the network address, broadcast address (IPv4),
// usable host range, and host counts for a CIDR block.
func (s *Service) SubnetCalculate(cidr string) (*SubnetInfo, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, ErrInvalidCIDR
	}

	ones, bits := ipNet.Mask.Size()
	total := new(big.Int).Lsh(big.NewInt(1), uint(bits-ones))

	info := &SubnetInfo{
		CIDR:           cidr,
		NetworkAddress: ipNet.IP.String(),
		PrefixLength:   ones,
		TotalAddresses: total.String(),
	}

	if ip4 := ipNet.IP.To4(); ip4 != nil {
		info.Version = 4
		info.SubnetMask = net.IP(ipNet.Mask).String()

		broadcast := make(net.IP, len(ip4))
		for i := range ip4 {
			broadcast[i] = ip4[i] | ^ipNet.Mask[i]
		}
		info.BroadcastAddress = broadcast.String()

		if ones >= 31 {
			// /31 and /32 have no usable host range (RFC 3021 point-to-point
			// links and single-host routes).
			info.UsableHosts = "0"
			info.FirstHost = ipNet.IP.String()
			info.LastHost = broadcast.String()
			return info, nil
		}

		usable := new(big.Int).Sub(total, big.NewInt(2))
		info.UsableHosts = usable.String()

		first := make(net.IP, len(ip4))
		copy(first, ip4)
		incrementIPv4(first)
		info.FirstHost = first.String()

		last := make(net.IP, len(broadcast))
		copy(last, broadcast)
		decrementIPv4(last)
		info.LastHost = last.String()

		return info, nil
	}

	info.Version = 6
	info.UsableHosts = total.String()
	return info, nil
}

// incrementIPv4 adds one to a 4-byte IPv4 address in place.
func incrementIPv4(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}

// decrementIPv4 subtracts one from a 4-byte IPv4 address in place.
func decrementIPv4(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] != 0 {
			ip[i]--
			break
		}
		ip[i]--
	}
}

// GenerateULA generates an IPv6 Unique Local Address prefix per RFC 4193:
// a randomly generated 40-bit Global ID under the fc00::/7 block with the
// Local bit set (fd00::/8), followed by an all-zero Subnet ID and
// Interface ID, returned as a /48 prefix.
func (s *Service) GenerateULA() (string, error) {
	globalID := make([]byte, 5)
	if _, err := rand.Read(globalID); err != nil {
		return "", fmt.Errorf("failed to generate random Global ID: %w", err)
	}

	addr := make(net.IP, 16)
	addr[0] = 0xfd
	copy(addr[1:6], globalID)

	return fmt.Sprintf("%s/48", addr.String()), nil
}

// unprivilegedPortMin and unprivilegedPortMax bound the IANA dynamic/private
// port range (RFC 6335), which avoids both well-known (0-1023) and
// registered (1024-49151) ports.
const (
	unprivilegedPortMin = 49152
	unprivilegedPortMax = 65535
)

// RandomPort suggests a random port in the unprivileged, dynamic/private
// range (49152-65535).
func (s *Service) RandomPort() (int, error) {
	span := int64(unprivilegedPortMax - unprivilegedPortMin + 1)
	n, err := rand.Int(rand.Reader, big.NewInt(span))
	if err != nil {
		return 0, fmt.Errorf("failed to generate random port: %w", err)
	}
	return unprivilegedPortMin + int(n.Int64()), nil
}
