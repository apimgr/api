package convert

import (
	"testing"

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
