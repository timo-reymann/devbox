package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

// Auth checks HTTP basic auth before the gatekeeper wakes the devbox
// container, so anonymous traffic can neither start it nor keep it awake.
// The credentials are the same BASIC_AUTH_* variables that Caddy enforces.
type Auth struct {
	user string
	pass string // plaintext, compared in constant time
	hash []byte // bcrypt, takes precedence over pass
}

// NewAuth returns nil when no credentials are configured.
func NewAuth(user, pass, hash string) *Auth {
	if pass == "" && hash == "" {
		return nil
	}
	return &Auth{user: user, pass: pass, hash: []byte(hash)}
}

// Check reports whether the request carries valid credentials.
func (a *Auth) Check(r *http.Request) bool {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	// Hash both sides so the comparison length does not depend on input.
	uSum, wantU := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(a.user))
	userOK := subtle.ConstantTimeCompare(uSum[:], wantU[:]) == 1

	var passOK bool
	if len(a.hash) > 0 {
		passOK = bcrypt.CompareHashAndPassword(a.hash, []byte(pass)) == nil
	} else {
		pSum, wantP := sha256.Sum256([]byte(pass)), sha256.Sum256([]byte(a.pass))
		passOK = subtle.ConstantTimeCompare(pSum[:], wantP[:]) == 1
	}
	return userOK && passOK
}

// Challenge answers an unauthenticated request.
func (a *Auth) Challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="devbox", charset="UTF-8"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
