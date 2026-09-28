// Command hockeypool serves a small live draft tracker for a points-only
// hockey pool. State lives in one JSON file so it survives restarts when
// DATA_DIR points at a persistent volume.
package main

import (
	"cmp"
	"crypto/sha256"
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed static
var staticFiles embed.FS

// The organizer's list, converted by players/convert.py. Loaded when the
// state has no players yet.
//
//go:embed players/players.csv
var seedPlayers string

type Player struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Team   string  `json:"team"`
	Pos    string  `json:"pos"`
	GP     int     `json:"gp"`
	G      int     `json:"g"`
	A      int     `json:"a"`
	Pts    int     `json:"pts"`
	Proj   float64 `json:"proj"`   // projected season points; 0 = derive from pace
	Expert float64 `json:"expert"` // expert-implied points (ESPN tiers); 0 = none
	Miss   int     `json:"miss"`   // expected games missed to injury; 0 = unknown
	Rookie bool    `json:"rookie"`
	Injury string  `json:"injury"` // free text: "", "DTD", "IR", "LTIR - back Dec", ...
	Tag    string  `json:"tag"`    // the list's default flag: "", "target", "avoid" or "caution" (admin only)
	Why    string  `json:"why"`    // reason for the flag (admin only)
	Note   string  `json:"note"`
}

type Pick struct {
	Overall  int       `json:"overall"` // 1-based
	PlayerID int       `json:"playerId"`
	Manager  int       `json:"manager"`
	Price    int       `json:"price,omitempty"` // auction mode only
	At       time.Time `json:"at"`
}

type State struct {
	Version    int      `json:"version"`
	Managers   []string `json:"managers"`
	Me         int      `json:"me"`
	RosterSize int      `json:"rosterSize"`
	Snake      bool     `json:"snake"`
	MaxTeams   int      `json:"maxTeams"`   // 0 = no limit; soft cap on distinct NHL teams on my roster
	Budget     int      `json:"budget"`     // 0 = snake/straight draft; >0 = auction with this much per manager
	MinBid     int      `json:"minBid"`     // auction: smallest legal bid (0 means $1)
	AuctionSet bool     `json:"auctionSet"` // budget was chosen in Setup; don't apply the $100 default again
	SeedHash   string   `json:"seedHash"`   // which built-in list the players came from
	Players    []Player `json:"players"`
	Picks      []Pick   `json:"picks"`

	Prefs map[string]Prefs `json:"prefs,omitempty"` // per-user team choice and flags
	You   *user            `json:"you,omitempty"`   // only in responses: who is asking
}

type store struct {
	mu   sync.Mutex
	path string
	s    State
}

// The pool teams on the organizer's sheet, in sheet order.
var poolTeams = []string{"Toad", "Sniffer", "Schlitter", "Rory", "Hoop", "Billy", "Jimbo", "Dag", "Longarm", "Alden", "Albert", "LayJazz", "Hardy", "Smitty"}

// The placeholder list the first deploy started with.
var placeholderTeams = []string{"Me", "Team 2", "Team 3", "Team 4", "Team 5", "Team 6", "Team 7", "Team 8"}

func defaultState() State {
	return State{
		Managers:   slices.Clone(poolTeams),
		RosterSize: 7,
		Snake:      true,
		Budget:     100,
		MinBid:     4,
		Players:    []Player{},
		Picks:      []Pick{},
	}
}

func openStore(dir string) (*store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	st := &store{path: filepath.Join(dir, "state.json"), s: defaultState()}
	b, err := os.ReadFile(st.path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &st.s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", st.path, err)
	}
	return st, nil
}

