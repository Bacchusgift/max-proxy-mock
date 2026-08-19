package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"max-proxy-mock/internal/model"
	appRuntime "max-proxy-mock/internal/runtime"
	"max-proxy-mock/internal/storage"
	"max-proxy-mock/internal/systemproxy"
)

type API struct {
	store        *storage.Store
	state        *appRuntime.State
	caPath       string
	systemProxy  *systemproxy.Manager
	proxyAddress string
}

func New(store *storage.Store, state *appRuntime.State, caPath, pacURL, proxyAddress string) *API {
	return &API{store: store, state: state, caPath: caPath, systemProxy: systemproxy.New(store, pacURL), proxyAddress: proxyAddress}
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/state", a.handleState)
	mux.HandleFunc("/api/projects", a.handleProjects)
	mux.HandleFunc("/api/projects/", a.handleProject)
	mux.HandleFunc("/api/endpoints/", a.handleEndpoint)
	mux.HandleFunc("/api/mocks", a.handleMocks)
	mux.HandleFunc("/api/mocks/", a.handleMock)
	mux.HandleFunc("/api/recording", a.handleRecording)
	mux.HandleFunc("/api/system-proxy", a.handleSystemProxy)
	mux.HandleFunc("/proxy.pac", a.handlePAC)
	mux.HandleFunc("/api/ca", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="max-proxy-ca.crt"`)
		http.ServeFile(w, r, a.caPath)
	})
	mux.HandleFunc("/events", a.handleEvents)
}

func (a *API) handlePAC(w http.ResponseWriter, r *http.Request) {
	projects, err := a.store.Projects(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var rules strings.Builder
	for _, p := range projects {
		if p.Domain == "" {
			continue
		}
		domainJSON, _ := json.Marshal(strings.ToLower(p.Domain))
		fmt.Fprintf(&rules, "  if (host === %s || dnsDomainIs(host, \".\" + %s)) return \"PROXY %s\";\n", domainJSON, domainJSON, a.proxyAddress)
	}
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	fmt.Fprintf(w, "function FindProxyForURL(url, host) {\n  host = host.toLowerCase();\n  if (isPlainHostName(host) || host === \"localhost\" || shExpMatch(host, \"127.*\")) return \"DIRECT\";\n%s  return \"DIRECT\";\n}\n", rules.String())
}

func (a *API) handleSystemProxy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonOut(w, a.systemProxy.Status(r.Context()))
	case http.MethodPost:
		if !sameOrigin(r) {
			http.Error(w, "请求来源无效", http.StatusForbidden)
			return
		}
		var in struct {
			Action string `json:"action"`
		}
		if !decode(w, r, &in) {
			return
		}
		var status systemproxy.Status
		var err error
		if in.Action == "enable" {
			status, err = a.systemProxy.Enable(r.Context())
		} else if in.Action == "restore" {
			status, err = a.systemProxy.Restore(r.Context())
		} else {
			http.Error(w, "未知操作", 400)
			return
		}
		respond(w, status, err)
	default:
		method(w)
	}
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
}

func (a *API) Bootstrap() error { return a.refresh() }
func (a *API) refresh() error {
	ctx := context.Background()
	ps, err := a.store.Projects(ctx)
	if err != nil {
		return err
	}
	domains := make([]string, 0, len(ps))
	for _, p := range ps {
		if p.Domain != "" {
			domains = append(domains, p.Domain)
		}
	}
	a.state.SetDomains(domains)
	ms, err := a.store.Mocks(ctx)
	if err != nil {
		return err
	}
	a.state.SetMocks(ms)
	return nil
}

func (a *API) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		method(w)
		return
	}
	jsonOut(w, map[string]any{"recording": a.state.Recording(), "proxyAddress": a.proxyAddress, "adminAddress": "127.0.0.1:8900"})
}

func (a *API) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ps, err := a.store.Projects(r.Context())
		respond(w, ps, err)
	case http.MethodPost:
		var in struct{ Name, Domain string }
		if !decode(w, r, &in) {
			return
		}
		p, err := a.store.CreateProject(r.Context(), in.Name, in.Domain)
		if err == nil {
			_ = a.refresh()
			a.state.Publish("projects")
		}
		respondStatus(w, p, err, http.StatusCreated)
	default:
		method(w)
	}
}

func (a *API) handleProject(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		notFound(w)
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "endpoints" {
		a.handleEndpoints(w, r, id)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var in struct{ Name, Domain string }
		if !decode(w, r, &in) {
			return
		}
		err := a.store.UpdateProject(r.Context(), id, in.Name, in.Domain)
		if err == nil {
			_ = a.refresh()
			a.state.Publish("projects")
		}
		respond(w, map[string]bool{"ok": err == nil}, err)
	case http.MethodDelete:
		err := a.store.DeleteProject(r.Context(), id)
		if err == nil {
			rec := a.state.Recording()
			if rec.ProjectID == id {
				a.state.SetRecording(model.RecordingState{})
			}
			_ = a.refresh()
			a.state.Publish("projects")
		}
		respond(w, map[string]bool{"ok": err == nil}, err)
	default:
		method(w)
	}
}

func (a *API) handleEndpoints(w http.ResponseWriter, r *http.Request, projectID string) {
	switch r.Method {
	case http.MethodGet:
		items, err := a.store.Endpoints(r.Context(), projectID)
		respond(w, items, err)
	case http.MethodPost:
		var in struct {
			Method, Path string
			Status       int
			ResponseBody string
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Method == "" {
			in.Method = "GET"
		}
		if in.Status == 0 {
			in.Status = 200
		}
		if !strings.HasPrefix(in.Path, "/") {
			in.Path = "/" + in.Path
		}
		ps, err := a.store.Projects(r.Context())
		if err != nil {
			respond(w, nil, err)
			return
		}
		domain := ""
		for _, p := range ps {
			if p.ID == projectID {
				domain = p.Domain
				break
			}
		}
		e := model.Endpoint{ProjectID: projectID, Method: strings.ToUpper(in.Method), Scheme: "https", Host: domain, Path: in.Path, Status: in.Status, ResponseHeaders: map[string]string{"Content-Type": "application/json; charset=utf-8"}, ResponseBody: in.ResponseBody, Source: "manual", LastSeenAt: time.Now().UTC()}
		saved, err := a.store.UpsertEndpoint(r.Context(), e)
		if err == nil {
			a.state.Publish("endpoints")
		}
		respondStatus(w, saved, err, http.StatusCreated)
	default:
		method(w)
	}
}

func (a *API) handleEndpoint(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/endpoints/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		notFound(w)
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "mock" && r.Method == http.MethodPost {
		m, err := a.store.CreateMock(r.Context(), id)
		if err == nil {
			_ = a.refresh()
			a.state.Publish("mocks")
		}
		respondStatus(w, m, err, http.StatusCreated)
		return
	}
	if r.Method == http.MethodDelete {
		err := a.store.DeleteEndpoint(r.Context(), id)
		if err == nil {
			_ = a.refresh()
			a.state.Publish("endpoints")
		}
		respond(w, map[string]bool{"ok": err == nil}, err)
		return
	}
	method(w)
}

func (a *API) handleMocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		method(w)
		return
	}
	items, err := a.store.Mocks(r.Context())
	respond(w, items, err)
}

func (a *API) handleMock(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/mocks/"), "/")
	if id == "" {
		notFound(w)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var in struct {
			Enabled *bool             `json:"enabled"`
			Status  *int              `json:"status"`
			Body    *string           `json:"body"`
			Headers map[string]string `json:"headers"`
		}
		if !decode(w, r, &in) {
			return
		}
		var err error
		if in.Enabled != nil {
			err = a.store.SetMockEnabled(r.Context(), id, *in.Enabled)
		}
		if err == nil {
			err = a.store.UpdateMock(r.Context(), id, in.Status, in.Body, in.Headers)
		}
		if err == nil {
			_ = a.refresh()
			a.state.Publish("mocks")
		}
		respond(w, map[string]bool{"ok": err == nil}, err)
	case http.MethodDelete:
		err := a.store.DeleteMock(r.Context(), id)
		if err == nil {
			_ = a.refresh()
			a.state.Publish("mocks")
		}
		respond(w, map[string]bool{"ok": err == nil}, err)
	default:
		method(w)
	}
}

func (a *API) handleRecording(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		method(w)
		return
	}
	var in model.RecordingState
	if !decode(w, r, &in) {
		return
	}
	if in.Active && (in.ProjectID == "" || in.Domain == "") {
		http.Error(w, "请选择项目并填写域名", http.StatusBadRequest)
		return
	}
	a.state.SetRecording(in)
	jsonOut(w, a.state.Recording())
}

func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, done := a.state.Subscribe()
	defer done()
	fmt.Fprint(w, "event: ready\ndata: connected\n\n")
	flusher.Flush()
	for {
		select {
		case event := <-ch:
			fmt.Fprintf(w, "event: change\ndata: %s\n\n", event)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(v); err != nil {
		http.Error(w, "请求内容无效", 400)
		return false
	}
	return true
}
func jsonOut(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func respond(w http.ResponseWriter, v any, err error) { respondStatus(w, v, err, 200) }
func respondStatus(w http.ResponseWriter, v any, err error, status int) {
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func method(w http.ResponseWriter)   { http.Error(w, "method not allowed", http.StatusMethodNotAllowed) }
func notFound(w http.ResponseWriter) { http.Error(w, "not found", http.StatusNotFound) }
