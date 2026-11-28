package server

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/apimgr/api/src/config"
)

var ownerTokenNamePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// ownerTokenCookieName derives the project-unique owner cookie name. The
// eight-letter suffix must come from the recorded IDEA.md project variable.
func ownerTokenCookieName(projectName, suffix string) (string, error) {
	projectName = strings.TrimSpace(projectName)
	if !ownerTokenNamePattern.MatchString(projectName) || len(suffix) != 8 {
		return "", errors.New("invalid owner-token cookie identifier")
	}
	for _, char := range suffix {
		if char < 'a' || char > 'z' {
			return "", errors.New("owner-token suffix must be eight lowercase letters")
		}
	}
	return projectName + "_owner_token_" + suffix, nil
}

// setOwnerTokenCookie writes the primary credential cookie. cookieName is
// supplied by the project-variable resolver; it is never guessed here.
func setOwnerTokenCookie(w http.ResponseWriter, r *http.Request, cfg *config.ServerConfig, cookieName, token string) {
	if cookieName == "" || token == "" {
		return
	}
	policy, _ := cfg.Server.Tokens.Normalized()
	maxAge := int(policy.CookieMaxAgeDuration().Seconds())
	proto, _, _ := GetURLVars(r)
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: strings.EqualFold(proto, "https"), SameSite: http.SameSiteStrictMode,
	})
}

// clearOwnerTokenCookie clears the primary credential cookie using the same
// scope and security attributes as its setter.
func clearOwnerTokenCookie(w http.ResponseWriter, r *http.Request, cookieName string) {
	if cookieName == "" {
		return
	}
	proto, _, _ := GetURLVars(r)
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0).UTC(), HttpOnly: true,
		Secure: strings.EqualFold(proto, "https"), SameSite: http.SameSiteStrictMode,
	})
}
