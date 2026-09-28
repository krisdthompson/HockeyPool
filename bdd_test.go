package main

// Step definitions for the Gherkin scenarios in features/*.feature.
// Run with `go test ./...` (or `go test -run TestFeatures -v` to see each
// scenario).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cucumber/godog"
)

type world struct {
	adminPW, guestPhrase string
	st                   *store
	h                    http.Handler
	cookie               *http.Cookie // the signed-in session, if any
	last                 *httptest.ResponseRecorder
	teams                []string
	me                   int
}

func (w *world) handler() http.Handler {
	if w.h == nil {
		dir, _ := os.MkdirTemp("", "bdd")
		w.st, _ = openStore(dir)
		sv := &server{st: w.st, adminUser: "kris"}
		if w.adminPW != "" {
			sv.adminHash = hashPassword(w.adminPW)
		}
		if w.guestPhrase != "" {
			sv.guestHash = hashPassword(w.guestPhrase)
		}
		w.h = sv.routes()
	}
	return w.h
}

func (w *world) do(method, path, body string, form url.Values) *httptest.ResponseRecorder {
	h := w.handler()
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if w.cookie != nil {
		r.AddCookie(w.cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			if c.MaxAge < 0 {
				w.cookie = nil
			} else {
				w.cookie = c
			}
		}
	}
	w.last = rec
	return rec
}

func (w *world) state() (State, error) {
	rec := w.do("GET", "/api/state", "", nil)
	var s State
	if rec.Code != 200 {
		return s, fmt.Errorf("state: HTTP %d", rec.Code)
	}
	return s, json.Unmarshal(rec.Body.Bytes(), &s)
}

