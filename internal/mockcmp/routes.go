package mockcmp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) routes(mux *http.ServeMux) {
	// folders and their permissions
	s.h(mux, "GET /v3/folders", s.listFolders)
	s.h(mux, "POST /v3/folders", s.createFolder)
	s.h(mux, "GET /v3/folders/{id}", s.getFolder)
	s.h(mux, "PATCH /v3/folders/{id}", s.patchFolder)
	s.h(mux, "DELETE /v3/folders/{id}", s.deleteFolder)
	s.h(mux, "GET /v3/folders/{id}/permissions", s.listPerms)
	s.h(mux, "POST /v3/folders/{id}/permissions", s.addPerms)
	s.h(mux, "PATCH /v3/folders/{id}/permissions/{aid}", s.patchPerm)
	s.h(mux, "DELETE /v3/folders/{id}/permissions/{aid}", s.deletePerm)

	// lookups
	s.h(mux, "GET /v3/users", s.findUser)
	s.h(mux, "GET /v3/users/{id}", s.getUser)
	s.h(mux, "GET /v3/userlist", func(w http.ResponseWriter, r *http.Request) {
		pagedList(w, r, s.users, func(u *user) any {
			roles := []map[string]string{}
			for _, x := range u.Roles {
				roles = append(roles, map[string]string{"name": x})
			}
			return map[string]any{"id": u.ID, "first_name": u.First, "last_name": u.Last, "full_name": u.First + " " + u.Last, "image_url": nil, "roles": roles}
		})
	})
	s.h(mux, "GET /v3/teams", func(w http.ResponseWriter, r *http.Request) {
		pagedList(w, r, s.teams, func(t *team) any { return map[string]any{"id": t.ID, "name": t.Name} })
	})
	s.h(mux, "GET /v3/teams/{id}", s.getTeam)
	s.h(mux, "GET /v3/workflows", func(w http.ResponseWriter, r *http.Request) {
		pagedList(w, r, s.workflows, func(x *workflow) any {
			return map[string]any{"id": x.ID, "name": x.Name, "description": nil, "is_flexible": false}
		})
	})
	s.h(mux, "GET /v3/workflows/{id}", s.getWorkflow)
	s.h(mux, "GET /v3/templates", s.listTemplates)
	s.h(mux, "GET /v3/templates/{id}", s.getTemplate)
	s.h(mux, "GET /v3/publishing-channels", func(w http.ResponseWriter, r *http.Request) {
		pagedList(w, r, s.channels, func(c *channel) any { return map[string]any{"id": c.ID, "name": c.Name, "disabled": false} })
	})
	s.h(mux, "GET /v3/brand-compliance/categories", func(w http.ResponseWriter, r *http.Request) {
		pagedList(w, r, s.categories, func(c *category) any {
			crit := []map[string]string{}
			for i, n := range c.Criteria {
				crit = append(crit, map[string]string{"id": fmt.Sprintf("crit-%d", i), "name": n})
			}
			return map[string]any{"id": c.ID, "name": c.Name, "criteria": crit}
		})
	})
	s.h(mux, "GET /v3/label-groups", func(w http.ResponseWriter, r *http.Request) {
		pagedList(w, r, s.groups, func(g *labelGroup) any {
			vals := []map[string]string{}
			for _, l := range g.Labels {
				vals = append(vals, map[string]string{"id": l.ID, "name": l.Name})
			}
			return map[string]any{"id": g.ID, "name": g.Display, "source_org_type": "current", "values": vals}
		})
	})

	// structured content types
	s.h(mux, "GET /v3/structured-content/content-types", s.listContentTypes)
	s.h(mux, "POST /v3/structured-content/content-types", s.createContentType)
	s.h(mux, "GET /v3/structured-content/content-types/{id}", s.getContentType)
	s.h(mux, "POST /v3/structured-content/content-types/{id}", s.updateContentType)
	s.h(mux, "POST /v3/structured-content/content-types/{id}/versions", s.addVersion)

	// custom fields
	s.h(mux, "GET /v3/fields", s.listFields)
	s.h(mux, "POST /v3/fields", s.createField)
	s.h(mux, "PATCH /v3/fields/{id}", s.patchField)
	s.h(mux, "POST /v3/fields/{id}/choices", s.addChoices)
	s.h(mux, "PATCH /v3/fields/{id}/choices/{cid}", s.patchChoice)
	s.h(mux, "DELETE /v3/fields/{id}/choices/{cid}", s.deleteChoice)

	// settings import / export
	s.h(mux, "GET /v3/settings", func(w http.ResponseWriter, r *http.Request) {
		js(w, http.StatusOK, map[string]any{"resources": s.export()})
	})
	s.h(mux, "POST /v3/settings", s.importSettings)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		apiErr(w, http.StatusNotFound, "not found: "+r.Method+" "+r.URL.Path)
	})
}

