package cmd

import (
	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/output"
)

func init() {
	register(Command{
		Category: "text", Name: "uuid",
		Usage: "text uuid [version] [count]",
		Desc:  "Generate one or more UUIDs (default version 4)",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			version := argAt(args, 0, "")
			count := argAt(args, 1, "")
			path := "/api/v1/text/uuid"
			if version != "" {
				path += "/" + api.EncodePathSegment(version)
				if count != "" {
					path += "/" + api.EncodePathSegment(count)
				}
			}
			body, err := c.Get(path, nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "text", Name: "hash",
		Usage: "text hash <algorithm> <input>",
		Desc:  "Hash input with the given algorithm (md5, sha1, sha256, sha512, ...)",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			algorithm, err := requireArg(args, 0, "algorithm")
			if err != nil {
				return err
			}
			input, err := requireArg(args, 1, "input")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/text/hash/"+api.EncodePathSegment(algorithm)+"/"+api.EncodePathSegment(input), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "text", Name: "hash-all",
		Usage: "text hash-all <input>",
		Desc:  "Hash input with every supported algorithm",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			input, err := requireArg(args, 0, "input")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/text/hash/multi/"+api.EncodePathSegment(input), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "text", Name: "encode",
		Usage: "text encode <encoding> <input>",
		Desc:  "Encode input (base64, base64url, base32, hex, url)",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			encoding, err := requireArg(args, 0, "encoding")
			if err != nil {
				return err
			}
			input, err := requireArg(args, 1, "input")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/text/encode/"+api.EncodePathSegment(encoding)+"/"+api.EncodePathSegment(input), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "text", Name: "decode",
		Usage: "text decode <encoding> <input>",
		Desc:  "Decode input (base64, base64url, base32, hex, url)",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			encoding, err := requireArg(args, 0, "encoding")
			if err != nil {
				return err
			}
			input, err := requireArg(args, 1, "input")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/text/decode/"+api.EncodePathSegment(encoding)+"/"+api.EncodePathSegment(input), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "text", Name: "case",
		Usage: "text case <style> <input>",
		Desc:  "Convert case (lower, upper, title, camel, snake, kebab)",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			style, err := requireArg(args, 0, "style")
			if err != nil {
				return err
			}
			input, err := requireArg(args, 1, "input")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/text/case/"+api.EncodePathSegment(style)+"/"+api.EncodePathSegment(input), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "text", Name: "lorem",
		Usage: "text lorem [type] [count]",
		Desc:  "Generate lorem ipsum text (type: words, sentences, paragraphs)",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			kind := argAt(args, 0, "")
			count := argAt(args, 1, "")
			path := "/api/v1/text/lorem"
			if kind != "" {
				path += "/" + api.EncodePathSegment(kind)
				if count != "" {
					path += "/" + api.EncodePathSegment(count)
				}
			}
			body, err := c.Get(path, nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "text", Name: "rot13",
		Usage: "text rot13 <input>",
		Desc:  "Apply ROT13 to input",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			input, err := requireArg(args, 0, "input")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/text/rot13/"+api.EncodePathSegment(input), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "text", Name: "reverse",
		Usage: "text reverse <input>",
		Desc:  "Reverse input text",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			input, err := requireArg(args, 0, "input")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/text/reverse/"+api.EncodePathSegment(input), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "text", Name: "stats",
		Usage: "text stats <input>",
		Desc:  "Show character/word/line statistics for input",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			input, err := requireArg(args, 0, "input")
			if err != nil {
				return err
			}
			body, err := c.PostJSON("/api/v1/text/stats", map[string]string{"text": input})
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})
}
