package server

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	"github.com/six-ddc/art/internal/api"
)

// TokenCookie is the name of the cookie that carries the token.
//
// Why a cookie is required: neither EventSource nor an iframe can set an
// Authorization header, so the protocol is "the first request with ?token=
// to any page causes the server to set an HttpOnly cookie, and all
// subsequent SSE / iframe / static-asset requests authenticate via that
// cookie instead."
const TokenCookie = "art_token"

// Auth is the authentication middleware. When token is empty (local mode)
// it passes every request through unchecked.
//
// Lookup order: Authorization: Bearer > ?token= > cookie.
// It also enforces an Origin check on every non-GET request: if Origin is
// present and doesn't match this server's origin, the request is rejected;
// a missing Origin (curl / the CLI) is allowed through — this blocks
// cross-site writes from a browser without getting in the way of the
// command line.
func Auth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			got := extractToken(r)
			if !tokensEqual(got, token) {
				WriteError(w, http.StatusUnauthorized, api.ErrUnauthorized, "missing or invalid token")
				return
			}
		}

		if !isSafeMethod(r.Method) {
			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r) {
				WriteError(w, http.StatusForbidden, api.ErrForbidden, "cross-origin request rejected")
				return
			}
		}

		// The first request with ?token= to any page sets an HttpOnly cookie,
		// so subsequent SSE/iframe/static-asset requests can authenticate
		// with it (none of them can set an Authorization header).
		if token != "" && tokensEqual(r.URL.Query().Get("token"), token) {
			http.SetCookie(w, &http.Cookie{
				Name:     TokenCookie,
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		}

		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	if c, err := r.Cookie(TokenCookie); err == nil {
		return c.Value
	}
	return ""
}

// tokensEqual uses a constant-time comparison to avoid a timing side
// channel when --host exposes the server on a non-loopback address
// (MINOR-4).
func tokensEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func sameOrigin(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
