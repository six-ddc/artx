package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthNoTokenPassesThrough(t *testing.T) {
	h := Auth("", okHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("local mode (no token) should pass through, got %d", w.Code)
	}
}

func TestAuthBearerHeader(t *testing.T) {
	h := Auth("secret", okHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Authorization: Bearer should pass, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthQueryToken(t *testing.T) {
	h := Auth("secret", okHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/docs?token=secret", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("?token= should pass, got %d", w.Code)
	}
	// The first request carrying ?token= should set an HttpOnly cookie.
	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == TokenCookie && c.Value == "secret" {
			found = true
			if !c.HttpOnly {
				t.Fatal("art_token cookie must be HttpOnly")
			}
		}
	}
	if !found {
		t.Fatal("art_token cookie should have been set")
	}
}

func TestAuthCookie(t *testing.T) {
	h := Auth("secret", okHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	req.AddCookie(&http.Cookie{Name: TokenCookie, Value: "secret"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cookie carrying token should pass, got %d", w.Code)
	}
}

func TestAuthMissingOrWrongTokenRejected(t *testing.T) {
	h := Auth("secret", okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing token should be 401, got %d", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/docs?token=wrong", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token should be 401, got %d", w2.Code)
	}
}

func TestAuthOriginCheckOnNonGET(t *testing.T) {
	h := Auth("secret", okHandler())

	// Cross-site Origin -> 403.
	req := httptest.NewRequest(http.MethodPost, "/api/docs", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Origin", "http://evil.example.com")
	req.Host = "127.0.0.1:7777"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-GET request with cross-site Origin should be 403, got %d", w.Code)
	}

	// Missing Origin (curl/CLI) -> passes through.
	req2 := httptest.NewRequest(http.MethodPost, "/api/docs", nil)
	req2.Header.Set("Authorization", "Bearer secret")
	req2.Host = "127.0.0.1:7777"
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("missing Origin should pass through, got %d: %s", w2.Code, w2.Body.String())
	}

	// Same-origin Origin -> passes through.
	req3 := httptest.NewRequest(http.MethodPost, "/api/docs", nil)
	req3.Header.Set("Authorization", "Bearer secret")
	req3.Header.Set("Origin", "http://127.0.0.1:7777")
	req3.Host = "127.0.0.1:7777"
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("same-origin Origin should pass through, got %d: %s", w3.Code, w3.Body.String())
	}
}
