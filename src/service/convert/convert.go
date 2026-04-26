package convert

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Service provides conversion utilities
type Service struct{}

// New creates a new Convert service
func New() *Service {
	return &Service{}
}

// Base conversions
func (s *Service) BinaryToDecimal(binary string) (int64, error) {
	return strconv.ParseInt(binary, 2, 64)
}

func (s *Service) DecimalToBinary(decimal int64) string {
	return strconv.FormatInt(decimal, 2)
}

func (s *Service) BinaryToHex(binary string) (string, error) {
	decimal, err := s.BinaryToDecimal(binary)
	if err != nil {
		return "", err
	}
	return s.DecimalToHex(decimal), nil
}

func (s *Service) HexToBinary(hexStr string) (string, error) {
	decimal, err := s.HexToDecimal(hexStr)
	if err != nil {
		return "", err
	}
	return s.DecimalToBinary(decimal), nil
}

func (s *Service) HexToDecimal(hexStr string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(hexStr, "0x"), 16, 64)
}

func (s *Service) DecimalToHex(decimal int64) string {
	return fmt.Sprintf("%x", decimal)
}

func (s *Service) OctalToDecimal(octal string) (int64, error) {
	return strconv.ParseInt(octal, 8, 64)
}

func (s *Service) DecimalToOctal(decimal int64) string {
	return strconv.FormatInt(decimal, 8)
}

// Base64 conversions
func (s *Service) Base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func (s *Service) Base64Decode(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}

func (s *Service) Base64URLEncode(data []byte) string {
	return base64.URLEncoding.EncodeToString(data)
}

func (s *Service) Base64URLDecode(encoded string) ([]byte, error) {
	return base64.URLEncoding.DecodeString(encoded)
}

// Hex conversions
func (s *Service) HexEncode(data []byte) string {
	return hex.EncodeToString(data)
}

func (s *Service) HexDecode(hexStr string) ([]byte, error) {
	return hex.DecodeString(hexStr)
}

// String/Byte conversions
func (s *Service) StringToBytes(str string) []byte {
	return []byte(str)
}

func (s *Service) BytesToString(data []byte) string {
	return string(data)
}

// JSON conversions
func (s *Service) JSONPrettyPrint(jsonStr string) (string, error) {
	var obj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return "", err
	}
	pretty, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", err
	}
	return string(pretty), nil
}

func (s *Service) JSONMinify(jsonStr string) (string, error) {
	var obj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return "", err
	}
	minified, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(minified), nil
}

// Unit conversions - Temperature
func (s *Service) CelsiusToFahrenheit(celsius float64) float64 {
	return (celsius * 9 / 5) + 32
}

func (s *Service) FahrenheitToCelsius(fahrenheit float64) float64 {
	return (fahrenheit - 32) * 5 / 9
}

func (s *Service) CelsiusToKelvin(celsius float64) float64 {
	return celsius + 273.15
}

func (s *Service) KelvinToCelsius(kelvin float64) float64 {
	return kelvin - 273.15
}

// Unit conversions - Length
func (s *Service) MilesToKilometers(miles float64) float64 {
	return miles * 1.60934
}

func (s *Service) KilometersToMiles(km float64) float64 {
	return km / 1.60934
}

func (s *Service) FeetToMeters(feet float64) float64 {
	return feet * 0.3048
}

func (s *Service) MetersToFeet(meters float64) float64 {
	return meters / 0.3048
}

func (s *Service) InchesToCentimeters(inches float64) float64 {
	return inches * 2.54
}

func (s *Service) CentimetersToInches(cm float64) float64 {
	return cm / 2.54
}

// Unit conversions - Weight
func (s *Service) PoundsToKilograms(pounds float64) float64 {
	return pounds * 0.453592
}

func (s *Service) KilogramsToPounds(kg float64) float64 {
	return kg / 0.453592
}

func (s *Service) OuncesToGrams(ounces float64) float64 {
	return ounces * 28.3495
}

func (s *Service) GramsToOunces(grams float64) float64 {
	return grams / 28.3495
}

// Unit conversions - Volume
func (s *Service) GallonsToLiters(gallons float64) float64 {
	return gallons * 3.78541
}

func (s *Service) LitersToGallons(liters float64) float64 {
	return liters / 3.78541
}

// Time conversions
func (s *Service) SecondsToMinutes(seconds float64) float64 {
	return seconds / 60
}

func (s *Service) MinutesToSeconds(minutes float64) float64 {
	return minutes * 60
}

func (s *Service) HoursToMinutes(hours float64) float64 {
	return hours * 60
}

func (s *Service) MinutesToHours(minutes float64) float64 {
	return minutes / 60
}

func (s *Service) DaysToHours(days float64) float64 {
	return days * 24
}

func (s *Service) HoursToDays(hours float64) float64 {
	return hours / 24
}
