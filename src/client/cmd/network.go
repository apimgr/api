package cmd

import (
	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/output"
)

func init() {
	register(Command{
		Category: "network", Name: "ip",
		Usage: "network ip",
		Desc:  "Show the caller's IP address as seen by the server",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			body, err := c.Get("/api/v1/network/ip", nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "network", Name: "user-agent",
		Usage: "network user-agent [ua]",
		Desc:  "Parse a User-Agent string (defaults to the CLI's own)",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			query := map[string]string{}
			if ua := argAt(args, 0, ""); ua != "" {
				query["ua"] = ua
			}
			body, err := c.Get("/api/v1/network/user-agent", query)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "network", Name: "mac",
		Usage: "network mac <address>",
		Desc:  "Look up the vendor for a MAC address",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			mac, err := requireArg(args, 0, "address")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/network/mac/"+api.EncodePathSegment(mac), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "network", Name: "subnet",
		Usage: "network subnet <cidr>",
		Desc:  "Calculate subnet details for a CIDR block",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			cidr, err := requireArg(args, 0, "cidr")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/network/subnet", map[string]string{"cidr": cidr})
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "network", Name: "ula",
		Usage: "network ula",
		Desc:  "Generate an IPv6 unique-local-address prefix",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			body, err := c.Get("/api/v1/network/ula", nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "network", Name: "port",
		Usage: "network port",
		Desc:  "Suggest a random free port",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			body, err := c.Get("/api/v1/network/port", nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})
}
