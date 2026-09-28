package main

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"maps"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Logins happen on /login (a page, not a browser pop-up) and set a signed
// session cookie. The admin (DRAFT_ADMIN_USER, default "kris") signs in with
// DRAFT_PASSWORD or the password behind DRAFT_ADMIN_HASH. Guests press
// "Sign in as guest" and give the entry phrase behind DRAFT_GUEST_HASH
// (comma-separated hashes allow more than one phrase). Guests can look but
// not change the draft. With no passwords configured the site is open and
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
	return sv.guestLogin(name, pw)
}

func (sv *server) guestLogin(name, phrase string) (user, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "guest"
	}
	if name == sv.adminUser {
		return user{}, false
	}
	for _, h := range strings.Split(sv.guestHash, ",") {
		if h = strings.TrimSpace(h); h != "" && checkHash(strings.TrimSpace(phrase), h) {
			return user{Name: name}, true
		}
	}
	return user{}, false
}

const cookieName = "hp_session"

// sessionKey signs session cookies. It comes from the configured secrets so
// sessions survive restarts and deploys.
func (sv *server) sessionKey() []byte {
	k := sha256.Sum256([]byte("hockeypool-session|" + os.Getenv("DRAFT_SESSION_SECRET") + "|" + sv.adminPW + "|" + sv.adminHash + "|" + sv.guestHash))
	return k[:]
}

func (sv *server) sign(v string) string {
	m := hmac.New(sha256.New, sv.sessionKey())
	m.Write([]byte(v))
	return hex.EncodeToString(m.Sum(nil))
}

func (sv *server) setSession(w http.ResponseWriter, r *http.Request, u user) {
	exp := time.Now().Add(30 * 24 * time.Hour).Unix()
	v := fmt.Sprintf("%s|%t|%d", u.Name, u.Admin, exp)
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: base64.RawURLEncoding.EncodeToString([]byte(v)) + "." + sv.sign(v),
		Path: "/", MaxAge: 30 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

func (sv *server) session(r *http.Request) (user, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return user{}, false
	}
	b64, sig, ok := strings.Cut(c.Value, ".")
	raw, err := base64.RawURLEncoding.DecodeString(b64)
	if !ok || err != nil || !hmac.Equal([]byte(sig), []byte(sv.sign(string(raw)))) {
		return user{}, false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return user{}, false
	}
	exp, _ := strconv.ParseInt(parts[2], 10, 64)
	if time.Now().Unix() > exp {
		return user{}, false
	}
	return user{Name: parts[0], Admin: parts[1] == "true"}, true
}

func (sv *server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz", "/login", "/logout":
			next.ServeHTTP(w, r)
			return
		}
		u := user{Name: sv.adminUser, Admin: true}
		if !sv.open() {
			var ok bool
			if u, ok = sv.session(r); !ok {
				// Scripts and tests may still send basic auth; browsers never
				// see a pop-up because no WWW-Authenticate header is sent.
				name, pw, basic := r.BasicAuth()
				if u, ok = sv.login(name, pw); !basic || !ok {
					if strings.HasPrefix(r.URL.Path, "/api/") {
						writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "signed out: reload to sign in"})
					} else {
						http.Redirect(w, r, "/login", http.StatusSeeOther)
					}
					return
				}
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

// loginPage is server-rendered: the admin form, and a guest button that
// expands the entry-phrase form in place (no pop-ups).
func (sv *server) loginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.ParseForm()
		var u user
		var ok bool
		if r.FormValue("guest") != "" {
			u, ok = sv.guestLogin(r.FormValue("name"), r.FormValue("phrase"))
		} else {
			u, ok = sv.login(r.FormValue("name"), r.FormValue("password"))
		}
		if ok {
			sv.setSession(w, r, u)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		loginTmpl.Execute(w, map[string]any{"Error": "That didn't match. Try again.", "Guest": r.FormValue("guest") != ""})
		return
	}
	loginTmpl.Execute(w, map[string]any{})
}

func (sv *server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

var loginTmpl = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Pool Draft sign in</title>
<style>
:root { --bg:#f6f7f9; --panel:#fff; --text:#16191d; --muted:#5d6670; --line:#dde1e6; --accent:#0b5cad; --accent-text:#fff; --bad:#b3261e; }
@media (prefers-color-scheme: dark) { :root { --bg:#111418; --panel:#1a1e24; --text:#e7eaee; --muted:#9aa3ad; --line:#2c323a; --accent:#5ea8f0; --accent-text:#0b1520; --bad:#f07b72; } }
* { box-sizing: border-box; }
body { margin:0; background:var(--bg); color:var(--text); font:16px/1.4 system-ui,-apple-system,"Segoe UI",sans-serif; }
main { max-width: 420px; margin: 8vh auto; padding: 0 16px; }
h1 { font-size: 22px; margin: 0 0 16px; }
section { background:var(--panel); border:1px solid var(--line); border-radius:10px; padding:16px; margin-bottom:16px; }
h2 { font-size: 15px; margin: 0 0 10px; }
label { display:block; margin: 8px 0; font-size: 14px; color: var(--muted); }
input { display:block; width:100%; margin-top:4px; font:inherit; color:inherit; background:var(--panel); border:1px solid var(--line); border-radius:6px; padding:9px 10px; }
button { font:inherit; border-radius:6px; padding:9px 14px; cursor:pointer; border:1px solid var(--line); background:transparent; color:inherit; }
button.primary { background:var(--accent); color:var(--accent-text); border-color:var(--accent); width:100%; margin-top:8px; }
.err { color: var(--bad); margin: 0 0 12px; }
.muted { color: var(--muted); font-size: 14px; }
</style></head>
<body><main>
<h1>🏒 Pool Draft</h1>
{{if .Error}}<p class="err" role="alert">{{.Error}}</p>{{end}}
<section>
  <h2>Guests</h2>
  <button type="button" id="guest-btn" aria-controls="guest-panel" aria-expanded="{{if .Guest}}true{{else}}false{{end}}">Sign in as guest</button>
  <div id="guest-panel" {{if not .Guest}}hidden{{end}}>
    <form method="post" action="/login">
      <input type="hidden" name="guest" value="1">
      <label>Entry phrase <input name="phrase" type="password" autocomplete="off" required></label>
      <label>Your name (optional) <input name="name" autocomplete="nickname"></label>
      <button class="primary">Enter as guest</button>
      <button type="button" id="guest-cancel">Cancel</button>
    </form>
  </div>
</section>
<section>
  <h2>Pool admin</h2>
  <form method="post" action="/login">
    <label>Username <input name="name" autocomplete="username" required autocapitalize="none"></label>
    <label>Password <input name="password" type="password" autocomplete="current-password" required></label>
    <button class="primary">Sign in</button>
  </form>
</section>
</main>
<script>
// Expand and collapse the guest form in place.
const btn = document.getElementById('guest-btn'), panel = document.getElementById('guest-panel');
const set = open => { panel.hidden = !open; btn.setAttribute('aria-expanded', String(open)); if (open) panel.querySelector('input[name=phrase]').focus(); };
btn.onclick = () => set(panel.hidden);
document.getElementById('guest-cancel').onclick = () => set(false);
</script>
</body></html>`))

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
