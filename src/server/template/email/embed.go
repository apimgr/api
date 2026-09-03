// Package emailtemplates embeds the default email notification templates
// shipped with the binary (AI.md PART 17 "Template Storage"). Each file is
// named "{event}.txt" and holds the plain-text "Subject: ...\n---\nbody"
// format described in PART 17 "Template Format". Operators override any of
// them by placing a same-named file under {config_dir}/template/email/ -
// the custom file wins when present, and deleting it resets to this
// embedded default (src/email.LoadTemplate implements that resolution).
package emailtemplates

import "embed"

// Defaults holds the embedded default template files, keyed by filename
// (e.g. "security_alert.txt") relative to this directory.
//
//go:embed *.txt
var Defaults embed.FS
