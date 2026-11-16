package mockcmp

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
)

var sections = []string{"labels", "custom_fields", "workflows", "templates", "apps", "webhooks", "routing_rules"}

// export builds the GET /settings payload.
func (s *Server) export() map[string]any {
	labels := []any{}
	for _, g := range s.groups {
		ls := []any{}
		for _, l := range g.Labels {
			ls = append(ls, map[string]any{"name": l.Name, "color": l.Color})
		}
		labels = append(labels, map[string]any{"label_type": g.LabelType, "has_single_value": g.Single, "labels": ls})
	}
	cf := map[string]any{}
	for _, k := range []string{"currency", "number", "percentage", "checkbox", "dropdown", "multichoice", "multi_select_dropdown", "date", "image", "richtext", "string", "textarea", "video"} {
		cf[k] = []any{}
	}
	for _, f := range s.fields { // only active fields are exported
		if !f.Active {
			continue
		}
		key := map[string]string{"currency_number": "currency", "dropdown": "dropdown"}[f.Type]
		if key != "" {
			cf[key] = append(cf[key].([]any), map[string]any{"label": f.Name})
		}
	}
	wfs, tpls := []any{}, []any{}
	for _, w := range s.workflows {
		wfs = append(wfs, map[string]any{"name": w.Name})
	}
	for _, t := range s.templates {
		tpls = append(tpls, map[string]any{"name": t.Title, "description": t.Description})
	}
	apps, hooks, rules := []any{}, []any{}, []any{}
	for _, a := range s.apps {
		apps = append(apps, map[string]any{"name": a.Name, "description": a.Description, "homepage_url": a.Homepage,
			"authorization_callback_urls": nz(a.Callbacks), "expose_email": a.ExposeEmail, "env": a.Env})
	}
	for _, h := range s.hooks {
		hooks = append(hooks, map[string]any{"name": h.Name, "description": h.Description, "callback_url": h.URL, "secret": h.Secret, "event_names": nz(h.Events)})
	}
	for _, r := range s.rules {
		var tpl any
		if r.Template != nil {
			tpl = *r.Template
		}
		conds := r.Conditions
		if conds == nil {
			conds = []map[string]any{}
		}
		rules = append(rules, map[string]any{"name": r.Name, "description": r.Description, "template_name": tpl, "watchers": nz(r.Watchers), "rules": conds})
	}
	return map[string]any{"labels": labels, "custom_fields": cf, "workflows": wfs, "templates": tpls, "apps": apps, "webhooks": hooks, "routing_rules": rules}
}

