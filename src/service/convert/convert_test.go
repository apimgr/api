package convert

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBinaryDecimalHex(t *testing.T) {
	s := New()

	dec, err := s.BinaryToDecimal("1010")
	require.NoError(t, err)
	assert.Equal(t, int64(10), dec)

	_, err = s.BinaryToDecimal("not-binary")
	assert.Error(t, err)

	assert.Equal(t, "1010", s.DecimalToBinary(10))
	assert.Equal(t, "0", s.DecimalToBinary(0))

	hexStr, err := s.BinaryToHex("1010")
	require.NoError(t, err)
	assert.Equal(t, "a", hexStr)

	_, err = s.BinaryToHex("bad")
	assert.Error(t, err)

	bin, err := s.HexToBinary("a")
	require.NoError(t, err)
	assert.Equal(t, "1010", bin)

	bin, err = s.HexToBinary("0xa")
	require.NoError(t, err)
	assert.Equal(t, "1010", bin)

	_, err = s.HexToBinary("zz")
	assert.Error(t, err)

	dec, err = s.HexToDecimal("ff")
	require.NoError(t, err)
	assert.Equal(t, int64(255), dec)

	dec, err = s.HexToDecimal("0xff")
	require.NoError(t, err)
	assert.Equal(t, int64(255), dec)

	_, err = s.HexToDecimal("zz")
	assert.Error(t, err)

	assert.Equal(t, "ff", s.DecimalToHex(255))
}

func TestOctal(t *testing.T) {
	s := New()

	dec, err := s.OctalToDecimal("17")
	require.NoError(t, err)
	assert.Equal(t, int64(15), dec)

	_, err = s.OctalToDecimal("bad")
	assert.Error(t, err)

	assert.Equal(t, "17", s.DecimalToOctal(15))
}

func TestBase64(t *testing.T) {
	s := New()
	data := []byte("hello world")

	encoded := s.Base64Encode(data)
	assert.Equal(t, "aGVsbG8gd29ybGQ=", encoded)

	decoded, err := s.Base64Decode(encoded)
	require.NoError(t, err)
	assert.Equal(t, data, decoded)

	_, err = s.Base64Decode("not valid base64!!")
	assert.Error(t, err)

	urlEncoded := s.Base64URLEncode(data)
	urlDecoded, err := s.Base64URLDecode(urlEncoded)
	require.NoError(t, err)
	assert.Equal(t, data, urlDecoded)

	_, err = s.Base64URLDecode("not valid!!")
	assert.Error(t, err)
}

func TestHexEncodeDecode(t *testing.T) {
	s := New()
	data := []byte("hi")

	encoded := s.HexEncode(data)
	assert.Equal(t, "6869", encoded)

	decoded, err := s.HexDecode(encoded)
	require.NoError(t, err)
	assert.Equal(t, data, decoded)

	_, err = s.HexDecode("zz")
	assert.Error(t, err)
}

func TestStringBytes(t *testing.T) {
	s := New()
	assert.Equal(t, []byte("hello"), s.StringToBytes("hello"))
	assert.Equal(t, "hello", s.BytesToString([]byte("hello")))
	// Round trip on empty input.
	assert.Equal(t, []byte{}, s.StringToBytes(""))
	assert.Equal(t, "", s.BytesToString(nil))
}

func TestJSONPrettyMinify(t *testing.T) {
	s := New()

	pretty, err := s.JSONPrettyPrint(`{"a":1,"b":2}`)
	require.NoError(t, err)
	assert.Equal(t, "{\n  \"a\": 1,\n  \"b\": 2\n}", pretty)

	_, err = s.JSONPrettyPrint("not json")
	assert.Error(t, err)

	minified, err := s.JSONMinify("{\n  \"a\": 1\n}")
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, minified)

	_, err = s.JSONMinify("not json")
	assert.Error(t, err)
}

