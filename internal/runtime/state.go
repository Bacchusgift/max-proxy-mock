package runtime

import (
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"max-proxy-mock/internal/model"
)

type State struct {
	mu          sync.RWMutex
	recording   model.RecordingState
	domains     []string
	mocks       atomic.Value
	listenersMu sync.Mutex
	listeners   map[chan string]struct{}
}

func New() *State {
	s := &State{listeners: map[chan string]struct{}{}}
	s.mocks.Store([]model.MockRule{})
	return s
}

func (s *State) Recording() model.RecordingState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.recording
}
func (s *State) SetRecording(v model.RecordingState) {
	v.Domain = NormalizeHost(v.Domain)
	s.mu.Lock()
	s.recording = v
	s.mu.Unlock()
	s.Publish("recording")
}
func (s *State) SetDomains(v []string) {
	s.mu.Lock()
	s.domains = append([]string(nil), v...)
	s.mu.Unlock()
}
func (s *State) SetMocks(v []model.MockRule) { s.mocks.Store(append([]model.MockRule(nil), v...)) }
func (s *State) Mocks() []model.MockRule     { return s.mocks.Load().([]model.MockRule) }

func NormalizeHost(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimPrefix(v, "https://")
	v = strings.TrimPrefix(v, "http://")
	v = strings.Split(v, "/")[0]
	if h, _, err := net.SplitHostPort(v); err == nil {
		return h
	}
	return strings.TrimSuffix(v, ".")
}
func DomainMatches(host, domain string) bool {
	host = NormalizeHost(host)
	domain = NormalizeHost(domain)
	return domain != "" && (host == domain || strings.HasSuffix(host, "."+domain))
}
func (s *State) ShouldMITM(host string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.recording.Active && DomainMatches(host, s.recording.Domain) {
		return true
	}
	for _, d := range s.domains {
		if DomainMatches(host, d) {
			return true
		}
	}
	return false
}

func (s *State) Subscribe() (<-chan string, func()) {
	ch := make(chan string, 8)
	s.listenersMu.Lock()
	s.listeners[ch] = struct{}{}
	s.listenersMu.Unlock()
	return ch, func() { s.listenersMu.Lock(); delete(s.listeners, ch); close(ch); s.listenersMu.Unlock() }
}
func (s *State) Publish(event string) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	for ch := range s.listeners {
		select {
		case ch <- event:
		default:
		}
	}
}
