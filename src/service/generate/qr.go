package generate

import (
	"bytes"
	"fmt"
	"image/png"
	"strings"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
)

// QR renders a QR code as PNG bytes for the given content string. width and
// height set the final rendered pixel size after scaling; content is
// encoded with automatic mode selection and medium (15%) error correction,
// a standard general-purpose default. Callers that need a Wi-Fi QR code
// should build the payload with BuildWifiQRPayload first and pass the
// result as content.
func (s *Service) QR(content string, width, height int) ([]byte, error) {
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if width <= 0 {
		width = 300
	}
	if height <= 0 {
		height = 300
	}

	bc, err := qr.Encode(content, qr.M, qr.Auto)
	if err != nil {
		return nil, fmt.Errorf("failed to encode QR code: %w", err)
	}

	scaled, err := barcode.Scale(bc, width, height)
	if err != nil {
		return nil, fmt.Errorf("failed to scale QR code: %w", err)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return nil, fmt.Errorf("failed to encode QR code PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// wifiQRSpecialChars are the characters the Wi-Fi QR payload format
// (WIFI:T:<security>;S:<ssid>;P:<password>;H:<hidden>;;) requires to be
// backslash-escaped when they appear inside a field value.
const wifiQRSpecialChars = `\;,:"`

// escapeWifiQRField backslash-escapes the characters that are significant
// to the Wi-Fi QR payload format so field values containing them (an SSID
// or password with a semicolon, for example) round-trip correctly.
func escapeWifiQRField(field string) string {
	var b strings.Builder
	for _, r := range field {
		if strings.ContainsRune(wifiQRSpecialChars, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// BuildWifiQRPayload builds the standard Wi-Fi QR code payload string
// (WIFI:T:<security>;S:<ssid>;P:<password>;H:<hidden>;;) understood by
// phone camera apps to offer a "join network" action. security is
// normalized to WPA, WEP, or nopass (case-insensitive; unrecognized values
// default to WPA); an empty password with security "nopass" omits the P
// field, matching the format used for open networks.
func BuildWifiQRPayload(ssid, password, security string, hidden bool) (string, error) {
	if ssid == "" {
		return "", fmt.Errorf("ssid is required")
	}

	sec := strings.ToUpper(strings.TrimSpace(security))
	switch sec {
	case "WPA", "WPA2", "WPA3":
		sec = "WPA"
	case "WEP":
		sec = "WEP"
	case "NOPASS", "NONE", "":
		sec = "nopass"
	default:
		sec = "WPA"
	}

	var b strings.Builder
	b.WriteString("WIFI:T:")
	b.WriteString(sec)
	b.WriteString(";S:")
	b.WriteString(escapeWifiQRField(ssid))
	if sec != "nopass" {
		if password == "" {
			return "", fmt.Errorf("password is required for security type %q", sec)
		}
		b.WriteString(";P:")
		b.WriteString(escapeWifiQRField(password))
	}
	if hidden {
		b.WriteString(";H:true")
	}
	b.WriteString(";;")
	return b.String(), nil
}