func TestTemperatureConversions(t *testing.T) {
	s := New()
	assert.InDelta(t, 32.0, s.CelsiusToFahrenheit(0), 0.0001)
	assert.InDelta(t, 212.0, s.CelsiusToFahrenheit(100), 0.0001)
	assert.InDelta(t, 0.0, s.FahrenheitToCelsius(32), 0.0001)
	assert.InDelta(t, 100.0, s.FahrenheitToCelsius(212), 0.0001)
	assert.InDelta(t, 273.15, s.CelsiusToKelvin(0), 0.0001)
	assert.InDelta(t, 0.0, s.KelvinToCelsius(273.15), 0.0001)
}

func TestLengthConversions(t *testing.T) {
	s := New()
	assert.InDelta(t, 1.60934, s.MilesToKilometers(1), 0.0001)
	assert.InDelta(t, 1.0, s.KilometersToMiles(1.60934), 0.0001)
	assert.InDelta(t, 0.3048, s.FeetToMeters(1), 0.0001)
	assert.InDelta(t, 1.0, s.MetersToFeet(0.3048), 0.0001)
	assert.InDelta(t, 2.54, s.InchesToCentimeters(1), 0.0001)
	assert.InDelta(t, 1.0, s.CentimetersToInches(2.54), 0.0001)
}

func TestWeightConversions(t *testing.T) {
	s := New()
	assert.InDelta(t, 0.453592, s.PoundsToKilograms(1), 0.0001)
	assert.InDelta(t, 1.0, s.KilogramsToPounds(0.453592), 0.0001)
	assert.InDelta(t, 28.3495, s.OuncesToGrams(1), 0.0001)
	assert.InDelta(t, 1.0, s.GramsToOunces(28.3495), 0.0001)
}

func TestVolumeConversions(t *testing.T) {
	s := New()
	assert.InDelta(t, 3.78541, s.GallonsToLiters(1), 0.0001)
	assert.InDelta(t, 1.0, s.LitersToGallons(3.78541), 0.0001)
}

func TestTimeConversions(t *testing.T) {
	s := New()
	assert.InDelta(t, 1.0, s.SecondsToMinutes(60), 0.0001)
	assert.InDelta(t, 60.0, s.MinutesToSeconds(1), 0.0001)
	assert.InDelta(t, 60.0, s.HoursToMinutes(1), 0.0001)
	assert.InDelta(t, 1.0, s.MinutesToHours(60), 0.0001)
	assert.InDelta(t, 24.0, s.DaysToHours(1), 0.0001)
	assert.InDelta(t, 1.0, s.HoursToDays(24), 0.0001)
}

func TestAreaConversions(t *testing.T) {
	s := New()
	assert.InDelta(t, 10.7639104167, s.SquareMetersToSquareFeet(1), 0.0001)
	assert.InDelta(t, 1.0, s.SquareFeetToSquareMeters(10.7639104167), 0.0001)
	assert.InDelta(t, 0.404685642, s.AcresToHectares(1), 0.0001)
	assert.InDelta(t, 1.0, s.HectaresToAcres(0.404685642), 0.0001)
}

func TestDataSizeConversions(t *testing.T) {
	s := New()
	assert.InDelta(t, 1.0, s.BytesToKilobytes(1000), 0.0001)
	assert.InDelta(t, 1000.0, s.KilobytesToBytes(1), 0.0001)
	assert.InDelta(t, 1.0, s.KilobytesToMegabytes(1000), 0.0001)
	assert.InDelta(t, 1000.0, s.MegabytesToKilobytes(1), 0.0001)
	assert.InDelta(t, 1.0, s.MegabytesToGigabytes(1000), 0.0001)
	assert.InDelta(t, 1000.0, s.GigabytesToMegabytes(1), 0.0001)
	assert.InDelta(t, 1.0, s.GigabytesToTerabytes(1000), 0.0001)
	assert.InDelta(t, 1000.0, s.TerabytesToGigabytes(1), 0.0001)
}

