package storage

import (
	"context"
	"path/filepath"
	"testing"

	"max-proxy-mock/internal/model"
)

func TestEndpointUpsertDeduplicatesByProjectAndPath(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	project, err := store.CreateProject(ctx, "Web", "api.example.com")
	if err != nil {
		t.Fatal(err)
	}
	first := model.Endpoint{ProjectID: project.ID, Method: "GET", Scheme: "https", Host: project.Domain, Path: "/api/users", Status: 200}
	if _, err = store.UpsertEndpoint(ctx, first); err != nil {
		t.Fatal(err)
	}
	first.Status = 201
	if _, err = store.UpsertEndpoint(ctx, first); err != nil {
		t.Fatal(err)
	}
	items, err := store.Endpoints(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one unique path, got %d", len(items))
	}
	if items[0].HitCount != 2 || items[0].Status != 201 {
		t.Fatalf("expected refreshed endpoint with 2 hits, got %#v", items[0])
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err = store.SetSetting(ctx, "proxy", "saved"); err != nil {
		t.Fatal(err)
	}
	value, ok, err := store.Setting(ctx, "proxy")
	if err != nil || !ok || value != "saved" {
		t.Fatalf("unexpected setting: %q %v %v", value, ok, err)
	}
	if err = store.DeleteSetting(ctx, "proxy"); err != nil {
		t.Fatal(err)
	}
	_, ok, err = store.Setting(ctx, "proxy")
	if err != nil || ok {
		t.Fatalf("expected deleted setting")
	}
}
