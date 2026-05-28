package main

import (
	"os"
	"os/exec"
	"os/signal"
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
	env := rewriteLoopbackEnv(os.Environ(), "host.docker.internal")

	dockerArgs := buildDockerExecArgs(sandboxExecContainer, cwd, env, sandboxExecBin, args)
	child := exec.Command("docker", dockerArgs...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	if err := child.Start(); err != nil {
		os.Exit(1)
	}

	// Install signal handler: forward SIGINT/SIGTERM to child and run pkill
	// backstop so the specific agent process (identified by MULTICA_TASK_ID)
	// is cleaned up inside the container.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			// Best-effort pkill backstop inside the container.
			taskID := os.Getenv("MULTICA_TASK_ID")
			if taskID != "" {
				pkill := exec.Command("docker", "exec", sandboxExecContainer, "sh", "-c", "pkill -TERM -f "+taskID)
				_ = pkill.Run()
			}
			// Forward the signal to the docker exec child process.
			if child.Process != nil {
				_ = child.Process.Signal(sig)
			}
		}
	}()

	runErr := child.Wait()
	signal.Stop(sigCh)
	close(sigCh)

	// Also run a cleanup pkill on normal exit so orphans don't linger.
	taskID := os.Getenv("MULTICA_TASK_ID")
	if taskID != "" && runErr != nil {
		pkill := exec.Command("docker", "exec", sandboxExecContainer, "sh", "-c", "pkill -TERM -f "+taskID)
		_ = pkill.Run()
	}

	os.Exit(exitCodeOf(runErr))
	return nil // unreachable; satisfies RunE signature
}