// update applies fn under the lock and persists the result if fn succeeds.
func (st *store) update(fn func(s *State) error) (State, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	next := st.s
	next.Players = slices.Clone(st.s.Players)
	next.Picks = slices.Clone(st.s.Picks)
	next.Managers = slices.Clone(st.s.Managers)
	next.Prefs = maps.Clone(st.s.Prefs)
	if err := fn(&next); err != nil {
		return st.s, err
	}
	// The UI expects arrays, never null.
	if next.Players == nil {
		next.Players = []Player{}
	}
	if next.Picks == nil {
		next.Picks = []Pick{}
	}
	next.Version++
	b, err := json.MarshalIndent(next, "", " ")
	if err != nil {
		return st.s, err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return st.s, err
	}
	if err := os.Rename(tmp, st.path); err != nil {
		return st.s, err
	}
	st.s = next
	return next, nil
}

func (st *store) get() State {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s
}

// onClock returns the manager index that makes the given 0-based pick.
func onClock(s *State, pick int) int {
	n := len(s.Managers)
	if n == 0 {
		return 0
	}
	round, idx := pick/n, pick%n
	if s.Snake && round%2 == 1 {
		return n - 1 - idx
	}
	return idx
}

var headerAliases = map[string]string{
	"name": "name", "player": "name", "player name": "name",
	"team": "team", "tm": "team", "nhl team": "team",
	"pos": "pos", "position": "pos",
	"gp": "gp", "games": "gp", "games played": "gp",
	"g": "g", "goals": "g",
	"a": "a", "assists": "a",
	"pts": "pts", "p": "pts", "points": "pts",
	"expert": "expert", "espn": "expert", "expert proj": "expert",
	"proj": "proj", "projected": "proj", "projection": "proj", "proj pts": "proj", "projected points": "proj",
	"miss": "miss", "games missed": "miss", "missed": "miss",
	"rookie": "rookie", "rk": "rookie", "rook": "rookie",
	"injury": "injury", "inj": "injury", "status": "injury", "injury status": "injury",
	"note": "note", "notes": "note", "comment": "note",
	"tag": "tag", "flag": "tag",
	"why": "why", "reason": "why",
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "y", "yes", "true", "1", "x", "r", "rookie", "✓":
		return true
	}
	return false
}

func num(v string) float64 {
	v = strings.TrimSpace(strings.ReplaceAll(v, ",", ""))
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

// parsePlayers reads CSV or TSV (pasted straight from a spreadsheet) with a
// header row. Only a name column is required.
func parsePlayers(text string) ([]Player, map[string]bool, error) {
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if text == "" {
		return nil, nil, errors.New("no player data")
	}
	first, _, _ := strings.Cut(text, "\n")
	r := csv.NewReader(strings.NewReader(text))
	if strings.Count(first, "\t") > strings.Count(first, ",") {
		r.Comma = '\t'
	}
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		if k, ok := headerAliases[strings.ToLower(strings.TrimSpace(h))]; ok {
			if _, dup := col[k]; !dup {
				col[k] = i
			}
		}
	}
	if _, ok := col["name"]; !ok {
		return nil, nil, fmt.Errorf("no name column found in header %q", rows[0])
	}
	field := func(row []string, k string) string {
		i, ok := col[k]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	var out []Player
	for _, row := range rows[1:] {
		name := field(row, "name")
		if name == "" {
			continue
		}
		if pos := strings.ToUpper(field(row, "pos")); pos == "G" || pos == "GOALIE" {
			continue // points-only pool: goalies aren't drafted
		}
		p := Player{
			ID:     len(out) + 1,
			Name:   name,
			Team:   strings.ToUpper(field(row, "team")),
			Pos:    strings.ToUpper(field(row, "pos")),
			GP:     int(num(field(row, "gp"))),
			G:      int(num(field(row, "g"))),
			A:      int(num(field(row, "a"))),
			Pts:    int(num(field(row, "pts"))),
			Proj:   num(field(row, "proj")),
			Expert: num(field(row, "expert")),
			Miss:   int(num(field(row, "miss"))),
			Rookie: truthy(field(row, "rookie")),
			Injury: field(row, "injury"),
			Note:   field(row, "note"),
			Tag:    normTag(field(row, "tag")),
			Why:    field(row, "why"),
		}
		if p.Pts == 0 {
			p.Pts = p.G + p.A
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, nil, errors.New("no player rows found")
	}
	has := map[string]bool{}
	for k := range col {
		has[k] = true
	}
	return out, has, nil
}

func nameKey(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(s, ".", ""))), " ")
}

