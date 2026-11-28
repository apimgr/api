//go:build e2e

package network

import (
	"net"
	"os"
	"strings"
)

func init() {
	e2eFixtureIP = net.ParseIP(strings.TrimSpace(os.Getenv("E2E_FIXTURE_IP")))
}
