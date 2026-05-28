package sandbox

import (
	"context"
	"testing"
)

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("arg count: got %v\nwant %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg %d: got %q want %q\nfull got: %v", i, got[i], want[i], got)
		}
	}
}

func TestCLIDocker_BuildsRunArgs(t *testing.T) {
	var gotArgs []string
	d := &cliDocker{run: func(_ context.Context, args ...string) (string, error) {
		gotArgs = args
		return "container123\n", nil
	}}
	id, err := d.Run(context.Background(), RunSpec{
		Name:     "multica-sbx-ISSUE1",
		Image:    "ghcr.io/ellosoft/sandbox:latest",
		Labels:   map[string]string{"multica.issue": "ISSUE1"},
		Network:  "bridge",
		AddHosts: []string{"host.docker.internal:host-gateway"},
		Mounts: []Mount{
			{HostPath: "/srv/ws", ContainerPath: "/srv/ws"},
			{HostPath: "/usr/local/bin", ContainerPath: "/usr/local/bin", ReadOnly: true},
		},
		CPUs: "2", Memory: "4g",
		Entrypoint: []string{"sleep", "infinity"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if id != "container123" {
		t.Fatalf("id = %q", id)
	}
	want := []string{
		"run", "-d", "--name", "multica-sbx-ISSUE1",
		"--label", "multica.issue=ISSUE1",
		"--network", "bridge",
		"--add-host", "host.docker.internal:host-gateway",
		"--cpus", "2", "--memory", "4g",
		"-v", "/srv/ws:/srv/ws",
		"-v", "/usr/local/bin:/usr/local/bin:ro",
		"ghcr.io/ellosoft/sandbox:latest", "sleep", "infinity",
	}
	assertArgs(t, gotArgs, want)
}

func TestCLIDocker_ExecArgs(t *testing.T) {
	d := &cliDocker{}
	cmd := d.ExecCmd(context.Background(), ExecSpec{
		ContainerID: "cid1",
		WorkingDir:  "/srv/ws/work",
		Env:         map[string]string{"FOO": "bar"},
		Argv:        []string{"claude", "-p"},
	})
	// docker exec -i -w /srv/ws/work -e FOO=bar cid1 claude -p
	got := cmd.Args
	if got[0] != "docker" || got[1] != "exec" || got[2] != "-i" {
		t.Fatalf("exec prefix wrong: %v", got)
	}
	joined := ""
	for _, a := range got {
		joined += a + " "
	}
	for _, want := range []string{"-w /srv/ws/work", "-e FOO=bar", "cid1 ", " claude -p"} {
		if !contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
