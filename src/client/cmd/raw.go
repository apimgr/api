package cmd

import (
	"net/http"
	"strings"

	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/output"
)

func init() {
	register(Command{
		Category: "raw", Name: "request",
		Usage: "raw request <method> <path> [key=value ...]",
		Desc:  "Call any supported server API method directly",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			method, err := requireArg(args, 0, "method")
			if err != nil {
				return err
			}
			path, err := requireArg(args, 1, "path")
			if err != nil {
				return err
			}
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			values := map[string]string{}
			for _, arg := range args[2:] {
				key, value, ok := strings.Cut(arg, "=")
				if ok {
					values[key] = value
				}
			}
			method = strings.ToUpper(method)
			var query map[string]string
			var body map[string]string
			if method == http.MethodGet {
				query = values
			} else {
				body = values
			}
			response, err := c.RequestJSON(method, path, query, body)
			if err != nil {
				return err
			}
			return output.Print(response, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "raw", Name: "get",
		Usage: "raw get <path> [key=value ...]",
		Desc:  "Call any server GET endpoint directly, including endpoints without a dedicated command",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			path, err := requireArg(args, 0, "path")
			if err != nil {
				return err
			}
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			query := map[string]string{}
			for _, kv := range args[1:] {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					continue
				}
				query[k] = v
			}
			body, err := c.Get(path, query)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "raw", Name: "post",
		Usage: "raw post <path> [key=value ...]",
		Desc:  "Call any server POST endpoint directly with a JSON body built from key=value pairs",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			path, err := requireArg(args, 0, "path")
			if err != nil {
				return err
			}
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			payload := map[string]string{}
			for _, kv := range args[1:] {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					continue
				}
				payload[k] = v
			}
			body, err := c.PostJSON(path, payload)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})
}
