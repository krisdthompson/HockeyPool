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
		ps, err := parsePlayers(text)
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
	if _, err := parsePlayers("Team,GP\nEDM,3\n"); err == nil {
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
	h := (&server{st: st, password: "pw"}).routes()
	do := func(path, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if auth {
			r.SetBasicAuth("", "pw")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := do("/api/undo", "{}", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: %d", w.Code)
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
		t.Fatalf("import mid-draft allowed: %d", w.Code)
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
