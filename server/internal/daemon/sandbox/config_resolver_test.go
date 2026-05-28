package sandbox

import "testing"

type fakeStore struct {
	project *Config
	global  *Config
}

func (s fakeStore) projectConfig(string) (*Config, error) { return s.project, nil }
func (s fakeStore) globalConfig() (*Config, error)        { return s.global, nil }

func TestResolve_ProjectOverridesGlobal(t *testing.T) {
	r := Resolver{Store: fakeStore{project: &Config{Enabled: true, Image: "proj:1"}, global: &Config{Enabled: true, Image: "glob:1"}}}
	got, err := r.Resolve("PROJ1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Image != "proj:1" {
		t.Fatalf("expected project image, got %q", got.Image)
	}
}

func TestResolve_FallsBackToGlobal(t *testing.T) {
	r := Resolver{Store: fakeStore{global: &Config{Enabled: true, Image: "glob:1"}}}
	got, err := r.Resolve("PROJ1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Image != "glob:1" {
		t.Fatalf("expected global fallback, got %+v", got)
	}
}

func TestResolve_NeitherDisabled(t *testing.T) {
	r := Resolver{Store: fakeStore{}}
	got, err := r.Resolve("PROJ1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatal("expected disabled when no config exists")
	}
}
