package cmd

import (
	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/output"
)

func init() {
	register(Command{
		Category: "crypto", Name: "bcrypt",
		Usage: "crypto bcrypt <password> [cost]",
		Desc:  "Bcrypt-hash a password",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			password, err := requireArg(args, 0, "password")
			if err != nil {
				return err
			}
			path := "/api/v1/crypto/bcrypt/" + api.EncodePathSegment(password)
			if cost := argAt(args, 1, ""); cost != "" {
				path = "/api/v1/crypto/bcrypt/" + api.EncodePathSegment(cost) + "/" + api.EncodePathSegment(password)
			}
			body, err := c.Get(path, nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "crypto", Name: "bcrypt-verify",
		Usage: "crypto bcrypt-verify <password> <hash>",
		Desc:  "Verify a password against a bcrypt hash",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			password, err := requireArg(args, 0, "password")
			if err != nil {
				return err
			}
			hash, err := requireArg(args, 1, "hash")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/crypto/bcrypt/verify/"+api.EncodePathSegment(password)+"/"+api.EncodePathSegment(hash), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "crypto", Name: "password",
		Usage: "crypto password [length]",
		Desc:  "Generate a random password",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			path := "/api/v1/crypto/password"
			if length := argAt(args, 0, ""); length != "" {
				path += "/" + api.EncodePathSegment(length)
			}
			body, err := c.Get(path, nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "crypto", Name: "password-strength",
		Usage: "crypto password-strength <password>",
		Desc:  "Score a password's strength",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			password, err := requireArg(args, 0, "password")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/crypto/password/strength/"+api.EncodePathSegment(password), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "crypto", Name: "pin",
		Usage: "crypto pin [length]",
		Desc:  "Generate a random numeric PIN",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			path := "/api/v1/crypto/pin"
			if length := argAt(args, 0, ""); length != "" {
				path += "/" + api.EncodePathSegment(length)
			}
			body, err := c.Get(path, nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "crypto", Name: "totp-secret",
		Usage: "crypto totp-secret [issuer]",
		Desc:  "Generate a new TOTP secret",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			query := map[string]string{}
			if issuer := argAt(args, 0, ""); issuer != "" {
				query["issuer"] = issuer
			}
			body, err := c.Get("/api/v1/crypto/totp/secret", query)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "crypto", Name: "totp-code",
		Usage: "crypto totp-code <secret>",
		Desc:  "Generate the current TOTP code for a secret",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			secret, err := requireArg(args, 0, "secret")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/crypto/totp/code/"+api.EncodePathSegment(secret), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "crypto", Name: "totp-verify",
		Usage: "crypto totp-verify <secret> <code>",
		Desc:  "Verify a TOTP code against a secret",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			secret, err := requireArg(args, 0, "secret")
			if err != nil {
				return err
			}
			code, err := requireArg(args, 1, "code")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/crypto/totp/verify/"+api.EncodePathSegment(secret)+"/"+api.EncodePathSegment(code), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "crypto", Name: "random-bytes",
		Usage: "crypto random-bytes <count>",
		Desc:  "Generate random bytes (base64)",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			count, err := requireArg(args, 0, "count")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/crypto/random/bytes/"+api.EncodePathSegment(count), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "crypto", Name: "random-hex",
		Usage: "crypto random-hex <count>",
		Desc:  "Generate random bytes (hex)",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			count, err := requireArg(args, 0, "count")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/crypto/random/hex/"+api.EncodePathSegment(count), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})
}