func nz(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

type settingsErr struct {
	status int
	body   any
}

func unprocessable(msg string) *settingsErr {
	return &settingsErr{http.StatusUnprocessableEntity, map[string]any{"code": 422, "status": "Unprocessable Entity",
		"details": map[string]any{"code": 422, "errors": []map[string]string{{"message": msg}}}, "message": msg}}
}

type changes map[string]map[string][]string // "create"/"update" -> section -> names

func newChanges() changes {
	c := changes{"create": {}, "update": {}}
	for _, k := range []string{"create", "update"} {
		for _, sec := range sections {
			c[k][sec] = []string{}
		}
	}
	return c
}

func (s *Server) importSettings(w http.ResponseWriter, r *http.Request) {
	execute := r.URL.Query().Get("execute") == "true"
	top := body(r)
	raw, ok := top["resources"]
	if !ok {
		js(w, http.StatusBadRequest, map[string]any{"code": 400, "status": "Bad Request",
			"errors": map[string]any{"json": map[string]any{"resources": []string{"Missing data for required field."}}}})
		return
	}
	var res map[string]json.RawMessage
	_ = json.Unmarshal(raw, &res)
	for _, sec := range sections {
		if _, ok := res[sec]; !ok {
			js(w, http.StatusBadRequest, map[string]any{"code": 400, "status": "Bad Request",
				"errors": map[string]any{"json": map[string]any{"resources": map[string]any{sec: []string{"Missing data for required field."}}}}})
			return
		}
	}
	var in struct {
		Labels []struct {
			LabelType string `json:"label_type"`
			Single    bool   `json:"has_single_value"`
			Labels    []struct {
				Name  string `json:"name"`
				Color string `json:"color"`
			} `json:"labels"`
		}
		Apps []struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Homepage    string   `json:"homepage_url"`
			Callbacks   []string `json:"authorization_callback_urls"`
			ExposeEmail bool     `json:"expose_email"`
			Env         string   `json:"env"`
		}
		Hooks []struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			URL         string   `json:"callback_url"`
			Secret      *string  `json:"secret"`
			Events      []string `json:"event_names"`
		}
		Rules []struct {
			Name        string           `json:"name"`
			Description string           `json:"description"`
			Template    *string          `json:"template_name"`
			Watchers    []string         `json:"watchers"`
			Conditions  []map[string]any `json:"rules"`
		}
	}
	_ = json.Unmarshal(res["labels"], &in.Labels)
	_ = json.Unmarshal(res["apps"], &in.Apps)
	_ = json.Unmarshal(res["webhooks"], &in.Hooks)
	_ = json.Unmarshal(res["routing_rules"], &in.Rules)

	// validate everything before changing anything
	for _, g := range in.Labels {
		for _, l := range g.Labels {
			if !labelColors[l.Color] {
				s.fail(w, unprocessable("Invalid label color found"))
				return
			}
		}
	}
	for _, a := range in.Apps {
		switch {
		case !strings.HasPrefix(a.Homepage, "https://"):
			s.fail(w, unprocessable("App with invalid homepage URL found"))
			return
		case len(a.Callbacks) == 0:
			s.fail(w, unprocessable("App should have at least one authorization callback URL"))
			return
		case a.Env != "production":
			s.fail(w, unprocessable("App with invalid ENVIRONMENT MODE found"))
			return
		}
		for _, c := range a.Callbacks {
			if !strings.HasPrefix(c, "https://") {
				s.fail(w, unprocessable("App with invalid authorization callback URL found"))
				return
			}
		}
	}
	for _, h := range in.Hooks {
		if h.Secret == nil || *h.Secret == "" {
			js(w, http.StatusBadRequest, map[string]any{"code": 400, "status": "Bad Request",
				"errors": map[string]any{"json": map[string]any{"resources": map[string]any{"webhooks": map[string]any{"0": map[string]any{"secret": []string{"Shorter than minimum length 1."}}}}}}})
			return
		}
		if !strings.HasPrefix(h.URL, "https://") {
			s.fail(w, unprocessable("Webhook with invalid callback URL found"))
			return
		}
		for _, e := range h.Events {
			if !acceptedEvents[e] {
				s.fail(w, unprocessable("Webhook with invalid events found"))
				return
			}
		}
	}
	for _, ru := range in.Rules {
		for _, c := range ru.Conditions {
			if e := validCondition(c); e != "" {
				s.fail(w, unprocessable(e))
				return
			}
		}
	}

	cs := newChanges()
	s.importLabels(in.Labels, execute, cs)
	s.importApps(in.Apps, execute, cs)
	s.importHooks(in.Hooks, execute, cs)
	s.importRules(in.Rules, execute, cs)
	js(w, http.StatusOK, map[string]any{"resources": s.export(), "changeset": cs})
}

func (s *Server) fail(w http.ResponseWriter, e *settingsErr) { js(w, e.status, e.body) }

func validCondition(c map[string]any) string {
	ft, _ := c["field_type"].(string)
	op, _ := c["operator"].(string)
	ident, _ := c["identifier"].(string)
	unit := c["unit"]
	cft, _ := c["custom_field_type"].(string)
	switch ft {
	case "dueDate":
		// no operator was ever accepted for this type when tested against the real API
		return "Routing rule found with rule type due date which has an invalid unit OR has an identifier OR has an invalid operator"
	case "label":
		if ident == "" || unit != nil || op != "equal" {
			return "Routing rule found with invalid label rule type. It does not have an identifier OR has a unit OR has an invalid operator"
		}
	case "customField":
		if ident == "" || unit != nil || op != "equal" || !contains([]string{"dropdown", "multichoice", "multi_select_dropdown", "checkbox"}, cft) {
			return "Routing rule found with invalid custom field rule type. It does not have an identifier OR has a unit OR has an invalid operator OR has invalid custom_field_type"
		}
	default:
		return "Routing rule found with an invalid field type"
	}
	return ""
}

