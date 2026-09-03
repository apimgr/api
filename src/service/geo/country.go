package geo

import (
	"fmt"

	"github.com/biter777/countries"
)

// CountryInfo represents country reference data resolved by name, alpha-2,
// alpha-3, or numeric code
type CountryInfo struct {
	Name        string   `json:"name"`
	Alpha2      string   `json:"alpha2"`
	Alpha3      string   `json:"alpha3"`
	Numeric     int      `json:"numeric"`
	Capital     string   `json:"capital"`
	Currency    string   `json:"currency"`
	CallingCode []string `json:"calling_code,omitempty"`
	TLD         string   `json:"tld"`
	Region      string   `json:"region"`
}

// Country resolves country reference data (name, alpha-2/alpha-3/numeric
// codes, capital, currency, calling code, TLD, region) from a country
// name, alpha-2 code, or alpha-3 code
func (s *Service) Country(query string) (*CountryInfo, error) {
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	code := countries.ByName(query)
	if !code.IsValid() || code == countries.Unknown {
		return nil, fmt.Errorf("country not found for %q", query)
	}

	callCodes := code.CallCodes()
	calling := make([]string, 0, len(callCodes))
	for _, cc := range callCodes {
		calling = append(calling, cc.String())
	}

	return &CountryInfo{
		Name:        code.String(),
		Alpha2:      code.Alpha2(),
		Alpha3:      code.Alpha3(),
		Numeric:     int(code),
		Capital:     code.Capital().String(),
		Currency:    code.Currency().String(),
		CallingCode: calling,
		TLD:         code.Domain().String(),
		Region:      code.Region().String(),
	}, nil
}
