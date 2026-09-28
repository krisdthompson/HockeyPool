package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParsePlayersCSVAndTSV(t *testing.T) {
	csvText := "Player,Tm,Position,GP,Goals,Assists,Rookie,Status\nAlpha One,edm,C,80,40,60,,\nBeta Two,TOR,D,10,2,3,Y,IR\n"
	tsv := strings.ReplaceAll(csvText, ",", "\t")
	for name, text := range map[string]string{"csv": csvText, "tsv": tsv} {
		ps, _, err := parsePlayers(text)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(ps) != 2 {
			t.Fatalf("%s: got %d players", name, len(ps))
		}
		if ps[0].Team != "EDM" || ps[0].Pts != 100 || ps[0].Rookie {
			t.Errorf("%s: first player %+v", name, ps[0])
		}
		if !ps[1].Rookie || ps[1].Injury != "IR" || ps[1].Pts != 5 {
			t.Errorf("%s: second player %+v", name, ps[1])
		}
	}
	if ps, _, _ := parsePlayers("Name,Pos\nSkater,C\nKeeper,G\n"); len(ps) != 1 || ps[0].Name != "Skater" {
		t.Errorf("goalie not skipped: %+v", ps)
	}
	if _, _, err := parsePlayers("Team,GP\nEDM,3\n"); err == nil {
		t.Error("expected error without a name column")
	}
}

func TestSnakeOrder(t *testing.T) {
	s := &State{Managers: []string{"a", "b", "c"}, Snake: true}
	var got []int
	for i := 0; i < 7; i++ {
		got = append(got, onClock(s, i))
	}
	want := []int{0, 1, 2, 2, 1, 0, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("snake order %v, want %v", got, want)
		}
	}
}

