package handler

import "testing"

func TestResolveSandbox_Precedence(t *testing.T) {
	proj := &sandboxRow{enabled: true, image: "proj:1"}
	glob := &sandboxRow{enabled: true, image: "glob:1"}
	if got := resolveSandbox(proj, glob); got.Image != "proj:1" {
		t.Fatalf("project should win, got %q", got.Image)
	}
	if got := resolveSandbox(nil, glob); !got.Enabled || got.Image != "glob:1" {
		t.Fatalf("global fallback failed: %+v", got)
	}
	if got := resolveSandbox(nil, nil); got.Enabled {
		t.Fatalf("expected disabled when neither set")
	}
}
