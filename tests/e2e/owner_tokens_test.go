//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

var (
	csrfInputPattern     = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	e2eOwnerTokenPattern = regexp.MustCompile(`own_[A-Za-z0-9]{32}`)
)

func newOwnerTokenClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: pageTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func readResponse(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read E2E response: %v", err)
	}
	return string(body)
}

func ownerPage(t *testing.T, client *http.Client) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, suite.localURL+"/server/preferences/tokens", nil)
	if err != nil {
		t.Fatalf("build owner-token page request: %v", err)
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) e2e-suite")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("load owner-token page: %v", err)
	}
	body := readResponse(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("owner-token page returned HTTP %d", response.StatusCode)
	}
	return body
}

func postOwnerForm(t *testing.T, client *http.Client, route string, fields url.Values) (*http.Response, string) {
	t.Helper()
	page := ownerPage(t, client)
	match := csrfInputPattern.FindStringSubmatch(page)
	if len(match) != 2 {
		t.Fatal("owner-token page has no CSRF form token")
	}
	fields.Set("csrf_token", match[1])
	request, err := http.NewRequest(http.MethodPost, suite.localURL+route, strings.NewReader(fields.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("POST %s: %v", route, err)
	}
	return response, readResponse(t, response)
}

func ownerCookie(t *testing.T, client *http.Client) *http.Cookie {
	t.Helper()
	parsed, err := url.Parse(suite.localURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range client.Jar.Cookies(parsed) {
		if strings.Contains(cookie.Name, "_owner_token_") && strings.HasPrefix(cookie.Value, "own_") {
			return cookie
		}
	}
	t.Fatal("owner-token form did not set the owner_token cookie")
	return nil
}

func getOwnerTokenPageAfterRedirect(t *testing.T, client *http.Client, response *http.Response) string {
	t.Helper()
	location := response.Header.Get("Location")
	if response.StatusCode != http.StatusSeeOther || location == "" {
		t.Fatalf("owner-token action returned HTTP %d with no 303 Location", response.StatusCode)
	}
	response, err := client.Get(suite.localURL + location)
	if err != nil {
		t.Fatalf("follow owner-token redirect: %v", err)
	}
	body := readResponse(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("owner-token redirect returned HTTP %d", response.StatusCode)
	}
	return body
}

func apiTokenRequest(t *testing.T, client *http.Client, method, route, token string, payload any) (*http.Response, []byte) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, suite.localURL+route, body)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := httpClient().Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, route, err)
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s %s response: %v", method, route, err)
	}
	return response, encoded
}

func mcpToolCall(t *testing.T, token string) *http.Response {
	return mcpToolCallNamed(t, token, "get_api_api_version_text_uuid")
}