// ---- folders ----

func (s *Server) folderPath(f *folder) string {
	if f.Parent == "" {
		return "/" + f.Name
	}
	if p, ok := s.folders[f.Parent]; ok {
		return s.folderPath(p) + "/" + f.Name
	}
	return "/" + f.Name
}

func (s *Server) folderJSON(f *folder) map[string]any {
	var parent any
	if f.Parent != "" {
		parent = f.Parent
	}
	return map[string]any{"id": f.ID, "name": f.Name, "parent_folder_id": parent, "path": s.folderPath(f), "created_at": f.Created, "modified_at": f.Modified}
}

func (s *Server) listFolders(w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get("parent_folder_id")
	var items []*folder
	for _, id := range s.folderOrder {
		if f, ok := s.folders[id]; ok && f.Parent == parent {
			items = append(items, f)
		}
	}
	pagedList(w, r, items, func(f *folder) any { return s.folderJSON(f) })
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	b := body(r)
	name, _ := str(b, "name")
	parent, _ := str(b, "parent_folder_id")
	if name == "" {
		apiErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if parent != "" && s.folders[parent] == nil {
		apiErr(w, http.StatusNotFound, "parent folder not found")
		return
	}
	f := &folder{ID: s.id(), Name: name, Parent: parent, Created: now(), Modified: now()}
	s.folders[f.ID] = f
	s.folderOrder = append(s.folderOrder, f.ID)
	js(w, http.StatusCreated, s.folderJSON(f))
}

func (s *Server) getFolder(w http.ResponseWriter, r *http.Request) {
	f := s.folders[r.PathValue("id")]
	if f == nil {
		apiErr(w, http.StatusNotFound, "folder not found")
		return
	}
	js(w, http.StatusOK, s.folderJSON(f))
}

func (s *Server) patchFolder(w http.ResponseWriter, r *http.Request) {
	f := s.folders[r.PathValue("id")]
	if f == nil {
		apiErr(w, http.StatusNotFound, "folder not found")
		return
	}
	b := body(r)
	if name, ok := str(b, "name"); ok && name != "" {
		f.Name = name
	}
	if _, present := b["parent_folder_id"]; present { // null moves the folder to the root
		parent, _ := str(b, "parent_folder_id")
		if parent != "" && s.folders[parent] == nil {
			apiErr(w, http.StatusNotFound, "parent folder not found")
			return
		}
		f.Parent = parent
	}
	f.Modified = now()
	js(w, http.StatusOK, s.folderJSON(f))
}

func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.folders[id] == nil {
		apiErr(w, http.StatusNotFound, "folder not found")
		return
	}
	var drop func(string)
	drop = func(id string) {
		for cid, c := range s.folders {
			if c.Parent == id {
				drop(cid)
			}
		}
		delete(s.folders, id)
		delete(s.perms, id)
	}
	drop(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) accessor(id string) (kind, name string, ok bool) {
	for _, u := range s.users {
		if u.ID == id {
			return "user", u.First + " " + u.Last, true
		}
	}
	for _, t := range s.teams {
		if t.ID == id {
			return "team", t.Name, true
		}
	}
	return "", "", false
}

func (s *Server) listPerms(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.folders[id] == nil {
		apiErr(w, http.StatusNotFound, "folder not found")
		return
	}
	pagedList(w, r, s.perms[id], func(p *permission) any {
		return map[string]any{"id": p.ID, "type": p.Type, "name": p.Name, "is_owner": p.Owner, "access_type": p.Access, "links": map[string]any{"accessor": nil}}
	})
}

func (s *Server) addPerms(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.folders[id] == nil {
		apiErr(w, http.StatusNotFound, "folder not found")
		return
	}
	var in struct {
		Type        string                       `json:"type"`
		Permissions []map[string]json.RawMessage `json:"permissions"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Type != "user" && in.Type != "team" || len(in.Permissions) == 0 {
		apiErr(w, http.StatusBadRequest, "type and permissions are required")
		return
	}
	for _, p := range in.Permissions {
		if _, has := p["is_owner"]; has && in.Type == "team" {
			js(w, http.StatusBadRequest, map[string]any{"code": 400, "status": "Bad Request",
				"errors": map[string]any{"json": map[string]any{"_schema": []string{"unsupported field 'is_owner' for 'team'"}}}})
			return
		}
		aid, _ := str(p, "id")
		kind, name, ok := s.accessor(aid)
		if !ok || kind != in.Type {
			apiErr(w, http.StatusNotFound, "accessor not found")
			return
		}
		access, _ := str(p, "access_type")
		owner, _ := boolean(p, "is_owner")
		s.setPerm(id, &permission{ID: aid, Type: kind, Name: name, Access: access, Owner: owner})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setPerm(folderID string, p *permission) {
	for i, e := range s.perms[folderID] {
		if e.ID == p.ID {
			s.perms[folderID][i] = p
			return
		}
	}
	s.perms[folderID] = append(s.perms[folderID], p)
}

func (s *Server) patchPerm(w http.ResponseWriter, r *http.Request) {
	for _, p := range s.perms[r.PathValue("id")] {
		if p.ID == r.PathValue("aid") {
			b := body(r)
			if a, ok := str(b, "access_type"); ok {
				p.Access = a
			}
			if o, ok := boolean(b, "is_owner"); ok && o {
				p.Owner = true
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	apiErr(w, http.StatusNotFound, "permission not found")
}

func (s *Server) deletePerm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for i, p := range s.perms[id] {
		if p.ID == r.PathValue("aid") {
			s.perms[id] = append(s.perms[id][:i], s.perms[id][i+1:]...)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	apiErr(w, http.StatusNotFound, "permission not found")
}

// ---- lookups ----

func (s *Server) findUser(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			w.Header().Set("Location", fmt.Sprintf("https://%s/v3/users/%s", r.Host, u.ID))
			w.WriteHeader(http.StatusFound)
			return
		}
	}
	apiErr(w, http.StatusNotFound, "user not found")
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	for _, u := range s.users {
		if u.ID == r.PathValue("id") {
			js(w, http.StatusOK, map[string]any{"id": u.ID, "first_name": u.First, "last_name": u.Last, "full_name": u.First + " " + u.Last, "email": u.Email, "image_url": nil})
			return
		}
	}
	apiErr(w, http.StatusNotFound, "user not found")
}

func (s *Server) getTeam(w http.ResponseWriter, r *http.Request) {
	for _, t := range s.teams {
		if t.ID == r.PathValue("id") {
			users := []map[string]string{}
			for _, uid := range t.Users {
				for _, u := range s.users {
					if u.ID == uid {
						users = append(users, map[string]string{"id": u.ID, "name": u.First + " " + u.Last, "email": u.Email})
					}
				}
			}
			js(w, http.StatusOK, map[string]any{"id": t.ID, "name": t.Name, "users": users})
			return
		}
	}
	apiErr(w, http.StatusNotFound, "team not found")
}

func (s *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	for _, x := range s.workflows {
		if x.ID == r.PathValue("id") {
			steps := []map[string]any{}
			for i, n := range x.Steps {
				steps = append(steps, map[string]any{"id": fmt.Sprintf("step-%d", i), "name": n, "sub_steps": []any{}})
			}
			js(w, http.StatusOK, map[string]any{"id": x.ID, "name": x.Name, "description": nil, "is_active": true, "is_flexible": false,
				"is_smart_duration_enabled": true, "is_asset_approval_enabled": false, "is_resource_management_enabled": false, "steps": steps, "fields": []any{}})
			return
		}
	}
	apiErr(w, http.StatusNotFound, "workflow not found")
}

func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	search := strings.ToLower(r.URL.Query().Get("search"))
	var items []*template
	for _, t := range s.templates {
		if search == "" || strings.Contains(strings.ToLower(t.Title), search) {
			items = append(items, t)
		}
	}
	pagedList(w, r, items, func(t *template) any { return map[string]any{"id": t.ID, "title": t.Title, "is_active": t.Active} })
}

func (s *Server) getTemplate(w http.ResponseWriter, r *http.Request) {
	for _, t := range s.templates {
		if t.ID == r.PathValue("id") {
			js(w, http.StatusOK, map[string]any{"id": t.ID, "title": t.Title, "description": t.Description, "is_active": t.Active, "applicable_to": t.ApplicableTo, "form_fields": []any{}})
			return
		}
	}
	apiErr(w, http.StatusNotFound, "template not found")
}