func TestDraftFlow(t *testing.T) {
	st, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := (&server{st: st, adminUser: "kris", adminPW: "pw"}).routes()
	do := func(path, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if auth {
			r.SetBasicAuth("kris", "pw")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := do("/api/undo", "{}", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: %d", w.Code)
	}
	if w := do("/api/settings", `{"managers":["x","y"],"me":0,"rosterSize":7,"snake":true,"budget":0}`, true); w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	if w := do("/api/import", `{"text":"Name,Team\nA,EDM\nB,TOR"}`, true); w.Code != 200 {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	if w := do("/api/pick", `{"playerId":1}`, true); w.Code != 200 {
		t.Fatalf("pick: %d %s", w.Code, w.Body)
	}
	if w := do("/api/pick", `{"playerId":1}`, true); w.Code != 400 {
		t.Fatalf("double pick allowed: %d", w.Code)
	}
	if w := do("/api/import", `{"text":"Name\nC"}`, true); w.Code != 400 {
		t.Fatalf("replace mid-draft allowed: %d", w.Code)
	}
	if w := do("/api/import", `{"mode":"merge","text":"Name,Rookie,Proj\na,y,40\nC,,"}`, true); w.Code != 200 {
		t.Fatalf("merge: %d %s", w.Code, w.Body)
	}
	if s := st.get(); len(s.Players) != 3 || !s.Players[0].Rookie || s.Players[0].Proj != 40 || s.Players[0].Team != "EDM" || s.Players[2].ID != 3 {
		t.Fatalf("merge result %+v", s.Players)
	}
	// State survives a reopen.
	st2, err := openStore(strings.TrimSuffix(st.path, "/state.json"))
	if err != nil || len(st2.get().Picks) != 1 {
		t.Fatalf("reopen: %v %+v", err, st2.get().Picks)
	}
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("healthz behind auth: %d", w.Code)
	}
}

func TestAuction(t *testing.T) {
	st, _ := openStore(t.TempDir())
	h := (&server{st: st}).routes()
	do := func(path, body string) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return w.Code
	}
	do("/api/import", `{"text":"Name\nA\nB\nC\nD"}`)
	if c := do("/api/settings", `{"managers":["x","y"],"me":0,"rosterSize":2,"budget":10,"minBid":1}`); c != 200 {
		t.Fatal("settings", c)
	}
	if c := do("/api/pick", `{"playerId":1,"price":3}`); c != 400 {
		t.Fatal("auction pick without manager allowed")
	}
	if c := do("/api/pick", `{"playerId":1,"manager":0,"price":10}`); c != 400 {
		t.Fatal("bid over max allowed (must keep $1 for last slot)")
	}
	if c := do("/api/pick", `{"playerId":1,"manager":0,"price":9}`); c != 200 {
		t.Fatal("max bid rejected", c)
	}
	if c := do("/api/pick", `{"playerId":2,"manager":0,"price":2}`); c != 400 {
		t.Fatal("overspend allowed")
	}
	if c := do("/api/pick", `{"playerId":2,"manager":0,"price":1}`); c != 200 {
		t.Fatal("last $1 rejected", c)
	}
	if c := do("/api/pick", `{"playerId":3,"manager":0,"price":1}`); c != 400 {
		t.Fatal("roster overflow allowed")
	}
}

func TestSeedParses(t *testing.T) {
	ps, _, err := parsePlayers(seedPlayers)
	if err != nil || len(ps) < 500 {
		t.Fatalf("seed: %d players, %v", len(ps), err)
	}
	for _, p := range ps {
		if p.Name == "Connor McDavid" && p.Expert == 0 {
			t.Error("ESPN expert projection missing from seed")
		}
	}
}

func TestMinBid(t *testing.T) {
	st, _ := openStore(t.TempDir())
	h := (&server{st: st}).routes()
	do := func(path, body string) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return w.Code
	}
	do("/api/import", `{"text":"Name\nA\nB"}`)
	if c := do("/api/settings", `{"managers":["x","y"],"me":0,"rosterSize":7,"budget":100,"minBid":4}`); c != 200 {
		t.Fatal("settings", c)
	}
	if c := do("/api/pick", `{"playerId":1,"manager":0,"price":3}`); c != 400 {
		t.Fatal("bid under the $4 minimum allowed")
	}
	// $100 with 6 more spots to fill at $4 each: max bid is $76.
	if c := do("/api/pick", `{"playerId":1,"manager":0,"price":77}`); c != 400 {
		t.Fatal("bid over $76 allowed")
	}
	if c := do("/api/pick", `{"playerId":1,"manager":0,"price":76}`); c != 200 {
		t.Fatal("$76 rejected", c)
	}
}

func TestGoneAndUnpick(t *testing.T) {
	st, _ := openStore(t.TempDir())
	h := (&server{st: st}).routes()
	do := func(path, body string) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return w.Code
	}
	do("/api/import", `{"text":"Name\nA\nB\nC"}`)
	if c := do("/api/pick", `{"playerId":1,"manager":-1}`); c != 200 {
		t.Fatal("mark gone without price rejected", c)
	}
	if c := do("/api/pick", `{"playerId":2,"manager":0,"price":10}`); c != 200 {
		t.Fatal("my pick rejected", c)
	}
	if c := do("/api/unpick", `{"playerId":1}`); c != 200 {
		t.Fatal("unpick", c)
	}
	s := st.get()
	if len(s.Picks) != 1 || s.Picks[0].PlayerID != 2 || s.Picks[0].Overall != 1 {
		t.Fatalf("after unpick: %+v", s.Picks)
	}
	if c := do("/api/unpick", `{"playerId":3}`); c != 400 {
		t.Fatal("unpick of undrafted player allowed")
	}
}

