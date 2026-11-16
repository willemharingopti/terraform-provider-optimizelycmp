// Package mockcmp is a stateful in-memory fake of the parts of the Optimizely CMP REST API that this provider uses.
// It runs over TLS (the provider refuses plain http) and lets the resources be tested end to end in CI without an account.
//
// It is not a general CMP emulator: it reproduces what the provider relies on, including the quirks found by testing the
// real API (the settings import matching by name and never deleting, UI-created label groups known by a slug, defaults the
// API rewrites, values it silently drops, webhook events it rejects). When the real API surprises the provider, teach the
// mock the same behaviour and add a test, so the surprise stays covered.
package mockcmp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	ClientID     = "mock-client"
	ClientSecret = "mock-secret"
	accessToken  = "mock-token"
)

// Server is a running mock. Use URL as base_url and URL+"/token" as token_url; Client() trusts its certificate.
type Server struct {
	*httptest.Server
	mu  sync.Mutex
	seq int

	folders      map[string]*folder
	folderOrder  []string
	perms        map[string][]*permission
	users        []*user
	teams        []*team
	workflows    []*workflow
	templates    []*template
	channels     []*channel
	categories   []*category
	contentTypes map[string]*contentType
	ctOrder      []string
	fields       []*field
	groups       []*labelGroup
	apps         []*app
	hooks        []*hook
	rules        []*rule

	Gets map[string]int // GET calls per path, so tests can assert on caching
}

type folder struct {
	ID, Name, Parent  string
	Created, Modified string
}
type permission struct {
	ID, Type, Name, Access string
	Owner                  bool
}
type user struct {
	ID, First, Last, Email string
	Roles                  []string
}
type team struct {
	ID, Name string
	Users    []string
}
type workflow struct {
	ID, Name string
	Steps    []string
}
type template struct {
	ID, Title, Description string
	Active                 bool
	ApplicableTo           []string
}
type channel struct{ ID, Name string }
type category struct {
	ID, Name string
	Criteria []string
}
type version struct {
	GUID    string
	Locales []string
	Defs    []map[string]any
}
type contentType struct {
	GUID, Name, Description, Thumbnail, CreatedBy, UpdatedBy, Created, Updated string
	Component, Disabled                                                        bool
	Versions                                                                   []*version
}
type choice struct {
	ID, Name, Color string
	Active          bool
}
type field struct {
	ID, Name, Helper, Type, Currency string
	Active, Multi                    bool
	Thousand                         bool
	Decimals                         int
	Choices                          []*choice
}
type labelGroup struct {
	ID, Display, LabelType string // LabelType is what the import matches on; for a UI-created group it is a slug of Display
	Single                 bool
	Labels                 []label
}
type label struct{ ID, Name, Color string }
type app struct {
	Name, Description, Homepage, Env string
	Callbacks                        []string
	ExposeEmail                      bool
}
type hook struct {
	Name, Description, URL, Secret string
	Events                         []string
}
type rule struct {
	Name, Description string
	Template          *string
	Watchers          []string
	Conditions        []map[string]any
}

// acceptedEvents are the webhook events the real settings import accepted when tested. It rejected the rest.
var acceptedEvents = map[string]bool{
	"asset_added": true, "asset_modified": true, "asset_removed": true, "external_sub_step_comment_added": true,
	"external_sub_step_comment_modified": true, "external_sub_step_comment_removed": true, "external_sub_step_completed": true,
	"external_sub_step_modified": true, "external_sub_step_started": true, "task_asset_added": true, "task_asset_draft_added": true,
	"task_asset_draft_removed": true, "task_asset_modified": true, "task_asset_removed": true, "task_brief_added": true,
	"task_brief_modified": true, "task_brief_removed": true, "task_custom_field_modified": true, "task_metadata_modified": true,
	"task_removed": true,
}

// labelColors are the colours the real API accepted for labels.
var labelColors = map[string]bool{"#4ECFD5": true, "#FFC700": true, "#FF98A7": true, "#5FEEAD": true, "#D6C4F2": true,
	"#5F9FF6": true, "#CB5DEB": true, "#9694B3": true}

