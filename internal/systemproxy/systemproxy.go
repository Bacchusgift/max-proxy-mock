package systemproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"max-proxy-mock/internal/storage"
)

const backupKey = "system_proxy_backup"

type Status struct {
	Supported   bool   `json:"supported"`
	OS          string `json:"os"`
	Service     string `json:"service"`
	Enabled     bool   `json:"enabled"`
	URL         string `json:"url"`
	ExpectedURL string `json:"expectedUrl"`
	Managed     bool   `json:"managed"`
	Message     string `json:"message,omitempty"`
}

type backup struct {
	Service string `json:"service"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

type Manager struct {
	store  *storage.Store
	pacURL string
}

func New(store *storage.Store, pacURL string) *Manager { return &Manager{store: store, pacURL: pacURL} }

func (m *Manager) Status(ctx context.Context) Status {
	s := Status{OS: runtime.GOOS, ExpectedURL: m.pacURL}
	if runtime.GOOS != "darwin" {
		s.Message = "当前系统请使用 PAC 地址手动配置"
		return s
	}
	service, err := primaryService(ctx)
	if err != nil {
		s.Message = err.Error()
		return s
	}
	enabled, url, err := autoProxy(ctx, service)
	if err != nil {
		s.Message = err.Error()
		return s
	}
	s.Supported = true
	s.Service = service
	s.Enabled = enabled
	s.URL = url
	s.Managed = enabled && samePAC(url, m.pacURL)
	return s
}

func (m *Manager) Enable(ctx context.Context) (Status, error) {
	s := m.Status(ctx)
	if !s.Supported {
		return s, errors.New(s.Message)
	}
	if s.Managed {
		return s, nil
	}
	b := backup{Service: s.Service, URL: s.URL, Enabled: s.Enabled}
	raw, _ := json.Marshal(b)
	if err := m.store.SetSetting(ctx, backupKey, string(raw)); err != nil {
		return s, err
	}
	if err := run(ctx, "networksetup", "-setautoproxyurl", s.Service, m.pacURL); err != nil {
		return s, err
	}
	if err := run(ctx, "networksetup", "-setautoproxystate", s.Service, "on"); err != nil {
		return s, err
	}
	result := m.Status(ctx)
	if !result.Managed {
		return result, errors.New("系统没有接受 PAC 设置")
	}
	return result, nil
}

func (m *Manager) Restore(ctx context.Context) (Status, error) {
	s := m.Status(ctx)
	if !s.Supported {
		return s, errors.New(s.Message)
	}
	raw, ok, err := m.store.Setting(ctx, backupKey)
	if err != nil {
		return s, err
	}
	if ok {
		var b backup
		if json.Unmarshal([]byte(raw), &b) == nil && b.Service != "" {
			if b.URL != "" {
				if err := run(ctx, "networksetup", "-setautoproxyurl", b.Service, b.URL); err != nil {
					return s, err
				}
			}
			state := "off"
			if b.Enabled {
				state = "on"
			}
			if err := run(ctx, "networksetup", "-setautoproxystate", b.Service, state); err != nil {
				return s, err
			}
		}
	} else {
		if err := run(ctx, "networksetup", "-setautoproxystate", s.Service, "off"); err != nil {
			return s, err
		}
	}
	_ = m.store.DeleteSetting(ctx, backupKey)
	return m.Status(ctx), nil
}

func primaryService(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "route", "-n", "get", "default").Output()
	if err != nil {
		return "", fmt.Errorf("无法识别当前网络接口")
	}
	device := ""
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "interface:" {
			device = f[1]
			break
		}
	}
	if device == "" {
		return "", errors.New("没有找到当前网络接口")
	}
	out, err = exec.CommandContext(ctx, "networksetup", "-listnetworkserviceorder").Output()
	if err != nil {
		return "", errors.New("无法读取 macOS 网络服务")
	}
	service := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "(") && !strings.HasPrefix(line, "(Hardware") {
			if i := strings.Index(line, ")"); i >= 0 {
				service = strings.TrimSpace(line[i+1:])
			}
		}
		marker := "Device: "
		if i := strings.Index(line, marker); i >= 0 {
			found := strings.TrimSuffix(strings.TrimSpace(line[i+len(marker):]), ")")
			if found == device && service != "" {
				return strings.TrimPrefix(service, "*"), nil
			}
		}
	}
	return "", fmt.Errorf("没有找到接口 %s 对应的网络服务", device)
}

func autoProxy(ctx context.Context, service string) (bool, string, error) {
	out, err := exec.CommandContext(ctx, "networksetup", "-getautoproxyurl", service).CombinedOutput()
	if err != nil {
		return false, "", fmt.Errorf("读取系统 PAC 设置失败: %s", strings.TrimSpace(string(out)))
	}
	enabled := false
	url := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "URL:") {
			url = strings.TrimSpace(strings.TrimPrefix(line, "URL:"))
		}
		if strings.HasPrefix(line, "Enabled:") {
			enabled = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(line, "Enabled:")), "Yes")
		}
	}
	return enabled, url, nil
}
func run(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("系统代理设置失败: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
func samePAC(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}