func mcpToolCallNamed(t *testing.T, token, tool string) *http.Response {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`, tool)
	request, err := http.NewRequest(http.MethodPost, suite.localURL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("MCP-Protocol-Version", "2026-07-28")
	request.Header.Set("Mcp-Method", "tools/call")
	request.Header.Set("Mcp-Name", tool)
	request.Header.Set("Accept", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := httpClient().Do(request)
	if err != nil {
		t.Fatalf("MCP tools/call: %v", err)
	}
	return response
}

func TestTier1OwnerTokenFormIssueAndRestore(t *testing.T) {
	client := newOwnerTokenClient(t)
	page := ownerPage(t, client)
	if !strings.Contains(page, "Use an existing token") || !strings.Contains(page, "New token") {
		t.Fatal("unowned tokens page does not show New token and restore forms")
	}
	response, _ := postOwnerForm(t, client, "/server/preferences/token/create", url.Values{})
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("New token form returned HTTP %d, want 303", response.StatusCode)
	}
	primary := ownerCookie(t, client).Value
	if !e2eOwnerTokenPattern.MatchString(primary) {
		t.Fatal("New token form set a malformed primary owner token cookie")
	}
	page = getOwnerTokenPageAfterRedirect(t, client, response)
	if !strings.Contains(page, "Primary token") || !strings.Contains(page, primary) {
		t.Fatal("new primary token was not displayed once on the redirected page")
	}
	response, _ = postOwnerForm(t, client, "/server/preferences/token/reveal", url.Values{})
	page = getOwnerTokenPageAfterRedirect(t, client, response)
	if !strings.Contains(page, primary) {
		t.Fatal("primary token reveal did not display the saved token")
	}

	newBrowser := newOwnerTokenClient(t)
	page = ownerPage(t, newBrowser)
	values := url.Values{"token": {primary}}
	response, _ = postOwnerForm(t, newBrowser, "/server/preferences/token/restore", values)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("restore form returned HTTP %d, want 303", response.StatusCode)
	}
	if restored := ownerCookie(t, newBrowser); restored.Value != primary {
		t.Fatal("restore form did not set the supplied primary token cookie")
	}
	page = getOwnerTokenPageAfterRedirect(t, newBrowser, response)
	if !strings.Contains(page, "Primary token") {
		t.Fatal("restored owner page did not render the primary token card")
	}
}

func TestTier1OwnerTokenScopesManagementAndMCP(t *testing.T) {
	response, body := apiTokenRequest(t, httpClient(), http.MethodPost, "/api/v1/server/preferences/tokens/primary", "", nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("primary issue API returned HTTP %d", response.StatusCode)
	}
	var issued struct {
		Data struct {
			Token string `json:"owner_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &issued); err != nil || !e2eOwnerTokenPattern.MatchString(issued.Data.Token) {
		t.Fatal("primary issue API did not return an owner token")
	}
	primary := issued.Data.Token
	response = mcpToolCall(t, primary)
	primaryMCPBody := readResponse(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(primaryMCPBody, `"result"`) || strings.Contains(primaryMCPBody, `"isError":true`) {
		t.Fatalf("primary owner token could not call an MCP read tool (HTTP %d): %s", response.StatusCode, primaryMCPBody)
	}
	response, _ = apiTokenRequest(t, httpClient(), http.MethodPost, "/api/v1/server/preferences/tokens/primary", primary, nil)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("issuing another owner with a valid token returned HTTP %d, want 409", response.StatusCode)
	}

	response, body = apiTokenRequest(t, httpClient(), http.MethodPost, "/api/v1/server/preferences/tokens", primary, map[string]string{"name": "read-only", "scope": "read", "expires": "never"})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("linked token creation returned HTTP %d", response.StatusCode)
	}
	var linked struct {
		Data struct {
			ID    string `json:"id"`
			Scope string `json:"scope"`
			Token string `json:"linked_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &linked); err != nil {
		t.Fatal("decode linked token response:", err)
	}
	if linked.Data.ID == "" || linked.Data.Scope != "read" || !e2eOwnerTokenPattern.MatchString(linked.Data.Token) {
		t.Fatal("linked token response omitted its id, scope or one-time credential")
	}
	linkedToken := linked.Data.Token
	response, body = apiTokenRequest(t, httpClient(), http.MethodPost, "/api/v1/server/preferences/tokens", primary, map[string]string{"name": "default-scope"})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("linked-token creation with default scope returned HTTP %d", response.StatusCode)
	}
	var secondLinked struct {
		Data struct {
			ID    string `json:"id"`
			Scope string `json:"scope"`
			Token string `json:"linked_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &secondLinked); err != nil {
		t.Fatal("decode default-scope linked token response:", err)
	}
	if secondLinked.Data.Scope != "read-write" || !e2eOwnerTokenPattern.MatchString(secondLinked.Data.Token) {
		t.Fatal("omitted linked-token scope did not default to read-write")
	}
	secondLinkedToken := secondLinked.Data.Token
	response, _ = apiTokenRequest(t, httpClient(), http.MethodPost, "/api/v1/server/preferences/tokens", primary, map[string]string{"name": "invalid-scope", "scope": "global"})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("global linked-token scope returned HTTP %d, want 400", response.StatusCode)
	}

	response, body = apiTokenRequest(t, httpClient(), http.MethodGet, "/api/v1/server/preferences/tokens", primary, nil)
	var listed struct {
		Data struct {
			Linked []struct {
				Scope string `json:"scope"`
			} `json:"linked"`
		} `json:"data"`
	}
	if response.StatusCode != http.StatusOK || json.Unmarshal(body, &listed) != nil {
		t.Fatalf("owner-token list returned HTTP %d with an invalid response", response.StatusCode)
	}
	if len(listed.Data.Linked) == 0 || listed.Data.Linked[0].Scope != "read" {
		t.Fatal("owner-token list did not preserve the linked token scope")
	}

	for _, route := range []struct {
		method, path string
		payload      any
	}{
		{http.MethodGet, "/api/v1/server/preferences/tokens", nil},
		{http.MethodPost, "/api/v1/server/preferences/tokens", map[string]string{"name": "forbidden", "scope": "read-write"}},
		{http.MethodPost, "/api/v1/server/preferences/tokens/primary/reveal", nil},
		{http.MethodPost, "/api/v1/server/preferences/tokens/primary/rotate", nil},
		{http.MethodDelete, "/api/v1/server/preferences/tokens/primary", nil},
		{http.MethodPost, "/api/v1/server/preferences/tokens/" + linked.Data.ID + "/reveal", nil},
		{http.MethodPost, "/api/v1/server/preferences/tokens/" + linked.Data.ID + "/rotate", nil},
		{http.MethodPatch, "/api/v1/server/preferences/tokens/" + linked.Data.ID, map[string]string{"name": "forbidden"}},
		{http.MethodDelete, "/api/v1/server/preferences/tokens/" + linked.Data.ID, nil},
	} {
		response, _ := apiTokenRequest(t, httpClient(), route.method, route.path, linkedToken, route.payload)
		if response.StatusCode != http.StatusForbidden || response.Header.Get("WWW-Authenticate") != `Bearer error="insufficient_scope"` {
			t.Errorf("linked token %s %s returned HTTP %d and WWW-Authenticate %q", route.method, route.path, response.StatusCode, response.Header.Get("WWW-Authenticate"))
		}
	}

	response = mcpToolCall(t, "")
	_ = readResponse(t, response)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("MCP tools/call without a token returned HTTP %d, want 401", response.StatusCode)
	}
	response = mcpToolCall(t, linkedToken)
	responseBody := readResponse(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(responseBody, `"result"`) || strings.Contains(responseBody, `"isError":true`) {
		t.Fatalf("read-scoped linked token could not call an MCP read tool (HTTP %d)", response.StatusCode)
	}
	response = mcpToolCallNamed(t, linkedToken, "post_api_api_version_text_compress")
	_ = readResponse(t, response)
	if response.StatusCode != http.StatusForbidden || response.Header.Get("WWW-Authenticate") != `Bearer error="insufficient_scope"` {
		t.Fatalf("read-scoped linked token called a write MCP tool: HTTP %d", response.StatusCode)
	}

	response, body = apiTokenRequest(t, httpClient(), http.MethodPost, "/api/v1/server/preferences/tokens/primary/rotate", primary, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("primary token rotation returned HTTP %d", response.StatusCode)
	}
	var rotated struct {
		Data struct {
			Token string `json:"owner_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &rotated); err != nil || !e2eOwnerTokenPattern.MatchString(rotated.Data.Token) || rotated.Data.Token == primary {
		t.Fatal("primary rotation did not replace the token")
	}
	response = mcpToolCall(t, linkedToken)
	if response.StatusCode != http.StatusOK {
		_ = readResponse(t, response)
		t.Fatal("rotating the primary invalidated an independent linked token")
	}
	_ = readResponse(t, response)
	response = mcpToolCall(t, secondLinkedToken)
	_ = readResponse(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatal("rotating the primary invalidated another linked token")
	}

	response, body = apiTokenRequest(t, httpClient(), http.MethodPost, "/api/v1/server/preferences/tokens/"+linked.Data.ID+"/rotate", rotated.Data.Token, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("linked-token rotation returned HTTP %d", response.StatusCode)
	}
	var linkedRotated struct {
		Data struct {
			Token string `json:"linked_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &linkedRotated); err != nil || !e2eOwnerTokenPattern.MatchString(linkedRotated.Data.Token) || linkedRotated.Data.Token == linkedToken {
		t.Fatal("linked-token rotation did not replace its credential")
	}
	response = mcpToolCall(t, linkedToken)
	_ = readResponse(t, response)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old linked token remained valid after rotation: HTTP %d", response.StatusCode)
	}
	response = mcpToolCall(t, linkedRotated.Data.Token)
	_ = readResponse(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("rotated linked token was not accepted by MCP: HTTP %d", response.StatusCode)
	}
	response = mcpToolCall(t, secondLinkedToken)
	_ = readResponse(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatal("rotating one linked token invalidated another linked token")
	}

	response, body = apiTokenRequest(t, httpClient(), http.MethodPatch, "/api/v1/server/preferences/tokens/"+linked.Data.ID, rotated.Data.Token, map[string]string{"name": "renamed-read-only", "expires": "7d"})
	var updated struct {
		Data struct {
			Name  string `json:"name"`
			Scope string `json:"scope"`
		} `json:"data"`
	}
	if response.StatusCode != http.StatusOK || json.Unmarshal(body, &updated) != nil || updated.Data.Name != "renamed-read-only" || updated.Data.Scope != "read" {
		t.Fatalf("linked-token update failed to preserve scope and update metadata: HTTP %d", response.StatusCode)
	}
	response, _ = apiTokenRequest(t, httpClient(), http.MethodDelete, "/api/v1/server/preferences/tokens/"+linked.Data.ID, rotated.Data.Token, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("linked-token revoke returned HTTP %d", response.StatusCode)
	}
	response, _ = apiTokenRequest(t, httpClient(), http.MethodDelete, "/api/v1/server/preferences/tokens/"+secondLinked.Data.ID, rotated.Data.Token, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("second linked-token revoke returned HTTP %d", response.StatusCode)
	}
	response, _ = apiTokenRequest(t, httpClient(), http.MethodDelete, "/api/v1/server/preferences/tokens/primary", rotated.Data.Token, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("primary-token revoke returned HTTP %d", response.StatusCode)
	}
}

