package mockcmp

import (
	"encoding/json"
	"net/http"
	"strings"
)

// ---- structured content types ----

// normalise mimics the real API rewriting field definitions: an empty validation_pattern comes back as ".*", and an
// order_index is added. The provider must not report either as drift.
func normalise(defs []map[string]any) []map[string]any {
	out := make([]map[string]any, len(defs))
	for i, d := range defs {
		c := map[string]any{}
		for k, v := range d {
			c[k] = v
		}
		if v, ok := c["validation_pattern"]; ok && v == "" {
			c["validation_pattern"] = ".*"
		}
		if core, ok := c["core"].(map[string]any); ok {
			cc := map[string]any{}
			for k, v := range core {
				cc[k] = v
			}
			if _, has := cc["order_index"]; !has {
				cc["order_index"] = float64(i)
			}
			c["core"] = cc
		}
		out[i] = c
	}
	return out
}

func (s *Server) ctBase(c *contentType) map[string]any {
	var desc, thumb any
	if c.Description != "" {
		desc = c.Description
	}
	if c.Thumbnail != "" {
		thumb = c.Thumbnail
	}
	m := map[string]any{"content_type_guid": c.GUID, "name": c.Name, "description": desc, "component": c.Component, "disabled": c.Disabled,
		"thumbnail_guid": thumb, "created_by": c.CreatedBy, "updated_by": c.UpdatedBy, "created_at": c.Created, "updated_at": c.Updated, "links": map[string]any{}}
	if n := len(c.Versions); n > 0 {
		v := c.Versions[n-1]
		m["latest_version"] = map[string]any{"version_guid": v.GUID, "latest": true, "expected_locales": append([]string{}, v.Locales...),
			"created_by": c.CreatedBy, "created_at": c.Created, "field_definitions": v.Defs, "links": map[string]any{}}
	}
	return m
}

type ctDetails struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Component   bool    `json:"component"`
	Disabled    bool    `json:"disabled"`
	Thumbnail   *string `json:"thumbnail_guid"`
}

func (d ctDetails) apply(c *contentType) {
	c.Name, c.Component, c.Disabled = d.Name, d.Component, d.Disabled
	c.Description, c.Thumbnail = "", ""
	if d.Description != nil {
		c.Description = *d.Description
	}
	if d.Thumbnail != nil {
		c.Thumbnail = *d.Thumbnail
	}
}

func (s *Server) knownUser(id string) bool {
	for _, u := range s.users {
		if u.ID == id {
			return true
		}
	}
	return false
}

func (s *Server) createContentType(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Details   ctDetails        `json:"details"`
		Defs      []map[string]any `json:"field_definitions"`
		CreatedBy string           `json:"created_by"`
		Locales   []string         `json:"expected_locales"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Details.Name == "" || in.Defs == nil {
		apiErr(w, http.StatusBadRequest, "details.name and field_definitions are required")
		return
	}
	if !s.knownUser(in.CreatedBy) {
		apiErr(w, http.StatusUnprocessableEntity, "created_by is not a known user")
		return
	}
	c := &contentType{GUID: s.id(), CreatedBy: in.CreatedBy, UpdatedBy: in.CreatedBy, Created: now(), Updated: now()}
	in.Details.apply(c)
	v := &version{GUID: s.id(), Locales: in.Locales, Defs: normalise(in.Defs)}
	c.Versions = []*version{v}
	s.contentTypes[c.GUID] = c
	s.ctOrder = append(s.ctOrder, c.GUID)
	js(w, http.StatusCreated, map[string]any{"created": true, "content_type_guid": c.GUID, "content_type_version_guid": v.GUID})
}

func (s *Server) listContentTypes(w http.ResponseWriter, r *http.Request) {
	out := []any{}
	for _, id := range s.ctOrder {
		out = append(out, s.ctBase(s.contentTypes[id]))
	}
	js(w, http.StatusOK, out)
}

func (s *Server) getContentType(w http.ResponseWriter, r *http.Request) {
	c := s.contentTypes[r.PathValue("id")]
	if c == nil {
		apiErr(w, http.StatusNotFound, "content type not found")
		return
	}
	js(w, http.StatusOK, s.ctBase(c))
}

func (s *Server) updateContentType(w http.ResponseWriter, r *http.Request) {
	c := s.contentTypes[r.PathValue("id")]
	if c == nil {
		apiErr(w, http.StatusNotFound, "content type not found")
		return
	}
	var in struct {
		Details   ctDetails `json:"details"`
		UpdatedBy string    `json:"updated_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Details.Name == "" || !s.knownUser(in.UpdatedBy) {
		apiErr(w, http.StatusBadRequest, "details and a known updated_by are required")
		return
	}
	in.Details.apply(c)
	c.UpdatedBy, c.Updated = in.UpdatedBy, now()
	js(w, http.StatusOK, map[string]any{"updated": true})
}