func TestLoginsAndPrivacy(t *testing.T) {
	st, _ := openStore(t.TempDir())
	sv := &server{st: st, adminUser: "kris", adminHash: hashPassword("admin-pw"), guestHash: hashPassword("guest-pw")}
	h := sv.routes()
	req := func(method, path, body, name, pw string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.SetBasicAuth(name, pw)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := req("POST", "/api/import", `{"text":"Name,Tag,Why\nA,target,secret plan\nB,,"}`, "Kris", "admin-pw"); w.Code != 200 {
		t.Fatalf("admin import: %d %s", w.Code, w.Body)
	}
	for _, c := range []struct{ name, pw string }{{"kris", "guest-pw"}, {"toad", "admin-pw"}, {"toad", "wrong"}, {"", "guest-pw"}} {
		if w := req("GET", "/api/state", "", c.name, c.pw); w.Code != 401 {
			t.Errorf("%s/%s: got %d, want 401", c.name, c.pw, w.Code)
		}
	}
	w := req("GET", "/api/state", "", "toad", "guest-pw")
	if w.Code != 200 {
		t.Fatalf("guest state: %d", w.Code)
	}
	if b := w.Body.String(); strings.Contains(b, "secret plan") || strings.Contains(b, `"target"`) || !strings.Contains(b, `"admin":false`) {
		t.Errorf("guest sees private data: %s", b)
	}
	if w := req("POST", "/api/pick", `{"playerId":1,"manager":-1}`, "toad", "guest-pw"); w.Code != 403 {
		t.Errorf("guest pick: %d, want 403", w.Code)
	}
	w = req("GET", "/api/state", "", "kris", "admin-pw")
	if b := w.Body.String(); !strings.Contains(b, "secret plan") || !strings.Contains(b, `"target"`) {
		t.Errorf("admin lost private data: %s", b)
	}
	// The admin can clear a list flag for themselves.
	req("POST", "/api/player", `{"id":1,"tag":"","why":""}`, "kris", "admin-pw")
	if b := req("GET", "/api/state", "", "kris", "admin-pw").Body.String(); strings.Contains(b, `"target"`) {
		t.Errorf("flag not cleared: %s", b)
	}
	if !checkHash("x", hashPassword("x")) || checkHash("y", hashPassword("x")) {
		t.Error("hash round trip")
	}
}

func TestReorderKeepsPicks(t *testing.T) {
	st, _ := openStore(t.TempDir())
	h := (&server{st: st}).routes()
	do := func(path, body string) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return w.Code
	}
	do("/api/import", `{"text":"Name\nA\nB"}`)
	do("/api/settings", `{"managers":["x","y","z"],"me":0,"rosterSize":7,"budget":100,"minBid":4}`)
	do("/api/pick", `{"playerId":1,"manager":2,"price":10}`) // z buys A
	if c := do("/api/settings", `{"managers":["z","x","y"],"me":1,"rosterSize":7,"budget":100,"minBid":4}`); c != 200 {
		t.Fatal("reorder", c)
	}
	s := st.get()
	if s.Managers[s.Picks[0].Manager] != "z" {
		t.Fatalf("pick moved to %s", s.Managers[s.Picks[0].Manager])
	}
	if c := do("/api/settings", `{"managers":["x","y"],"me":0,"rosterSize":7,"budget":100,"minBid":4}`); c != 400 {
		t.Fatal("removing a team with picks allowed")
	}
}

func TestRepick(t *testing.T) {
	st, _ := openStore(t.TempDir())
	h := (&server{st: st}).routes()
	do := func(path, body string) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return w.Code
	}
	do("/api/import", `{"text":"Name\nA\nB"}`)
	do("/api/settings", `{"managers":["x","y"],"me":0,"rosterSize":7,"budget":100,"minBid":4}`)
	do("/api/pick", `{"playerId":1,"manager":-1}`)
	if c := do("/api/repick", `{"playerId":1,"manager":1,"price":3}`); c != 400 {
		t.Fatal("repick under minimum allowed")
	}
	if c := do("/api/repick", `{"playerId":1,"manager":1,"price":30}`); c != 200 {
		t.Fatal("repick", c)
	}
	if p := st.get().Picks[0]; p.Manager != 1 || p.Price != 30 {
		t.Fatalf("repick result %+v", p)
	}
	// Re-pricing the same buy doesn't count the old price against the budget.
	if c := do("/api/repick", `{"playerId":1,"manager":1,"price":76}`); c != 200 {
		t.Fatal("re-price to max", c)
	}
}