// mergePlayers updates existing players matched by name (and team, when the
// new list has one and the name is ambiguous) with only the columns the new
// list has, and appends players it doesn't know. IDs never change, so picks
// stay valid.
func mergePlayers(s *State, in []Player, has map[string]bool) (updated, added int) {
	byName := map[string][]int{}
	nextID := 0
	for i, p := range s.Players {
		byName[nameKey(p.Name)] = append(byName[nameKey(p.Name)], i)
		nextID = max(nextID, p.ID)
	}
	for _, np := range in {
		idx := -1
		for _, i := range byName[nameKey(np.Name)] {
			if idx == -1 || (has["team"] && s.Players[i].Team == np.Team) {
				idx = i
			}
		}
		if idx == -1 {
			nextID++
			np.ID = nextID
			s.Players = append(s.Players, np)
			added++
			continue
		}
		p := &s.Players[idx]
		if has["team"] && np.Team != "" {
			p.Team = np.Team
		}
		if has["pos"] && np.Pos != "" {
			p.Pos = np.Pos
		}
		if has["gp"] {
			p.GP = np.GP
		}
		if has["g"] {
			p.G = np.G
		}
		if has["a"] {
			p.A = np.A
		}
		if has["pts"] || has["g"] || has["a"] {
			p.Pts = np.Pts
		}
		if has["proj"] {
			p.Proj = np.Proj
		}
		if has["expert"] {
			p.Expert = np.Expert
		}
		if has["miss"] {
			p.Miss = np.Miss
		}
		if has["rookie"] {
			p.Rookie = np.Rookie
		}
		if has["injury"] {
			p.Injury = np.Injury
		}
		if has["tag"] {
			p.Tag = np.Tag
		}
		if has["why"] {
			p.Why = np.Why
		}
		if has["note"] && np.Note != "" {
			p.Note = np.Note
		}
		updated++
	}
	return updated, added
}

// checkBuy validates manager m buying one more player at price and
// returns the price to record (0 outside auctions).
func checkBuy(s *State, m, price int) (int, error) {
	n := 0
	for _, k := range s.Picks {
		if k.Manager == m {
			n++
		}
	}
	if n >= s.RosterSize {
		return 0, fmt.Errorf("%s already has %d players", s.Managers[m], n)
	}
	if s.Budget == 0 {
		return 0, nil
	}
	if price < minBid(s) {
		return 0, fmt.Errorf("auction: price must be at least $%d", minBid(s))
	}
	if mb := maxBid(s, m); price > mb {
		return 0, fmt.Errorf("%s can bid at most $%d", s.Managers[m], mb)
	}
	return price, nil
}

func minBid(s *State) int {
	return max(1, s.MinBid)
}

// maxBid is the most a manager can bid while keeping the minimum bid for
// each other open slot.
func maxBid(s *State, m int) int {
	spent, n := 0, 0
	for _, k := range s.Picks {
		if k.Manager == m {
			spent += k.Price
			n++
		}
	}
	return s.Budget - spent - minBid(s)*max(0, s.RosterSize-n-1)
}

type server struct {
	st        *store
	adminUser string // lower case
	adminPW   string // plain password from a secret, optional
	adminHash string // pbkdf2 hash, optional
	guestHash string // pbkdf2 hash for guests; empty = no guests
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// mutate decodes the request body into req and applies fn to the state.
// Only the admin changes anything.
func mutate[T any](sv *server, fn func(s *State, req T, u user) error) http.HandlerFunc {
	return adminOnly(func(w http.ResponseWriter, r *http.Request) {
		var req T
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json: " + err.Error()})
			return
		}
		u := userOf(r)
		s, err := sv.st.update(func(s *State) error { return fn(s, req, u) })
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, view(s, u))
	})
}

