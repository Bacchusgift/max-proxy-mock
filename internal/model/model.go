package model

import "time"

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Domain    string    `json:"domain"`
	CreatedAt time.Time `json:"createdAt"`
}

type Endpoint struct {
	ID              string            `json:"id"`
	ProjectID       string            `json:"projectId"`
	Method          string            `json:"method"`
	Scheme          string            `json:"scheme"`
	Host            string            `json:"host"`
	Path            string            `json:"path"`
	Status          int               `json:"status"`
	RequestHeaders  map[string]string `json:"requestHeaders"`
	RequestBody     string            `json:"requestBody"`
	ResponseHeaders map[string]string `json:"responseHeaders"`
	ResponseBody    string            `json:"responseBody"`
	ContentType     string            `json:"contentType"`
	DurationMs      int64             `json:"durationMs"`
	Source          string            `json:"source"`
	Mocked          bool              `json:"mocked"`
	HitCount        int               `json:"hitCount"`
	LastSeenAt      time.Time         `json:"lastSeenAt"`
}

type MockRule struct {
	ID         string            `json:"id"`
	EndpointID string            `json:"endpointId"`
	ProjectID  string            `json:"projectId"`
	Method     string            `json:"method"`
	Host       string            `json:"host"`
	Path       string            `json:"path"`
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	Enabled    bool              `json:"enabled"`
	CreatedAt  time.Time         `json:"createdAt"`
}

type RecordingState struct {
	Active    bool   `json:"active"`
	ProjectID string `json:"projectId"`
	Domain    string `json:"domain"`
}
