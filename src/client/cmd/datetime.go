package cmd

import (
	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/output"
)

func init() {
	register(Command{
		Category: "datetime", Name: "now",
		Usage: "datetime now [timezone]",
		Desc:  "Show the current time, optionally in a timezone",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			path := "/api/v1/datetime/now"
			if tz := argAt(args, 0, ""); tz != "" {
				path += "/" + api.EncodePathSegment(tz)
			}
			body, err := c.Get(path, nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "datetime", Name: "timestamp",
		Usage: "datetime timestamp",
		Desc:  "Show the current Unix timestamp",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			body, err := c.Get("/api/v1/datetime/timestamp", nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "datetime", Name: "convert",
		Usage: "datetime convert <timestamp> [timezone]",
		Desc:  "Convert a Unix timestamp to a readable date",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			ts, err := requireArg(args, 0, "timestamp")
			if err != nil {
				return err
			}
			path := "/api/v1/datetime/convert/" + api.EncodePathSegment(ts)
			if tz := argAt(args, 1, ""); tz != "" {
				path += "/" + api.EncodePathSegment(tz)
			}
			body, err := c.Get(path, nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "datetime", Name: "to-unix",
		Usage: "datetime to-unix <datetime>",
		Desc:  "Convert a readable date to a Unix timestamp",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			datetime, err := requireArg(args, 0, "datetime")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/datetime/to-unix/"+api.EncodePathSegment(datetime), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "datetime", Name: "add",
		Usage: "datetime add <timestamp> <duration>",
		Desc:  "Add a duration to a timestamp",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			ts, err := requireArg(args, 0, "timestamp")
			if err != nil {
				return err
			}
			duration, err := requireArg(args, 1, "duration")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/datetime/add/"+api.EncodePathSegment(ts)+"/"+api.EncodePathSegment(duration), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "datetime", Name: "diff",
		Usage: "datetime diff <timestamp1> <timestamp2>",
		Desc:  "Show the difference between two timestamps",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			ts1, err := requireArg(args, 0, "timestamp1")
			if err != nil {
				return err
			}
			ts2, err := requireArg(args, 1, "timestamp2")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/datetime/diff/"+api.EncodePathSegment(ts1)+"/"+api.EncodePathSegment(ts2), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "datetime", Name: "timezones",
		Usage: "datetime timezones",
		Desc:  "List available timezones",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			body, err := c.Get("/api/v1/datetime/timezones", nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "datetime", Name: "timezone",
		Usage: "datetime timezone <timezone>",
		Desc:  "Show information about a timezone",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			tz, err := requireArg(args, 0, "timezone")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/datetime/timezone/"+api.EncodePathSegment(tz), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})

	register(Command{
		Category: "datetime", Name: "timezone-convert",
		Usage: "datetime timezone-convert <timestamp> <from> <to>",
		Desc:  "Convert a timestamp from one timezone to another",
		Run: func(c *api.Client, out *OutputOptions, args []string) error {
			ts, err := requireArg(args, 0, "timestamp")
			if err != nil {
				return err
			}
			from, err := requireArg(args, 1, "from")
			if err != nil {
				return err
			}
			to, err := requireArg(args, 2, "to")
			if err != nil {
				return err
			}
			body, err := c.Get("/api/v1/datetime/timezone/convert/"+api.EncodePathSegment(ts)+"/"+api.EncodePathSegment(from)+"/"+api.EncodePathSegment(to), nil)
			if err != nil {
				return err
			}
			return output.Print(body, output.Format(out.Format))
		},
	})
}
