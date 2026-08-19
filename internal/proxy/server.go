package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/elazarl/goproxy"

	"max-proxy-mock/internal/model"
	appRuntime "max-proxy-mock/internal/runtime"
	"max-proxy-mock/internal/storage"
)

const captureLimit int64 = 2 << 20

type exchange struct {
	projectID                  string
	start                      time.Time
	method, scheme, host, path string
	reqHeaders                 map[string]string
	reqBody                    string
}

func New(store *storage.Store, state *appRuntime.State, ca *tls.Certificate) *goproxy.ProxyHttpServer {
	p := goproxy.NewProxyHttpServer()
	p.Verbose = false
	mitm := &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: goproxy.TLSConfigFromCA(ca)}
	p.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		if state.ShouldMITM(host) {
			return mitm, host
		}
		return goproxy.OkConnect, host
	})
	p.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		host := appRuntime.NormalizeHost(req.URL.Hostname())
		path := req.URL.EscapedPath()
		if path == "" {
			path = "/"
		}
		for _, m := range state.Mocks() {
			if m.Enabled && appRuntime.DomainMatches(host, m.Host) && m.Path == path && (m.Method == "" || strings.EqualFold(m.Method, req.Method)) {
				return req, mockResponse(req, m)
			}
		}
		r := state.Recording()
		if !r.Active || !appRuntime.DomainMatches(host, r.Domain) {
			return req, nil
		}
		body := previewAndRestore(&req.Body, captureLimit)
		ctx.UserData = &exchange{projectID: r.ProjectID, start: time.Now(), method: req.Method, scheme: req.URL.Scheme, host: host, path: path, reqHeaders: headers(req.Header), reqBody: body}
		return req, nil
	})
	p.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		x, ok := ctx.UserData.(*exchange)
		if !ok || resp == nil {
			return resp
		}
		body := ""
		if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
			body = previewAndRestore(&resp.Body, captureLimit)
			if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
				body = gunzipPreview(body)
			}
		}
		e := model.Endpoint{ProjectID: x.projectID, Method: x.method, Scheme: x.scheme, Host: x.host, Path: x.path, Status: resp.StatusCode, RequestHeaders: x.reqHeaders, RequestBody: x.reqBody, ResponseHeaders: headers(resp.Header), ResponseBody: body, ContentType: resp.Header.Get("Content-Type"), DurationMs: time.Since(x.start).Milliseconds(), Source: "recorded", LastSeenAt: time.Now().UTC()}
		if _, err := store.UpsertEndpoint(context.Background(), e); err == nil {
			state.Publish("endpoints")
		}
		return resp
	})
	return p
}

func mockResponse(req *http.Request, m model.MockRule) *http.Response {
	h := make(http.Header)
	for k, v := range m.Headers {
		if !hopHeader(k) && !strings.EqualFold(k, "Date") && !strings.EqualFold(k, "Server") {
			h.Set(k, v)
		}
	}
	h.Del("Content-Length")
	h.Del("Content-Encoding")
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", "application/json; charset=utf-8")
	}
	return &http.Response{StatusCode: m.Status, Status: http.StatusText(m.Status), Header: h, Body: io.NopCloser(strings.NewReader(m.Body)), Request: req}
}
func previewAndRestore(body *io.ReadCloser, limit int64) string {
	if body == nil || *body == nil {
		return ""
	}
	prefix, _ := io.ReadAll(io.LimitReader(*body, limit+1))
	*body = io.NopCloser(io.MultiReader(bytes.NewReader(prefix), *body))
	if int64(len(prefix)) > limit {
		return string(prefix[:limit]) + "\n…（内容已截断）"
	}
	return string(prefix)
}

func gunzipPreview(v string) string {
	r, err := gzip.NewReader(strings.NewReader(v))
	if err != nil {
		return v
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, captureLimit))
	if err != nil {
		return v
	}
	return string(b)
}
func headers(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		if !hopHeader(k) {
			out[textproto.CanonicalMIMEHeaderKey(k)] = strings.Join(v, "\n")
		}
	}
	return out
}
func hopHeader(k string) bool {
	switch strings.ToLower(k) {
	case "connection", "proxy-connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	}
	return false
}
