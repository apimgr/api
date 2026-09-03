package convert

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CurrencyResult is the outcome of a live currency conversion
type CurrencyResult struct {
	Amount float64 `json:"amount"`
	From   string  `json:"from"`
	To     string  `json:"to"`
	Rate   float64 `json:"rate"`
	Result float64 `json:"result"`
	Date   string  `json:"date"`
}

// currencyHTTPClient is a shared client with a hard timeout for the
// keyless Frankfurter provider (ECB reference rates, no API key, free)
var currencyHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
}

const currencyEndpoint = "https://api.frankfurter.dev/v1/latest"

// frankfurterResult mirrors the Frankfurter "latest" API response
type frankfurterResult struct {
	Amount float64            `json:"amount"`
	Base   string             `json:"base"`
	Date   string             `json:"date"`
	Rates  map[string]float64 `json:"rates"`
}

// ConvertCurrency converts amount from one ISO 4217 currency code to
// another using the free, keyless Frankfurter API (European Central Bank
// reference rates, updated daily on ECB business days)
func (s *Service) ConvertCurrency(amount float64, from, to string) (CurrencyResult, error) {
	from = strings.ToUpper(strings.TrimSpace(from))
	to = strings.ToUpper(strings.TrimSpace(to))
	if from == "" || to == "" {
		return CurrencyResult{}, fmt.Errorf("from and to currency codes are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("base", from)
	params.Set("symbols", to)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, currencyEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return CurrencyResult{}, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := currencyHTTPClient.Do(req)
	if err != nil {
		return CurrencyResult{}, fmt.Errorf("currency provider request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return CurrencyResult{}, fmt.Errorf("currency provider returned status %d (check currency codes)", resp.StatusCode)
	}

	var result frankfurterResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return CurrencyResult{}, fmt.Errorf("failed to decode currency provider response: %w", err)
	}

	rate, ok := result.Rates[to]
	if !ok {
		return CurrencyResult{}, fmt.Errorf("no rate returned for %s", to)
	}

	return CurrencyResult{
		Amount: amount,
		From:   from,
		To:     to,
		Rate:   rate,
		Result: amount * rate,
		Date:   result.Date,
	}, nil
}