func isDrafted(s *State, id int) bool {
	for _, p := range s.Picks {
		if p.PlayerID == id {
			return true
		}
	}
	return false
}

type settingsReq struct {
	Managers   []string `json:"managers"`
	Me         int      `json:"me"`
	RosterSize int      `json:"rosterSize"`
	Snake      bool     `json:"snake"`
	MaxTeams   int      `json:"maxTeams"`
	Budget     int      `json:"budget"`
	MinBid     int      `json:"minBid"`
}

type importReq struct {
	Text string `json:"text"`
	Mode string `json:"mode"` // "replace" (default) or "merge"
}

type pickReq struct {
	PlayerID int  `json:"playerId"`
	Manager  *int `json:"manager"` // nil = whoever is on the clock; -1 = gone, buyer unknown
	Price    int  `json:"price"`
}

// Gone marks a pick whose buyer wasn't recorded.
const Gone = -1

// normTag keeps only the tags the page knows how to show.
func normTag(t string) string {
	switch t = strings.ToLower(strings.TrimSpace(t)); t {
	case "target", "avoid", "caution":
		return t
	}
	return ""
}

type playerEditReq struct {
	Tag    string `json:"tag"`
	Why    string `json:"why"`
	ID     int    `json:"id"`
	Rookie bool   `json:"rookie"`
	Injury string `json:"injury"`
	Miss   int    `json:"miss"`
	Note   string `json:"note"`
}