func TestTier2OwnerTokenNewFormWithoutJavaScript(t *testing.T) {
	ctx, _ := newTab(t, true)
	var html string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(suite.browserURL+"/server/preferences/tokens"),
		chromedp.WaitReady(`form[action="/server/preferences/token/create"] button`, chromedp.ByQuery),
		chromedp.Click(`form[action="/server/preferences/token/create"] button`, chromedp.ByQuery, chromedp.NodeReady),
		chromedp.WaitReady(".copy-value", chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		saveArtifacts(t, ctx, "tier2-owner-token-create")
		t.Fatalf("issue an owner token from the no-JavaScript form: %v", err)
	}
	if !e2eOwnerTokenPattern.MatchString(html) || !strings.Contains(html, "Primary token") {
		saveArtifacts(t, ctx, "tier2-owner-token-create")
		t.Fatal("no-JavaScript New token form did not display the generated primary token")
	}
}

func TestTier3OwnerTokenNewForm(t *testing.T) {
	ctx, rec := newTab(t, false)
	var html string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(suite.browserURL+"/server/preferences/tokens"),
		chromedp.WaitReady(`form[action="/server/preferences/token/create"] button`, chromedp.ByQuery),
		chromedp.Click(`form[action="/server/preferences/token/create"] button`, chromedp.ByQuery, chromedp.NodeReady),
		chromedp.WaitReady(".copy-value", chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		saveArtifacts(t, ctx, "tier3-owner-token-create")
		t.Fatalf("issue an owner token in the full browser: %v", err)
	}
	if !e2eOwnerTokenPattern.MatchString(html) || !strings.Contains(html, "Primary token") {
		saveArtifacts(t, ctx, "tier3-owner-token-create")
		t.Fatal("full browser New token form did not display the generated primary token")
	}
	for _, msg := range rec.consoleErrors() {
		t.Errorf("owner-token flow: JavaScript console error: %s", msg)
	}
}

