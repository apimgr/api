package server

import "testing"

func TestValidOwnerCookieName(t *testing.T) {
	valid, err := ownerTokenCookieName("api", "abcdefgh")
	if err != nil {
		t.Fatal(err)
	}
	if !validOwnerCookieName(valid, "api") {
		t.Fatal("derived owner cookie name should be accepted")
	}
	for _, name := range []string{"", "api_owner_token_ABCDEFGH", "api_owner_token_abcdefg", "other_owner_token_abcdefgh", valid + "-extra"} {
		if validOwnerCookieName(name, "api") {
			t.Errorf("invalid cookie name %q accepted", name)
		}
	}
}