var fieldTypes = map[string]bool{"text": true, "text_area": true, "rich_text": true, "date": true, "image": true, "video": true,
	"simple_number": true, "percentage_number": true, "currency_number": true, "dropdown": true, "label": true, "checkbox": true, "radio_button": true}

// New starts a mock with a little seed data: two users, two teams, a workflow, a template, a channel and one label group
// created "in the UI" (so the import knows it by the slug content_pillar).
func New(t testing.TB) *Server {
	s := &Server{
		folders: map[string]*folder{}, perms: map[string][]*permission{}, contentTypes: map[string]*contentType{}, Gets: map[string]int{},
	}
	s.users = []*user{
		{ID: s.id(), First: "Ada", Last: "Lovelace", Email: "ada@example.invalid", Roles: []string{"Creator"}},
		{ID: s.id(), First: "Grace", Last: "Hopper", Email: "grace@example.invalid", Roles: []string{"NC Admin"}},
	}
	s.teams = []*team{{ID: s.id(), Name: "Mock Team", Users: []string{s.users[0].ID}}, {ID: s.id(), Name: "Other Team"}}
	s.workflows = []*workflow{{ID: s.id(), Name: "Mock Workflow", Steps: []string{"Draft", "Review"}}}
	s.templates = []*template{{ID: s.id(), Title: "Mock Template", Description: "d", Active: true, ApplicableTo: []string{"work_request"}}}
	s.channels = []*channel{{ID: s.id(), Name: "Web"}}
	s.categories = []*category{{ID: s.id(), Name: "Tone", Criteria: []string{"Friendly"}}}
	s.groups = []*labelGroup{{ID: s.id(), Display: "Content Pillar", LabelType: "content_pillar"}}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", s.token)
	s.routes(mux)
	s.Server = httptest.NewTLSServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *Server) id() string {
	s.seq++
	return fmt.Sprintf("%024x", s.seq)
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

func js(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func apiErr(w http.ResponseWriter, status int, msg string) {
	js(w, status, map[string]any{"code": status, "status": http.StatusText(status), "message": msg,
		"details": map[string]any{"code": status, "errors": []map[string]string{{"message": msg}}}})
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != ClientID || r.Form.Get("client_secret") != ClientSecret {
		http.Error(w, "invalid_client", http.StatusUnauthorized)
		return
	}
	js(w, http.StatusOK, map[string]any{"access_token": accessToken, "expires_in": 3600, "token_type": "Bearer"})
}

// h registers an authenticated, serialised handler.
func (s *Server) h(mux *http.ServeMux, pattern string, fn func(w http.ResponseWriter, r *http.Request)) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+accessToken {
			apiErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Method == http.MethodGet {
			s.Gets[r.URL.Path]++
		}
		fn(w, r)
	})
}

func body(r *http.Request) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	_ = json.NewDecoder(r.Body).Decode(&m)
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return m
}

func str(m map[string]json.RawMessage, k string) (string, bool) {
	raw, ok := m[k]
	if !ok || string(raw) == "null" {
		return "", ok
	}
	var v string
	_ = json.Unmarshal(raw, &v)
	return v, true
}

func boolean(m map[string]json.RawMessage, k string) (bool, bool) {
	raw, ok := m[k]
	if !ok {
		return false, false
	}
	var v bool
	_ = json.Unmarshal(raw, &v)
	return v, true
}

// paged answers a list endpoint with the offset/page_size pagination the real API uses.
func paged(w http.ResponseWriter, r *http.Request, total int, item func(i int) any) {
	q := r.URL.Query()
	off, _ := strconv.Atoi(q.Get("offset"))
	size, _ := strconv.Atoi(q.Get("page_size"))
	if size <= 0 {
		size = 10
	}
	if size > 100 {
		size = 100
	}
	data := []any{}
	for i := off; i < total && i < off+size; i++ {
		data = append(data, item(i))
	}
	var next any
	if off+size < total {
		next = fmt.Sprintf("https://%s%s?offset=%d&page_size=%d", r.Host, r.URL.Path, off+size, size)
	}
	js(w, http.StatusOK, map[string]any{"data": data, "pagination": map[string]any{"next": next, "previous": nil}})
}

func pagedList[T any](w http.ResponseWriter, r *http.Request, items []T, conv func(T) any) {
	paged(w, r, len(items), func(i int) any { return conv(items[i]) })
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