func (sv *server) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFiles, "static")
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		s := sv.st.get()
		if v, err := strconv.Atoi(r.URL.Query().Get("since")); err == nil && v == s.Version {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, view(s, userOf(r)))
	})

	mux.HandleFunc("GET /api/export", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="draft-%s.json"`, time.Now().Format("20060102-1504")))
		writeJSON(w, http.StatusOK, sv.st.get())
	}))

	mux.HandleFunc("POST /api/restore", mutate(sv, func(s *State, req State, _ user) error {
		if len(req.Managers) == 0 {
			return errors.New("backup has no managers")
		}
		v := s.Version
		*s = req
		s.Version, s.You = v, nil
		return nil
	}))

	mux.HandleFunc("POST /api/settings", mutate(sv, func(s *State, req settingsReq, u user) error {
		var names []string
		for _, m := range req.Managers {
			if m = strings.TrimSpace(m); m != "" {
				names = append(names, m)
			}
		}
		if len(names) < 2 {
			return errors.New("need at least two managers")
		}
		if req.Me < 0 || req.Me >= len(names) {
			return errors.New("your draft slot is out of range")
		}
		if req.RosterSize < 1 || req.RosterSize > 40 {
			return errors.New("roster size must be 1-40")
		}
		if req.Budget < 0 || req.MinBid < 0 {
			return errors.New("budget and minimum bid can't be negative")
		}
		if req.Budget > 0 && max(1, req.MinBid)*req.RosterSize > req.Budget {
			return errors.New("minimum bid × players each is more than the budget")
		}
		// Picks point at teams by position, so follow each team to its new
		// place when the list is reordered or renamed in place.
		newIdx := map[string]int{}
		for i, n := range names {
			if _, dup := newIdx[strings.ToLower(n)]; dup {
				return fmt.Errorf("%q is listed twice", n)
			}
			newIdx[strings.ToLower(n)] = i
		}
		remap := make([]int, len(s.Managers))
		for i, n := range s.Managers {
			j, ok := newIdx[strings.ToLower(n)]
			if !ok {
				j = len(names) // removed
				if len(names) == len(s.Managers) {
					j = i // same size: a rename keeps its position
				}
			}
			remap[i] = j
		}
		picks := slices.Clone(s.Picks)
		for i, p := range picks {
			if p.Manager == Gone {
				continue
			}
			if remap[p.Manager] >= len(names) {
				return fmt.Errorf("%s has picks; undo them before removing the team", s.Managers[p.Manager])
			}
			picks[i].Manager = remap[p.Manager]
		}
		s.Picks = picks
		for name, pr := range s.Prefs {
			if pr.Me >= 0 && pr.Me < len(remap) && name != u.Name {
				pr.Me = remap[pr.Me]
				s.Prefs[name] = pr
			}
		}
		s.Managers, s.Me, s.RosterSize, s.Snake, s.MaxTeams, s.Budget = names, req.Me, req.RosterSize, req.Snake, req.MaxTeams, req.Budget
		s.MinBid = req.MinBid
		s.AuctionSet = true
		setPrefs(s, u, func(p *Prefs) { p.Me = req.Me })
		return nil
	}))

	mux.HandleFunc("POST /api/import", mutate(sv, func(s *State, req importReq, _ user) error {
		players, has, err := parsePlayers(req.Text)
		if err != nil {
			return err
		}
		if req.Mode == "merge" {
			mergePlayers(s, players, has)
			return nil
		}
		if len(s.Picks) > 0 {
			return errors.New("picks already made; use Merge, or reset the draft before replacing players")
		}
		s.Players = players
		return nil
	}))

	mux.HandleFunc("POST /api/reseed", mutate(sv, func(s *State, _ struct{}, _ user) error {
		if len(s.Picks) > 0 {
			return errors.New("picks already made; reset the draft first")
		}
		players, _, err := parsePlayers(seedPlayers)
		if err != nil {
			return err
		}
		s.Players = players
		s.SeedHash = seedHash()
		return nil
	}))

	mux.HandleFunc("POST /api/pick", mutate(sv, func(s *State, req pickReq, _ user) error {
		if len(s.Picks) >= len(s.Managers)*s.RosterSize {
			return errors.New("draft is complete")
		}
		found := false
		for _, p := range s.Players {
			found = found || p.ID == req.PlayerID
		}
		if !found {
			return errors.New("unknown player")
		}
		if isDrafted(s, req.PlayerID) {
			return errors.New("player already drafted")
		}
		m := onClock(s, len(s.Picks))
		if req.Manager != nil {
			if *req.Manager < Gone || *req.Manager >= len(s.Managers) {
				return errors.New("unknown manager")
			}
			m = *req.Manager
		} else if s.Budget > 0 {
			return errors.New("auction: choose who won the player")
		}
		if m == Gone {
			// Just off the board: the buyer and price are optional.
			s.Picks = append(s.Picks, Pick{Overall: len(s.Picks) + 1, PlayerID: req.PlayerID, Manager: Gone, Price: max(0, req.Price), At: time.Now()})
			return nil
		}
		price, err := checkBuy(s, m, req.Price)
		if err != nil {
			return err
		}
		s.Picks = append(s.Picks, Pick{Overall: len(s.Picks) + 1, PlayerID: req.PlayerID, Manager: m, Price: price, At: time.Now()})
		return nil
	}))

	// Change who bought an already-drafted player, and for how much.
	mux.HandleFunc("POST /api/repick", mutate(sv, func(s *State, req pickReq, _ user) error {
		i := slices.IndexFunc(s.Picks, func(k Pick) bool { return k.PlayerID == req.PlayerID })
		if i < 0 {
			return errors.New("player isn't drafted")
		}
		if req.Manager == nil || *req.Manager < Gone || *req.Manager >= len(s.Managers) {
			return errors.New("unknown manager")
		}
		m, price := *req.Manager, max(0, req.Price)
		if m != Gone {
			others := *s
			others.Picks = slices.Delete(slices.Clone(s.Picks), i, i+1)
			var err error
			if price, err = checkBuy(&others, m, req.Price); err != nil {
				return err
			}
		}
		s.Picks[i].Manager, s.Picks[i].Price = m, price
		return nil
	}))

	mux.HandleFunc("POST /api/unpick", mutate(sv, func(s *State, req pickReq, _ user) error {
		for i, k := range s.Picks {
			if k.PlayerID == req.PlayerID {
				s.Picks = append(s.Picks[:i], s.Picks[i+1:]...)
				for j := i; j < len(s.Picks); j++ {
					s.Picks[j].Overall = j + 1
				}
				return nil
			}
		}
		return errors.New("player isn't drafted")
	}))

	mux.HandleFunc("POST /api/undo", mutate(sv, func(s *State, _ struct{}, _ user) error {
		if len(s.Picks) == 0 {
			return errors.New("nothing to undo")
		}
		s.Picks = s.Picks[:len(s.Picks)-1]
		return nil
	}))

	mux.HandleFunc("POST /api/reset", mutate(sv, func(s *State, _ struct{}, _ user) error {
		s.Picks = []Pick{}
		return nil
	}))

	mux.HandleFunc("POST /api/player", mutate(sv, func(s *State, req playerEditReq, u user) error {
		for i := range s.Players {
			if s.Players[i].ID == req.ID {
				p := &s.Players[i]
				p.Rookie, p.Injury, p.Miss, p.Note = req.Rookie, strings.TrimSpace(req.Injury), req.Miss, strings.TrimSpace(req.Note)
				p.Why = strings.TrimSpace(req.Why)
				setPrefs(s, u, func(pr *Prefs) { pr.Tags[req.ID] = normTag(req.Tag) })
				return nil
			}
		}
		return errors.New("unknown player")
	}))

	return sv.auth(mux)
}

func seedHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(seedPlayers)))[:12]
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "hash" {
		fmt.Println(hashPassword(os.Args[2])) // for DRAFT_ADMIN_HASH / DRAFT_GUEST_HASH
		return
	}
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		dir = "data"
	}
	st, err := openStore(dir)
	if err != nil {
		log.Fatal(err)
	}
	// Bring a draft created by an earlier version up to the pool's setup
	// (14 teams, $100 auction), as long as no picks have been made.
	if s := st.get(); s.MinBid == 0 {
		// The pool's minimum bid is $4; drafts saved before it existed get it.
		if _, err := st.update(func(s *State) error { s.MinBid = 4; return nil }); err != nil {
			log.Fatal(err)
		}
	}
	if s := st.get(); len(s.Picks) == 0 && (slices.Equal(s.Managers, placeholderTeams) || s.Budget == 0 && !s.AuctionSet) {
		if _, err := st.update(func(s *State) error {
			if slices.Equal(s.Managers, placeholderTeams) {
				s.Managers = slices.Clone(poolTeams)
			}
			if s.Budget == 0 && !s.AuctionSet {
				s.Budget = 100
			}
			s.AuctionSet = true
			return nil
		}); err != nil {
			log.Fatal(err)
		}
	}
	// Before the first pick, a deploy with a changed built-in list replaces
	// the players. Once the draft starts, lists only change via Setup.
	if s := st.get(); len(s.Players) == 0 || (len(s.Picks) == 0 && s.SeedHash != seedHash()) {
		players, _, err := parsePlayers(seedPlayers)
		if err != nil {
			log.Fatalf("seed players: %v", err)
		}
		if _, err := st.update(func(s *State) error { s.Players, s.SeedHash = players, seedHash(); return nil }); err != nil {
			log.Fatal(err)
		}
		log.Printf("loaded %d players from the built-in list", len(players))
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	sv := &server{
		st:        st,
		adminUser: strings.ToLower(cmp.Or(os.Getenv("DRAFT_ADMIN_USER"), "kris")),
		adminPW:   os.Getenv("DRAFT_PASSWORD"),
		adminHash: os.Getenv("DRAFT_ADMIN_HASH"),
		guestHash: os.Getenv("DRAFT_GUEST_HASH"),
	}
	if sv.open() {
		log.Printf("no passwords configured: the site is open to everyone")
	}
	log.Printf("hockeypool listening on :%s, state in %s", port, st.path)
	log.Fatal(http.ListenAndServe(":"+port, sv.routes()))
}
