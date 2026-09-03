package cmd

import (
	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/output"
)

func init() {
	for _, action := range []struct{ name, desc string }{
		{"health", "Show server health status"},
		{"liveness", "Check liveness probe"},
		{"readiness", "Check readiness probe"},
		{"info", "Show system information"},
		{"version", "Show server version"},
		{"endpoints", "List available API endpoints"},
		{"stats", "Show server statistics"},
	} {
		action := action
		register(Command{
			Category: "system",
			Name:     action.name,
			Usage:    "system " + action.name,
			Desc:     action.desc,
			Run: func(c *api.Client, out *OutputOptions, args []string) error {
				body, err := c.Get("/api/v1/system/"+action.name, nil)
				if err != nil {
					return err
				}
				return output.Print(body, output.Format(out.Format))
			},
		})
	}
}
