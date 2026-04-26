package parse

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Service provides parsing utilities
type Service struct{}

// New creates a new Parse service
func New() *Service {
	return &Service{}
}

// JSON parsing
func (s *Service) ParseJSON(jsonStr string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &result)
	return result, err
}

func (s *Service) ParseJSONArray(jsonStr string) ([]interface{}, error) {
	var result []interface{}
	err := json.Unmarshal([]byte(jsonStr), &result)
	return result, err
}

// XML parsing
func (s *Service) ParseXML(xmlStr string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := xml.Unmarshal([]byte(xmlStr), &result)
	return result, err
}

// URL parsing
func (s *Service) ParseURL(urlStr string) (*URLParts, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	
	return &URLParts{
		Scheme:   u.Scheme,
		Host:     u.Host,
		Hostname: u.Hostname(),
		Port:     u.Port(),
		Path:     u.Path,
		Query:    u.RawQuery,
		Fragment: u.Fragment,
		User:     u.User.Username(),
	}, nil
}

type URLParts struct {
	Scheme   string `json:"scheme"`
	Host     string `json:"host"`
	Hostname string `json:"hostname"`
	Port     string `json:"port"`
	Path     string `json:"path"`
	Query    string `json:"query"`
	Fragment string `json:"fragment"`
	User     string `json:"user"`
}

// Query string parsing
func (s *Service) ParseQueryString(query string) (map[string][]string, error) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return nil, err
	}
	return values, nil
}

// Date/Time parsing
func (s *Service) ParseDateTime(dateStr string) (time.Time, error) {
	// Try common formats
	formats := []string{
		time.RFC3339,
		time.RFC1123,
		time.RFC822,
		"2006-01-02",
		"2006-01-02 15:04:05",
		"01/02/2006",
		"01-02-2006",
		"2006/01/02",
	}
	
	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}
	
	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

// Number parsing
func (s *Service) ParseInt(str string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(str), 10, 64)
}

func (s *Service) ParseFloat(str string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(str), 64)
}

func (s *Service) ParseBool(str string) (bool, error) {
	str = strings.ToLower(strings.TrimSpace(str))
	switch str {
	case "true", "yes", "1", "on":
		return true, nil
	case "false", "no", "0", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %s", str)
	}
}

// CSV parsing (simple)
func (s *Service) ParseCSVLine(line string) []string {
	var result []string
	var current strings.Builder
	inQuotes := false
	
	for i := 0; i < len(line); i++ {
		char := line[i]
		
		switch char {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if !inQuotes {
				result = append(result, current.String())
				current.Reset()
			} else {
				current.WriteByte(char)
			}
		default:
			current.WriteByte(char)
		}
	}
	
	result = append(result, current.String())
	return result
}

// User-Agent parsing (basic)
func (s *Service) ParseUserAgent(ua string) *UserAgent {
	result := &UserAgent{
		Raw: ua,
	}
	
	// Detect browser
	if strings.Contains(ua, "Chrome") {
		result.Browser = "Chrome"
	} else if strings.Contains(ua, "Firefox") {
		result.Browser = "Firefox"
	} else if strings.Contains(ua, "Safari") && !strings.Contains(ua, "Chrome") {
		result.Browser = "Safari"
	} else if strings.Contains(ua, "Edge") {
		result.Browser = "Edge"
	} else if strings.Contains(ua, "MSIE") || strings.Contains(ua, "Trident") {
		result.Browser = "Internet Explorer"
	}
	
	// Detect OS
	if strings.Contains(ua, "Windows") {
		result.OS = "Windows"
	} else if strings.Contains(ua, "Mac OS") {
		result.OS = "macOS"
	} else if strings.Contains(ua, "Linux") {
		result.OS = "Linux"
	} else if strings.Contains(ua, "Android") {
		result.OS = "Android"
	} else if strings.Contains(ua, "iOS") || strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") {
		result.OS = "iOS"
	}
	
	// Detect device type
	if strings.Contains(ua, "Mobile") || strings.Contains(ua, "Android") || strings.Contains(ua, "iPhone") {
		result.Device = "Mobile"
	} else if strings.Contains(ua, "Tablet") || strings.Contains(ua, "iPad") {
		result.Device = "Tablet"
	} else {
		result.Device = "Desktop"
	}
	
	return result
}

type UserAgent struct {
	Raw     string `json:"raw"`
	Browser string `json:"browser"`
	OS      string `json:"os"`
	Device  string `json:"device"`
}

// Email parsing
func (s *Service) ParseEmail(email string) (*EmailParts, error) {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid email format")
	}
	
	return &EmailParts{
		Local:  parts[0],
		Domain: parts[1],
		Full:   email,
	}, nil
}

type EmailParts struct {
	Local  string `json:"local"`
	Domain string `json:"domain"`
	Full   string `json:"full"`
}
