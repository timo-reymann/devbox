package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func req(user, pass string, set bool) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	if set {
		r.SetBasicAuth(user, pass)
	}
	return r
}

func TestAuthCheck(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	plain := NewAuth("dev", "dev", "")
	hashed := NewAuth("dev", "ignored", string(hash))

	cases := []struct {
		name string
		a    *Auth
		r    *http.Request
		want bool
	}{
		{"plain ok", plain, req("dev", "dev", true), true},
		{"plain wrong password", plain, req("dev", "nope", true), false},
		{"plain wrong user", plain, req("root", "dev", true), false},
		{"plain no header", plain, req("", "", false), false},
		{"hash ok", hashed, req("dev", "s3cret", true), true},
		{"hash ignores plain", hashed, req("dev", "ignored", true), false},
	}
	for _, c := range cases {
		if got := c.a.Check(c.r); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNewAuthUnset(t *testing.T) {
	if NewAuth("dev", "", "") != nil {
		t.Fatal("expected nil Auth without credentials")
	}
}

func TestHandleHTTPRejectsBeforeWake(t *testing.T) {
	// A nil docker client would panic if the handler tried to wake the app.
	g := &Gatekeeper{cfg: &Config{Auth: NewAuth("dev", "dev", "")}}
	h := g.HandleHTTP(&httpRoute{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("", "", false))
	if w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("got %d %v", w.Code, w.Header())
	}
}
