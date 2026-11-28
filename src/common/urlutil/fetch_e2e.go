//go:build e2e

package urlutil

import (
	"net"
	"os"
	"strings"
)

func init() {
	e2eFixtureIP = net.ParseIP(strings.TrimSpace(os.Getenv("E2E_FIXTURE_IP")))
}
