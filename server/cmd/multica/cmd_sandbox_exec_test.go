package main

import (
	"strings"
	"testing"
)

func TestRewriteLoopbackEnv(t *testing.T) {
	in := []string{
		"MULTICA_DAEMON_URL=http://127.0.0.1:8765",
		"MULTICA_SERVER_URL=https://multica.internal.example.com",
		"FOO=bar",
		"X=http://localhost:3000/cb",
	}
	out := rewriteLoopbackEnv(in, "host.docker.internal")
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "MULTICA_DAEMON_URL=http://host.docker.internal:8765") {
		t.Fatalf("daemon loopback not rewritten:\n%s", joined)
	}
	if !strings.Contains(joined, "MULTICA_SERVER_URL=https://multica.internal.example.com") {
		t.Fatalf("server url must be untouched:\n%s", joined)
	}
	if !strings.Contains(joined, "X=http://host.docker.internal:3000/cb") {
		t.Fatalf("localhost not rewritten:\n%s", joined)
	}
	if !strings.Contains(joined, "FOO=bar") {
		t.Fatalf("non-url env dropped:\n%s", joined)
	}
}

func TestBuildDockerExecArgs(t *testing.T) {
	args := buildDockerExecArgs("cid1", "/srv/ws/work", []string{"FOO=bar"}, "/usr/bin/claude", []string{"-p", "--verbose"})
	want := []string{"exec", "-i", "-w", "/srv/ws/work", "-e", "FOO=bar", "cid1", "/usr/bin/claude", "-p", "--verbose"}
	if len(args) != len(want) {
		t.Fatalf("len: got %v want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("arg %d: got %q want %q (full %v)", i, args[i], want[i], args)
		}
	}
}