// The import matches by name only and never deletes. An object that matches exactly once is updated (with lists merged);
// anything else is created, including when several share the name.

func (s *Server) importLabels(items []struct {
	LabelType string `json:"label_type"`
	Single    bool   `json:"has_single_value"`
	Labels    []struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"labels"`
}, commit bool, cs changes) {
	for _, it := range items {
		var g *labelGroup
		for _, x := range s.groups {
			if x.LabelType == it.LabelType { // the import knows a UI-created group by its slug, not its display name
				g = x
			}
		}
		if g == nil {
			cs["create"]["labels"] = append(cs["create"]["labels"], it.LabelType)
			if commit {
				g = &labelGroup{ID: s.id(), Display: it.LabelType, LabelType: it.LabelType, Single: it.Single}
				s.groups = append(s.groups, g)
				s.mergeLabels(g, it.Labels)
			}
			continue
		}
		changed := g.Single != it.Single
		for _, l := range it.Labels {
			found := false
			for _, e := range g.Labels {
				if e.Name == l.Name {
					found = true
					changed = changed || e.Color != l.Color
				}
			}
			changed = changed || !found
		}
		if changed {
			cs["update"]["labels"] = append(cs["update"]["labels"], it.LabelType)
		}
		if commit {
			g.Single = it.Single
			s.mergeLabels(g, it.Labels)
		}
	}
}

func (s *Server) mergeLabels(g *labelGroup, in []struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}) {
	for _, l := range in {
		done := false
		for i := range g.Labels {
			if g.Labels[i].Name == l.Name {
				g.Labels[i].Color, done = l.Color, true
			}
		}
		if !done {
			g.Labels = append(g.Labels, label{ID: s.id(), Name: l.Name, Color: l.Color})
		}
	}
}

func union(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, x := range b {
		if !contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

func (s *Server) importApps(items []struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Homepage    string   `json:"homepage_url"`
	Callbacks   []string `json:"authorization_callback_urls"`
	ExposeEmail bool     `json:"expose_email"`
	Env         string   `json:"env"`
}, commit bool, cs changes) {
	for _, it := range items {
		var matches []*app
		for _, a := range s.apps {
			if a.Name == it.Name {
				matches = append(matches, a)
			}
		}
		if len(matches) != 1 {
			cs["create"]["apps"] = append(cs["create"]["apps"], it.Name)
			if commit {
				s.apps = append(s.apps, &app{Name: it.Name, Description: it.Description, Homepage: it.Homepage, Env: it.Env, Callbacks: it.Callbacks, ExposeEmail: it.ExposeEmail})
			}
			continue
		}
		a := matches[0]
		merged := union(a.Callbacks, it.Callbacks)
		if a.Description != it.Description || a.Homepage != it.Homepage || a.Env != it.Env || a.ExposeEmail != it.ExposeEmail || len(merged) != len(a.Callbacks) {
			cs["update"]["apps"] = append(cs["update"]["apps"], it.Name)
		}
		if commit {
			a.Description, a.Homepage, a.Env, a.ExposeEmail, a.Callbacks = it.Description, it.Homepage, it.Env, it.ExposeEmail, merged
		}
	}
}

func (s *Server) importHooks(items []struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	URL         string   `json:"callback_url"`
	Secret      *string  `json:"secret"`
	Events      []string `json:"event_names"`
}, commit bool, cs changes) {
	for _, it := range items {
		var matches []*hook
		for _, h := range s.hooks {
			if h.Name == it.Name {
				matches = append(matches, h)
			}
		}
		if len(matches) != 1 {
			cs["create"]["webhooks"] = append(cs["create"]["webhooks"], it.Name)
			if commit {
				s.hooks = append(s.hooks, &hook{Name: it.Name, Description: it.Description, URL: it.URL, Secret: *it.Secret, Events: it.Events})
			}
			continue
		}
		h := matches[0]
		merged := union(h.Events, it.Events)
		if h.Description != it.Description || h.URL != it.URL || h.Secret != *it.Secret || len(merged) != len(h.Events) {
			cs["update"]["webhooks"] = append(cs["update"]["webhooks"], it.Name)
		}
		if commit {
			h.Description, h.URL, h.Secret, h.Events = it.Description, it.URL, *it.Secret, merged
		}
	}
}

func (s *Server) importRules(items []struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Template    *string          `json:"template_name"`
	Watchers    []string         `json:"watchers"`
	Conditions  []map[string]any `json:"rules"`
}, commit bool, cs changes) {
	for _, it := range items {
		// CMP silently drops a template that does not exist and any watcher that is not a CMP user, and adds a default
		// watcher (the second user) when a rule is created. Watchers are only ever added.
		var tpl *string
		if it.Template != nil {
			for _, t := range s.templates {
				if t.Title == *it.Template {
					n := t.Title
					tpl = &n
				}
			}
		}
		var valid []string
		for _, w := range it.Watchers {
			if id := s.UserID2(w); id != "" {
				valid = append(valid, strings.ToLower(w))
			}
		}
		var matches []*rule
		for _, r := range s.rules {
			if r.Name == it.Name {
				matches = append(matches, r)
			}
		}
		if len(matches) != 1 {
			cs["create"]["routing_rules"] = append(cs["create"]["routing_rules"], it.Name)
			if commit {
				s.rules = append(s.rules, &rule{Name: it.Name, Description: it.Description, Template: tpl,
					Watchers: union([]string{strings.ToLower(s.users[1].Email)}, valid), Conditions: it.Conditions})
			}
			continue
		}
		r := matches[0]
		merged := union(r.Watchers, valid)
		same := r.Description == it.Description && reflect.DeepEqual(r.Template, tpl) && len(merged) == len(r.Watchers) &&
			reflect.DeepEqual(normConds(r.Conditions), normConds(it.Conditions))
		if !same {
			cs["update"]["routing_rules"] = append(cs["update"]["routing_rules"], it.Name)
		}
		if commit {
			r.Description, r.Template, r.Watchers, r.Conditions = it.Description, tpl, merged, it.Conditions
		}
	}
}

func normConds(in []map[string]any) []map[string]any {
	b, _ := json.Marshal(in)
	var out []map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// UserID2 is UserID without taking the lock, for use inside handlers that already hold it.
func (s *Server) UserID2(email string) string {
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			return u.ID
		}
	}
	return ""
}

// ---- state accessors for tests ----

func (s *Server) LabelGroups(labelType string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, g := range s.groups {
		if g.LabelType == labelType || g.Display == labelType {
			n++
		}
	}
	return n
}

// LabelNames returns the labels of the group with the given label_type (as the import knows it).
func (s *Server) LabelNames(labelType string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, g := range s.groups {
		if g.LabelType == labelType {
			for _, l := range g.Labels {
				out = append(out, l.Name)
			}
		}
	}
	return sortedCopy(out)
}

func (s *Server) HookCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, h := range s.hooks {
		if h.Name == name {
			n++
		}
	}
	return n
}

func (s *Server) AppCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, a := range s.apps {
		if a.Name == name {
			n++
		}
	}
	return n
}

func (s *Server) RuleCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.rules {
		if r.Name == name {
			n++
		}
	}
	return n
}

// SeedHook adds a webhook directly, bypassing the import's validation (as one created in the CMP interface would be).
func (s *Server) SeedHook(name, url, secret string, events ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = append(s.hooks, &hook{Name: name, URL: url, Secret: secret, Events: events})
}
