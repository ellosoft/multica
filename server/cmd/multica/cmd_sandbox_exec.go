package main

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

// rewriteLoopbackEnv returns env with loopback host:port URLs repointed at the
// docker host gateway so an in-container process can reach the host daemon.
func rewriteLoopbackEnv(env []string, gateway string) []string {
	out := make([]string, len(env))
	for i, kv := range env {
		val := kv
		val = strings.ReplaceAll(val, "//localhost:", "//"+gateway+":")
		val = strings.ReplaceAll(val, "//127.0.0.1:", "//"+gateway+":")
		out[i] = val
	}
	return out
}

// sanitizeEnvForContainer drops the host PATH (which points at the daemon host's
// dirs — e.g. /opt/homebrew/bin on a macOS dev box — that don't exist in a Linux
// container) and substitutes a deterministic container PATH. The agent and
// multica binaries are mirror-mounted read-only at identical absolute paths, so
// their dirs are placed first, followed by the standard container search path.
func sanitizeEnvForContainer(env []string, realExec, selfPath string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if key, _, ok := strings.Cut(kv, "="); ok && key == "PATH" {
			continue // replaced below
		}
		out = append(out, kv)
	}
	dirs := []string{}
	seen := map[string]bool{}
	for _, d := range []string{filepath.Dir(selfPath), filepath.Dir(realExec)} {
		if d != "" && d != "." && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	dirs = append(dirs, "/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin")
	return append(out, "PATH="+strings.Join(dirs, ":"))
}

// buildDockerExecArgs builds the `docker exec` argv (no TTY):
//
//	exec -i -w <cwd> -e K=V... <container> <realExec> <agentArgs...>
func buildDockerExecArgs(container, cwd string, env []string, realExec string, agentArgs []string) []string {
	args := []string{"exec", "-i", "-w", cwd}
	for _, kv := range env {
		args = append(args, "-e", kv)
	}
	args = append(args, container)
	args = append(args, realExec)
	args = append(args, agentArgs...)
	return args
}

// exitCodeOf extracts the exit code from a process error, or returns 1 for
// other errors and 0 for nil.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if ok := isExitError(err, &exitErr); ok {
		return exitErr.ExitCode()
	}
	return 1
}

func isExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

var sandboxExecContainer string
var sandboxExecBin string

var sandboxExecCmd = &cobra.Command{
	Use:    "__sandbox-exec",
	Short:  "Launch the agent binary inside a per-issue Docker container",
	Hidden: true,
	// DisableFlagParsing would prevent --container/--exec from working, so we
	// instead use Args: cobra.ArbitraryArgs and let cobra stop at -- naturally.
	Args: cobra.ArbitraryArgs,
	RunE: runSandboxExec,
}

func init() {
	sandboxExecCmd.Flags().StringVar(&sandboxExecContainer, "container", "", "Docker container ID or name (required)")
	sandboxExecCmd.Flags().StringVar(&sandboxExecBin, "exec", "", "Real agent binary path inside the container (required)")
	_ = sandboxExecCmd.MarkFlagRequired("container")
	_ = sandboxExecCmd.MarkFlagRequired("exec")
	rootCmd.AddCommand(sandboxExecCmd)
}

func runSandboxExec(cobraCmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	selfPath, _ := os.Executable()
	env := sanitizeEnvForContainer(
		rewriteLoopbackEnv(os.Environ(), "host.docker.internal"),
		sandboxExecBin, selfPath,
	)

	dockerArgs := buildDockerExecArgs(sandboxExecContainer, cwd, env, sandboxExecBin, args)
	child := exec.Command("docker", dockerArgs...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	if err := child.Start(); err != nil {
		os.Exit(1)
	}

	// Forward catchable termination signals to the docker exec client. The
	// authoritative cleanup of the in-container agent process is performed by
	// the daemon (sandbox.Manager.KillTaskProcess) after the task ends: when the
	// daemon cancels a task it kills this shim with SIGKILL, which cannot be
	// trapped here, so we do not rely on a signal handler for cleanup.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			if child.Process != nil {
				_ = child.Process.Signal(sig)
			}
		}
	}()

	runErr := child.Wait()
	signal.Stop(sigCh)
	close(sigCh)

	os.Exit(exitCodeOf(runErr))
	return nil // unreachable; satisfies RunE signature
}