func TestTier3OwnerTokenPageScopeSelector(t *testing.T) {
	ctx, _ := newTab(t, false)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(suite.browserURL+"/server/preferences/tokens"),
		chromedp.WaitReady(`form[action="/server/preferences/token/create"] button`, chromedp.ByQuery),
		chromedp.Click(`form[action="/server/preferences/token/create"] button`, chromedp.ByQuery, chromedp.NodeReady),
		chromedp.WaitReady("#linked-scope", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("create an owner and load its linked-token scope selector: %v", err)
	}
	var present bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#linked-scope option[value="read"]') !== null`, &present)); err != nil {
		t.Fatal("inspect owner-token scope selector:", err)
	}
	if !present {
		t.Fatal("linked-token page does not expose the read scope")
	}
	var pageHTML, pageURL string
	if err := chromedp.Run(ctx,
		chromedp.SetValue("#linked-name", "browser-linked", chromedp.ByQuery),
		chromedp.SetValue("#linked-scope", "read", chromedp.ByQuery),
		chromedp.Click(`form[action="/server/preferences/tokens"] button[type="submit"]`, chromedp.ByQuery, chromedp.NodeReady),
		chromedp.Sleep(time.Second),
		chromedp.Location(&pageURL),
		chromedp.OuterHTML("html", &pageHTML),
	); err != nil {
		saveArtifacts(t, ctx, "tier3-owner-token-linked-create")
		t.Fatalf("create a read-scoped linked token through the browser form: %v", err)
	}
	if !strings.Contains(pageHTML, "browser-linked") || !e2eOwnerTokenPattern.MatchString(pageHTML) {
		t.Fatalf("browser linked-token form result: URL %s, name=%t credential=%t owner=%t linked-item=%t", pageURL, strings.Contains(pageHTML, "browser-linked"), e2eOwnerTokenPattern.MatchString(pageHTML), strings.Contains(pageHTML, "primary-token-heading"), strings.Contains(pageHTML, "token-list-item"))
	}
}
