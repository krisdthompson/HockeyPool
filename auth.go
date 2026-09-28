package main

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
)

// Logins: the admin (DRAFT_ADMIN_USER, default "kris") signs in with
// DRAFT_PASSWORD or the password behind DRAFT_ADMIN_HASH. Anyone else signs
// in as a guest with any username and the password behind DRAFT_GUEST_HASH.
// Guests can look, and keep their own team choice and flags, but can't
// change the draft. With no passwords configured the site is open and
// everyone is the admin (local use).

type user struct {
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
}

type ctxKey struct{}

func userOf(r *http.Request) user {
	u, _ := r.Context().Value(ctxKey{}).(user)
	return u
}

// Prefs is one user's private view of the draft.
type Prefs struct {
	Me   int            `json:"me"`   // their pool team, -1 = not chosen
	Tags map[int]string `json:"tags"` // player ID -> tag; for the admin, overrides the list's tag
}

const hashIters = 200_000

// hashPassword returns "pbkdf2$iters$salt$hash" for storing in config.
func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key, _ := pbkdf2.Key(sha256.New, pw, salt, hashIters, 32)
	return fmt.Sprintf("pbkdf2$%d$%x$%x", hashIters, salt, key)
}

func checkHash(pw, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iters, err1 := strconv.Atoi(parts[1])
	salt, err2 := hex.DecodeString(parts[2])
	want, err3 := hex.DecodeString(parts[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iters, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

func (sv *server) open() bool {
	return sv.adminPW == "" && sv.adminHash == "" && sv.guestHash == ""
}

func (sv *server) login(name, pw string) (user, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return user{}, false
	}
	if name == sv.adminUser {
		ok := sv.adminPW != "" && subtle.ConstantTimeCompare([]byte(pw), []byte(sv.adminPW)) == 1
		ok = ok || sv.adminHash != "" && checkHash(pw, sv.adminHash)
		return user{Name: name, Admin: true}, ok
	}
	return user{Name: name}, sv.guestHash != "" && checkHash(pw, sv.guestHash)
}

func (sv *server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		u := user{Name: sv.adminUser, Admin: true}
		if !sv.open() {
			name, pw, _ := r.BasicAuth()
			var ok bool
			if u, ok = sv.login(name, pw); !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="hockeypool: kris, or any name with the guest password"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

// adminOnly wraps handlers that change the shared draft.
func adminOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !userOf(r).Admin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "guests can look but not change the draft"})
			return
		}
		h(w, r)
	}
}

func prefsOf(s *State, u user) Prefs {
	if p, ok := s.Prefs[u.Name]; ok {
		return p
	}
	if u.Admin {
		return Prefs{Me: s.Me} // drafts saved before usernames existed
	}
	return Prefs{Me: -1}
}

// setPrefs stores a changed copy of a user's prefs.
func setPrefs(s *State, u user, fn func(p *Prefs)) {
	p := prefsOf(s, u)
	p.Tags = maps.Clone(p.Tags)
	if p.Tags == nil {
		p.Tags = map[int]string{}
	}
	fn(&p)
	s.Prefs = maps.Clone(s.Prefs)
	if s.Prefs == nil {
		s.Prefs = map[string]Prefs{}
	}
	s.Prefs[u.Name] = p
}

// view is the state as one user sees it: their team, their flags, and none
// of anyone else's private notes.
func view(s State, u user) State {
	p := prefsOf(&s, u)
	v := s
	v.Prefs = nil
	v.Me = p.Me
	v.You = &u
	v.Players = make([]Player, len(s.Players))
	for i, pl := range s.Players {
		t, set := p.Tags[pl.ID]
		if !u.Admin {
			pl.Why = ""
			if !set {
				t = ""
			}
		} else if !set {
			t = pl.Tag
		}
		pl.Tag = t
		v.Players[i] = pl
	}
	return v
}
