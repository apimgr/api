package email

import (
	"bufio"
	"encoding/binary"
	"net"
	"os"
	"strconv"
	"strings"
)

// procNetRoute is the Linux kernel's routing table export, overridable in
// tests. On non-Linux platforms the file is absent and gateway detection
// simply yields no candidate, which the auto-detection sweep skips.
var procNetRoute = "/proc/net/route"

// defaultGatewayIP returns the IPv4 address of the default gateway, used
// as auto-detection priority 3 in AI.md PART 17's SMTP host table (a
// containerised app frequently reaches the host's relay through it).
// Returns "" when no default route can be determined.
func defaultGatewayIP() string {
	f, err := os.Open(procNetRoute)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Skip the column header line.
	if !scanner.Scan() {
		return ""
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// Columns: Iface Destination Gateway Flags RefCnt Use Metric Mask
		if len(fields) < 8 {
			continue
		}
		// The default route is the entry whose destination and mask are
		// both zero.
		if fields[1] != "00000000" || fields[7] != "00000000" {
			continue
		}
		ip := parseHexIPv4(fields[2])
		if ip == "" || ip == "0.0.0.0" {
			continue
		}
		return ip
	}
	return ""
}

// parseHexIPv4 decodes the little-endian hex IPv4 encoding used by
// /proc/net/route (e.g. "0101A8C0" is 192.168.1.1).
func parseHexIPv4(hex string) string {
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return ""
	}
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(v))
	return net.IP(buf).String()
}