func TestEnergyConversions(t *testing.T) {
	s := New()
	assert.InDelta(t, 1.0, s.JoulesToCalories(4.184), 0.0001)
	assert.InDelta(t, 4.184, s.CaloriesToJoules(1), 0.0001)
	assert.InDelta(t, 1.0, s.JoulesToKilowattHours(3600000), 0.0001)
	assert.InDelta(t, 3600000.0, s.KilowattHoursToJoules(1), 0.0001)
}

func TestPressureConversions(t *testing.T) {
	s := New()
	assert.InDelta(t, 1.0, s.PascalsToBar(100000), 0.0001)
	assert.InDelta(t, 100000.0, s.BarToPascals(1), 0.0001)
	assert.InDelta(t, 1.0, s.PascalsToPSI(6894.757293168), 0.0001)
	assert.InDelta(t, 6894.757293168, s.PSIToPascals(1), 0.0001)
	assert.InDelta(t, 1.0, s.PascalsToAtmospheres(101325), 0.0001)
	assert.InDelta(t, 101325.0, s.AtmospheresToPascals(1), 0.0001)
}

func TestSpeedConversions(t *testing.T) {
	s := New()
	assert.InDelta(t, 1.609344, s.MphToKmh(1), 0.0001)
	assert.InDelta(t, 1.0, s.KmhToMph(1.609344), 0.0001)
	assert.InDelta(t, 3.6, s.MsToKmh(1), 0.0001)
	assert.InDelta(t, 1.0, s.KmhToMs(3.6), 0.0001)
	assert.InDelta(t, 1.852, s.KnotsToKmh(1), 0.0001)
	assert.InDelta(t, 1.0, s.KmhToKnots(1.852), 0.0001)
}