func (s *Server) addVersion(w http.ResponseWriter, r *http.Request) {
	c := s.contentTypes[r.PathValue("id")]
	if c == nil {
		apiErr(w, http.StatusNotFound, "content type not found")
		return
	}
	var in struct {
		Defs      []map[string]any `json:"field_definitions"`
		CreatedBy string           `json:"created_by"`
		Locales   []string         `json:"expected_locales"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Defs == nil || !s.knownUser(in.CreatedBy) {
		apiErr(w, http.StatusBadRequest, "field_definitions and a known created_by are required")
		return
	}
	v := &version{GUID: s.id(), Locales: in.Locales, Defs: normalise(in.Defs)}
	c.Versions = append(c.Versions, v)
	c.Updated = now()
	js(w, http.StatusCreated, map[string]any{"created": true, "content_type_guid": c.GUID, "content_type_version_guid": v.GUID})
}

// ---- custom fields ----

func isChoiceType(t string) bool {
	return t == "dropdown" || t == "label" || t == "checkbox" || t == "radio_button"
}
func isNumberType(t string) bool {
	return t == "simple_number" || t == "percentage_number" || t == "currency_number"
}

func (s *Server) fieldJSON(f *field) map[string]any {
	m := map[string]any{"id": f.ID, "name": f.Name, "helper_text": f.Helper, "is_active": f.Active, "type": f.Type, "links": map[string]any{}}
	if f.Type == "dropdown" || f.Type == "label" {
		m["is_multi_select"] = f.Multi
	}
	if isChoiceType(f.Type) {
		cs := []map[string]any{}
		for _, c := range f.Choices {
			e := map[string]any{"id": c.ID, "name": c.Name, "is_active": c.Active}
			if f.Type == "label" {
				e["color"] = c.Color
			}
			cs = append(cs, e)
		}
		m["choices"] = cs
	}
	if isNumberType(f.Type) {
		m["has_thousand_separator"], m["decimal_places"] = f.Thousand, f.Decimals
	}
	if f.Type == "currency_number" {
		m["currency_code"] = f.Currency
	}
	return m
}

func (s *Server) listFields(w http.ResponseWriter, r *http.Request) {
	want := r.URL.Query()["ids"]
	var items []*field
	for _, f := range s.fields {
		if len(want) == 0 || contains(want, f.ID) {
			items = append(items, f)
		}
	}
	pagedList(w, r, items, func(f *field) any { return s.fieldJSON(f) })
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Server) createField(w http.ResponseWriter, r *http.Request) {
	var in map[string]json.RawMessage
	_ = json.NewDecoder(r.Body).Decode(&in)
	name, _ := str(in, "name")
	typ, _ := str(in, "type")
	if name == "" || !fieldTypes[typ] {
		apiErr(w, http.StatusBadRequest, "name and a valid type are required")
		return
	}
	f := &field{ID: s.id(), Name: name, Type: typ, Active: true, Decimals: 2, Currency: "USD"}
	f.Helper, _ = str(in, "helper_text")
	if a, ok := boolean(in, "is_active"); ok {
		f.Active = a
	}
	if m, ok := boolean(in, "is_multi_select"); ok {
		f.Multi = m
	}
	if t, ok := boolean(in, "has_thousand_separator"); ok {
		f.Thousand = t
	}
	if raw, ok := in["decimal_places"]; ok {
		var d int
		_ = json.Unmarshal(raw, &d)
		if d <= 0 {
			apiErr(w, http.StatusBadRequest, "decimal_places must be greater than 0")
			return
		}
		f.Decimals = d
	}
	if c, ok := str(in, "currency_code"); ok && c != "" {
		f.Currency = c
	}
	var choices []map[string]any
	if raw, ok := in["choices"]; ok {
		_ = json.Unmarshal(raw, &choices)
	}
	switch {
	case isChoiceType(typ) && len(choices) == 0:
		apiErr(w, http.StatusBadRequest, "choices are required for this field type")
		return
	case !isChoiceType(typ) && len(choices) > 0:
		apiErr(w, http.StatusBadRequest, "choices are not allowed for this field type")
		return
	}
	for _, c := range choices {
		n, _ := c["name"].(string)
		col, _ := c["color"].(string)
		f.Choices = append(f.Choices, &choice{ID: s.id(), Name: n, Color: col, Active: true})
	}
	s.fields = append(s.fields, f)
	js(w, http.StatusCreated, map[string]any{"id": f.ID})
}

func (s *Server) findField(id string) *field {
	for _, f := range s.fields {
		if f.ID == id {
			return f
		}
	}
	return nil
}

func (s *Server) patchField(w http.ResponseWriter, r *http.Request) {
	f := s.findField(r.PathValue("id"))
	if f == nil {
		apiErr(w, http.StatusNotFound, "field not found")
		return
	}
	in := body(r)
	if typ, _ := str(in, "type"); typ != f.Type {
		apiErr(w, http.StatusBadRequest, "type is required and cannot be changed")
		return
	}
	if v, ok := str(in, "name"); ok && v != "" {
		f.Name = v
	}
	if v, ok := str(in, "helper_text"); ok {
		f.Helper = v
	}
	if v, ok := boolean(in, "is_active"); ok {
		f.Active = v
	}
	if v, ok := boolean(in, "is_multi_select"); ok && (f.Type == "dropdown" || f.Type == "label") {
		f.Multi = v
	}
	if v, ok := boolean(in, "has_thousand_separator"); ok && isNumberType(f.Type) {
		f.Thousand = v
	}
	if raw, ok := in["decimal_places"]; ok && isNumberType(f.Type) {
		_ = json.Unmarshal(raw, &f.Decimals)
	}
	if v, ok := str(in, "currency_code"); ok && f.Type == "currency_number" {
		f.Currency = v
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addChoices(w http.ResponseWriter, r *http.Request) {
	f := s.findField(r.PathValue("id"))
	if f == nil || !isChoiceType(f.Type) {
		apiErr(w, http.StatusNotFound, "choice field not found")
		return
	}
	var in []map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	for _, c := range in {
		n, _ := c["name"].(string)
		col, _ := c["color"].(string)
		f.Choices = append(f.Choices, &choice{ID: s.id(), Name: n, Color: col, Active: true})
	}
	out := []map[string]any{}
	for _, c := range f.Choices {
		out = append(out, map[string]any{"id": c.ID, "name": c.Name, "is_active": c.Active})
	}
	js(w, http.StatusCreated, out)
}

func (f *field) choice(id string) *choice {
	for _, c := range f.Choices {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func (s *Server) patchChoice(w http.ResponseWriter, r *http.Request) {
	f := s.findField(r.PathValue("id"))
	if f == nil || f.choice(r.PathValue("cid")) == nil {
		apiErr(w, http.StatusNotFound, "choice not found")
		return
	}
	c := f.choice(r.PathValue("cid"))
	in := body(r)
	if v, ok := str(in, "name"); ok && v != "" {
		c.Name = v
	}
	if v, ok := str(in, "color"); ok {
		c.Color = v
	}
	if v, ok := boolean(in, "is_active"); ok {
		c.Active = v
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteChoice(w http.ResponseWriter, r *http.Request) {
	f := s.findField(r.PathValue("id"))
	if f == nil || f.choice(r.PathValue("cid")) == nil {
		apiErr(w, http.StatusNotFound, "choice not found")
		return
	}
	c := f.choice(r.PathValue("cid"))
	active := 0
	for _, x := range f.Choices {
		if x.Active {
			active++
		}
	}
	if c.Active && active <= 1 {
		apiErr(w, http.StatusBadRequest, "cannot delete the last active choice of a field")
		return
	}
	for i, x := range f.Choices {
		if x == c {
			f.Choices = append(f.Choices[:i], f.Choices[i+1:]...)
			break
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- state accessors for tests ----

func (s *Server) FolderCount() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.folders) }
func (s *Server) FolderNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, f := range s.folders {
		out = append(out, f.Name)
	}
	return sortedCopy(out)
}

// ContentTypeState reports whether the content type exists, whether it is disabled, and how many versions it has.
func (s *Server) ContentTypeState(guid string) (exists, disabled bool, versions int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.contentTypes[guid]
	if c == nil {
		return false, false, 0
	}
	return true, c.Disabled, len(c.Versions)
}

// FieldState reports whether the field exists and whether it is active.
func (s *Server) FieldState(id string) (exists, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.findField(id)
	if f == nil {
		return false, false
	}
	return true, f.Active
}

// Users returns the seeded users' IDs and emails.
func (s *Server) UserEmails() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for _, u := range s.users {
		out[strings.ToLower(u.Email)] = u.ID
	}
	return out
}

func (s *Server) TeamID(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.teams {
		if t.Name == name {
			return t.ID
		}
	}
	return ""
}

func (s *Server) UserID(email string) string { return s.UserEmails()[strings.ToLower(email)] }

// GetCount returns how many GET requests a path has received.
func (s *Server) GetCount(path string) int { s.mu.Lock(); defer s.mu.Unlock(); return s.Gets[path] }
