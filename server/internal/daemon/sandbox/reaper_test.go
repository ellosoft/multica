package sandbox

import (
	"context"
	"testing"
	"time"
)

func TestReapIdle_RemovesStale(t *testing.T) {
	fd := newFakeDocker()
	m := NewManager(fd)
	if _, err := m.EnsureSandbox(context.Background(), "ISSUE1", cfg(), nil); err != nil {
		t.Fatal(err)
	}
	m.touch("ISSUE1", time.Now().Add(-2*time.Hour))
	removed, err := m.ReapIdle(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 reaped, got %d", removed)
	}
}

func TestReapIdle_KeepsRecent(t *testing.T) {
	fd := newFakeDocker()
	m := NewManager(fd)
	if _, err := m.EnsureSandbox(context.Background(), "ISSUE1", cfg(), nil); err != nil {
		t.Fatal(err)
	}
	m.touch("ISSUE1", time.Now())
	removed, err := m.ReapIdle(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("expected 0 reaped, got %d", removed)
	}
}