// HexToRGB covers valid input with and without the "#" prefix, whitespace
// tolerance, and malformed hex strings.
func TestHexToRGB(t *testing.T) {
	s := New()

	tests := []struct {
		name    string
		input   string
		want    RGB
		wantErr bool
	}{
		{name: "with hash", input: "#ff0080", want: RGB{R: 255, G: 0, B: 128}},
		{name: "without hash", input: "00ff00", want: RGB{R: 0, G: 255, B: 0}},
		{name: "whitespace tolerant", input: "  #000000  ", want: RGB{R: 0, G: 0, B: 0}},
		{name: "wrong length", input: "fff", wantErr: true},
		{name: "invalid red", input: "zz0000", wantErr: true},
		{name: "invalid green", input: "00zz00", wantErr: true},
		{name: "invalid blue", input: "0000zz", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.HexToRGB(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// RGBToHex covers valid components and out-of-range values.
func TestRGBToHex(t *testing.T) {
	s := New()

	tests := []struct {
		name    string
		input   RGB
		want    string
		wantErr bool
	}{
		{name: "valid", input: RGB{R: 255, G: 0, B: 128}, want: "#ff0080"},
		{name: "black", input: RGB{R: 0, G: 0, B: 0}, want: "#000000"},
		{name: "red out of range", input: RGB{R: 256, G: 0, B: 0}, wantErr: true},
		{name: "negative component", input: RGB{R: 0, G: -1, B: 0}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.RGBToHex(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// RGBToHSL covers grayscale (max==min), a saturated color, and
// out-of-range input.
func TestRGBToHSL(t *testing.T) {
	s := New()

	tests := []struct {
		name    string
		input   RGB
		want    HSL
		wantErr bool
	}{
		{name: "pure red", input: RGB{R: 255, G: 0, B: 0}, want: HSL{H: 0, S: 100, L: 50}},
		{name: "gray", input: RGB{R: 128, G: 128, B: 128}, want: HSL{H: 0, S: 0, L: 50}},
		{name: "white", input: RGB{R: 255, G: 255, B: 255}, want: HSL{H: 0, S: 0, L: 100}},
		{name: "out of range", input: RGB{R: 300, G: 0, B: 0}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.RGBToHSL(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// HSLToRGB covers a saturated color, the sat==0 grayscale shortcut, both
// branches of the l<0.5/l>=0.5 split, and out-of-range input.
func TestHSLToRGB(t *testing.T) {
	s := New()

	tests := []struct {
		name    string
		input   HSL
		want    RGB
		wantErr bool
	}{
		{name: "pure red", input: HSL{H: 0, S: 100, L: 50}, want: RGB{R: 255, G: 0, B: 0}},
		{name: "grayscale shortcut", input: HSL{H: 0, S: 0, L: 50}, want: RGB{R: 128, G: 128, B: 128}},
		{name: "light lightness branch", input: HSL{H: 120, S: 50, L: 75}, want: RGB{R: 159, G: 223, B: 159}},
		{name: "dark lightness branch", input: HSL{H: 240, S: 50, L: 25}, want: RGB{R: 32, G: 32, B: 96}},
		{name: "h out of range", input: HSL{H: 400, S: 50, L: 50}, wantErr: true},
		{name: "s out of range", input: HSL{H: 0, S: 150, L: 50}, wantErr: true},
		{name: "l out of range", input: HSL{H: 0, S: 50, L: -1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.HSLToRGB(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// currencyRedirectTransport rewrites every outbound request to target the
// given httptest server, regardless of the original host, so the
// hardcoded currencyEndpoint constant can be exercised against a local
// mock.
type currencyRedirectTransport struct {
	targetURL *url.URL
}

func (rt *currencyRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.targetURL.Scheme
	req.URL.Host = rt.targetURL.Host
	req.Host = rt.targetURL.Host
	return http.DefaultTransport.RoundTrip(req)
}

// withMockCurrencyClient swaps the package-level currencyHTTPClient for
// one that redirects all requests to srv, restoring the original on
// cleanup.
func withMockCurrencyClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("failed to parse test server url: %v", err)
	}
	original := currencyHTTPClient
	currencyHTTPClient = &http.Client{
		Timeout:   10 * time.Second,
		Transport: &currencyRedirectTransport{targetURL: target},
	}
	t.Cleanup(func() {
		currencyHTTPClient = original
	})
}

// ConvertCurrency covers a successful conversion, missing currency codes,
// a non-200 provider response, malformed JSON, and a missing rate in the
// response.
func TestConvertCurrency(t *testing.T) {
	s := New()

	tests := []struct {
		name      string
		amount    float64
		from      string
		to        string
		status    int
		body      string
		wantErr   bool
		wantRate  float64
		wantValue float64
	}{
		{
			name:      "successful conversion",
			amount:    10,
			from:      "usd",
			to:        "eur",
			status:    http.StatusOK,
			body:      `{"amount":1,"base":"USD","date":"2024-01-01","rates":{"EUR":0.9}}`,
			wantRate:  0.9,
			wantValue: 9.0,
		},
		{
			name:    "missing from code",
			amount:  10,
			from:    "  ",
			to:      "EUR",
			wantErr: true,
		},
		{
			name:    "non-200 status",
			amount:  10,
			from:    "USD",
			to:      "EUR",
			status:  http.StatusBadRequest,
			body:    "",
			wantErr: true,
		},
		{
			name:    "malformed json",
			amount:  10,
			from:    "USD",
			to:      "EUR",
			status:  http.StatusOK,
			body:    "{not json",
			wantErr: true,
		},
		{
			name:    "rate missing from response",
			amount:  10,
			from:    "USD",
			to:      "XYZ",
			status:  http.StatusOK,
			body:    `{"amount":1,"base":"USD","date":"2024-01-01","rates":{}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			withMockCurrencyClient(t, srv)

			result, err := s.ConvertCurrency(tt.amount, tt.from, tt.to)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tt.wantRate, result.Rate, 0.0001)
			assert.InDelta(t, tt.wantValue, result.Result, 0.0001)
			assert.Equal(t, "USD", result.From)
			assert.Equal(t, "EUR", result.To)
		})
	}
}