func (w *world) playerID(name string) (int, error) {
	for _, p := range w.st.get().Players {
		if p.Name == name {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("no player %q", name)
}

func (w *world) team(name string) (int, error) {
	if i := slices.Index(w.teams, name); i >= 0 {
		return i, nil
	}
	return 0, fmt.Errorf("no pool team %q", name)
}

func (w *world) post(path string, v any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(v)
	return w.do("POST", path, string(b), nil)
}

func expectOK(rec *httptest.ResponseRecorder) error {
	if rec.Code != 200 {
		return fmt.Errorf("HTTP %d: %s", rec.Code, rec.Body)
	}
	return nil
}

func initScenario(sc *godog.ScenarioContext) {
	w := &world{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*w = world{}
		return ctx, nil
	})

	// Setup.
	sc.Step(`^the admin is "([^"]*)" with password "([^"]*)"$`, func(_, pw string) { w.adminPW = pw })
	sc.Step(`^the guest entry phrase is "([^"]*)"$`, func(p string) { w.guestPhrase = p })
	sc.Step(`^the player list is:$`, func(t *godog.Table) error {
		var rows []string
		for _, r := range t.Rows {
			var cells []string
			for _, c := range r.Cells {
				cells = append(cells, c.Value)
			}
			rows = append(rows, strings.Join(cells, ","))
		}
		players, _, err := parsePlayers(strings.Join(rows, "\n"))
		if err != nil {
			return err
		}
		w.handler()
		_, err = w.st.update(func(s *State) error { s.Players = players; return nil })
		return err
	})
	sc.Step(`^the pool teams are "([^"]*)"$`, func(list string) {
		w.teams = nil
		for _, t := range strings.Split(list, ",") {
			w.teams = append(w.teams, strings.TrimSpace(t))
		}
	})
	sc.Step(`^Kris is "([^"]*)"$`, func(name string) (err error) { w.me, err = w.team(name); return })
	sc.Step(`^each team has \$(\d+) for (\d+) players with a \$(\d+) minimum bid$`, func(budget, roster, min int) error {
		return expectOK(w.post("/api/settings", map[string]any{"managers": w.teams, "me": w.me, "rosterSize": roster, "budget": budget, "minBid": min}))
	})

	// Signing in.
	sc.Step(`^a signed-out visitor opens the board$`, func() { w.cookie = nil; w.do("GET", "/", "", nil) })
	sc.Step(`^a signed-out visitor opens the sign-in page$`, func() { w.cookie = nil; w.do("GET", "/login", "", nil) })
	sc.Step(`^they are redirected to the sign-in page$`, func() error {
		if w.last.Code != http.StatusSeeOther || w.last.Header().Get("Location") != "/login" {
			return fmt.Errorf("got HTTP %d to %q", w.last.Code, w.last.Header().Get("Location"))
		}
		return nil
	})
	sc.Step(`^opening the board redirects to the sign-in page$`, func() error {
		w.do("GET", "/", "", nil)
		if w.last.Code != http.StatusSeeOther {
			return fmt.Errorf("got HTTP %d", w.last.Code)
		}
		return nil
	})
	sc.Step(`^no browser sign-in pop-up is requested$`, func() error {
		if h := w.last.Header().Get("WWW-Authenticate"); h != "" {
			return fmt.Errorf("WWW-Authenticate: %s", h)
		}
		return nil
	})
	sc.Step(`^the page has a "([^"]*)" button$`, func(label string) error {
		if !strings.Contains(w.last.Body.String(), ">"+label+"</button>") {
			return fmt.Errorf("no %q button", label)
		}
		return nil
	})
	sc.Step(`^the guest entry phrase form starts collapsed$`, func() error {
		b := w.last.Body.String()
		if !strings.Contains(b, `id="guest-panel" hidden`) || !strings.Contains(b, `aria-expanded="false"`) {
			return fmt.Errorf("guest form isn't collapsed")
		}
		return nil
	})
	login := func(name, pw string) {
		w.cookie = nil
		w.do("POST", "/login", "", url.Values{"name": {name}, "password": {pw}})
	}
	guest := func(name, phrase string) {
		w.cookie = nil
		w.do("POST", "/login", "", url.Values{"guest": {"1"}, "name": {name}, "phrase": {phrase}})
	}
	sc.Step(`^"([^"]*)" signs in with password "([^"]*)"$`, login)
	sc.Step(`^"([^"]*)" is signed in$`, func(name string) error {
		login(name, w.adminPW)
		if w.cookie == nil {
			return fmt.Errorf("sign-in failed")
		}
		return nil
	})
	sc.Step(`^a guest named "([^"]*)" enters with phrase "([^"]*)"$`, guest)
	sc.Step(`^a guest named "([^"]*)" is signed in$`, func(name string) error {
		guest(name, w.guestPhrase)
		if w.cookie == nil {
			return fmt.Errorf("guest sign-in failed")
		}
		return nil
	})
	sc.Step(`^they are signed in as the admin$`, func() error {
		s, err := w.state()
		if err == nil && (s.You == nil || !s.You.Admin) {
			err = fmt.Errorf("not admin: %+v", s.You)
		}
		return err
	})
	sc.Step(`^they are signed in as guest "([^"]*)"$`, func(name string) error {
		s, err := w.state()
		if err == nil && (s.You == nil || s.You.Admin || s.You.Name != name) {
			err = fmt.Errorf("signed in as %+v", s.You)
		}
		return err
	})
	sc.Step(`^sign-in fails$`, func() error {
		if w.cookie != nil || w.last.Code != http.StatusUnauthorized {
			return fmt.Errorf("sign-in succeeded (HTTP %d)", w.last.Code)
		}
		return nil
	})
	sc.Step(`^they sign out$`, func() { w.do("POST", "/logout", "", url.Values{}) })

	// Privacy.
	sc.Step(`^they load the draft$`, func() error { return expectOK(w.do("GET", "/api/state", "", nil)) })
	sc.Step(`^the response does not contain "([^"]*)"$`, func(text string) error {
		if strings.Contains(w.last.Body.String(), text) {
			return fmt.Errorf("response contains %q", text)
		}
		return nil
	})
	sc.Step(`^no player is flagged$`, func() error {
		var s State
		json.Unmarshal(w.last.Body.Bytes(), &s)
		for _, p := range s.Players {
			if p.Tag != "" || p.Why != "" {
				return fmt.Errorf("%s is flagged %q", p.Name, p.Tag)
			}
		}
		return nil
	})
	sc.Step(`^"([^"]*)" is flagged "([^"]*)"$`, func(name, tag string) error {
		var s State
		json.Unmarshal(w.last.Body.Bytes(), &s)
		for _, p := range s.Players {
			if p.Name == name && p.Tag == tag {
				return nil
			}
		}
		return fmt.Errorf("%s isn't flagged %q", name, tag)
	})
	sc.Step(`^the request is refused as not allowed$`, func() error {
		if w.last.Code != http.StatusForbidden {
			return fmt.Errorf("HTTP %d", w.last.Code)
		}
		return nil
	})

	// Drafting.
	markDrafted := func(name string) error {
		id, err := w.playerID(name)
		if err != nil {
			return err
		}
		w.post("/api/pick", map[string]any{"playerId": id, "manager": Gone})
		return nil
	}
	sc.Step(`^(?:Kris|they) marks? "([^"]*)" as drafted$`, markDrafted)
	sc.Step(`^Kris marked "([^"]*)" as drafted$`, func(name string) error {
		if err := markDrafted(name); err != nil {
			return err
		}
		return expectOK(w.last)
	})
	record := func(name, team string, price int) error {
		id, err := w.playerID(name)
		if err != nil {
			return err
		}
		m, err := w.team(team)
		if err != nil {
			return err
		}
		w.post("/api/pick", map[string]any{"playerId": id, "manager": m, "price": price})
		return nil
	}
	sc.Step(`^Kris records "([^"]*)" as bought by "([^"]*)" for \$(\d+)$`, record)
	sc.Step(`^Kris recorded "([^"]*)" as bought by "([^"]*)" for \$(\d+)$`, func(name, team string, price int) error {
		if err := record(name, team, price); err != nil {
			return err
		}
		return expectOK(w.last)
	})
	sc.Step(`^Kris sets "([^"]*)" drafted by "([^"]*)" for \$(\d+)$`, func(name, team string, price int) error {
		id, err := w.playerID(name)
		if err != nil {
			return err
		}
		m, err := w.team(team)
		if err != nil {
			return err
		}
		return expectOK(w.post("/api/repick", map[string]any{"playerId": id, "manager": m, "price": price}))
	})
	sc.Step(`^Kris unticks "([^"]*)"$`, func(name string) error {
		id, err := w.playerID(name)
		if err != nil {
			return err
		}
		return expectOK(w.post("/api/unpick", map[string]any{"playerId": id}))
	})
	sc.Step(`^Kris clears all picks$`, func() error { return expectOK(w.post("/api/reset", map[string]any{})) })
	sc.Step(`^Kris reorders the pool teams to "([^"]*)"$`, func(list string) error {
		var teams []string
		for _, t := range strings.Split(list, ",") {
			teams = append(teams, strings.TrimSpace(t))
		}
		s := w.st.get()
		me := slices.Index(teams, "Kris")
		if err := expectOK(w.post("/api/settings", map[string]any{"managers": teams, "me": me, "rosterSize": s.RosterSize, "budget": s.Budget, "minBid": s.MinBid})); err != nil {
			return err
		}
		w.teams = teams
		return nil
	})
	pickOf := func(name string) (*Pick, error) {
		id, err := w.playerID(name)
		if err != nil {
			return nil, err
		}
		for _, k := range w.st.get().Picks {
			if k.PlayerID == id {
				return &k, nil
			}
		}
		return nil, nil
	}
	sc.Step(`^"([^"]*)" is drafted by nobody recorded$`, func(name string) error {
		k, err := pickOf(name)
		if err == nil && (k == nil || k.Manager != Gone) {
			err = fmt.Errorf("pick is %+v", k)
		}
		return err
	})
	sc.Step(`^"([^"]*)" is drafted by "([^"]*)" for \$(\d+)$`, func(name, team string, price int) error {
		k, err := pickOf(name)
		if err != nil {
			return err
		}
		m, err := w.team(team)
		if err == nil && (k == nil || k.Manager != m || k.Price != price) {
			err = fmt.Errorf("pick is %+v", k)
		}
		return err
	})
	sc.Step(`^"([^"]*)" is available$`, func(name string) error {
		k, err := pickOf(name)
		if err == nil && k != nil {
			err = fmt.Errorf("still drafted: %+v", k)
		}
		return err
	})
	sc.Step(`^no players are drafted$`, func() error {
		if n := len(w.st.get().Picks); n != 0 {
			return fmt.Errorf("%d picks", n)
		}
		return nil
	})
	money := func(team string) (left, maxb int, err error) {
		m, err := w.team(team)
		if err != nil {
			return 0, 0, err
		}
		s := w.st.get()
		left = s.Budget
		for _, k := range s.Picks {
			if k.Manager == m {
				left -= k.Price
			}
		}
		return left, maxBid(&s, m), nil
	}
	sc.Step(`^"([^"]*)" has \$(\d+) left and can bid at most \$(\d+)$`, func(team string, wantLeft, wantMax int) error {
		left, maxb, err := money(team)
		if err == nil && (left != wantLeft || maxb != wantMax) {
			err = fmt.Errorf("$%d left, max bid $%d", left, maxb)
		}
		return err
	})
	sc.Step(`^"([^"]*)" has \$(\d+) left$`, func(team string, want int) error {
		left, _, err := money(team)
		if err == nil && left != want {
			err = fmt.Errorf("$%d left", left)
		}
		return err
	})
	sc.Step(`^a backup of the draft including "([^"]*)" can be downloaded$`, func(name string) error {
		var list struct{ Backups []string }
		if err := json.Unmarshal(w.do("GET", "/api/backups", "", nil).Body.Bytes(), &list); err != nil || len(list.Backups) == 0 {
			return fmt.Errorf("no backups listed (%v)", err)
		}
		rec := w.do("GET", "/api/backups/"+list.Backups[0], "", nil)
		var s State
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			return fmt.Errorf("backup %s: %v", list.Backups[0], err)
		}
		id, _ := w.playerID(name)
		if !slices.ContainsFunc(s.Picks, func(k Pick) bool { return k.PlayerID == id }) {
			return fmt.Errorf("latest backup doesn't include %s", name)
		}
		if w.do("GET", "/api/backups/..%2Fstate.json", "", nil).Code != http.StatusNotFound {
			return fmt.Errorf("backup download isn't limited to backup files")
		}
		return nil
	})
	sc.Step(`^the request is refused with "([^"]*)"$`, func(text string) error {
		if w.last.Code != http.StatusBadRequest || !strings.Contains(w.last.Body.String(), text) {
			return fmt.Errorf("HTTP %d: %s", w.last.Code, w.last.Body)
		}
		return nil
	})
}

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"features"}, Tags: "~@ui", TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("feature scenarios failed")
	}
}
