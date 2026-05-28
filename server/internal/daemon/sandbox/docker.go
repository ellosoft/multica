// Package sandbox provides types and helpers for running issue agent containers.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Mount describes a bind-mount passed to docker run.
type Mount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// RunSpec describes the configuration for a new container.
type RunSpec struct {
	Name       string
	Image      string
	Labels     map[string]string
	Network    string
	AddHosts   []string
	Mounts     []Mount
	CPUs       string
	Memory     string
	ExtraArgs  []string
	Entrypoint []string
}

// ExecSpec describes a command to execute inside a running container.
type ExecSpec struct {
	ContainerID string
	WorkingDir  string
	Env         map[string]string
	TTY         bool
	Argv        []string
}

// DockerClient is the interface used by higher-level sandbox code.
type DockerClient interface {
	// Run creates and starts a detached container; returns the container ID.
	Run(ctx context.Context, spec RunSpec) (string, error)
	// ExecCmd builds but does not start an exec.Cmd for running a command inside the container.
	ExecCmd(ctx context.Context, spec ExecSpec) *exec.Cmd
	// RunInside runs a command inside an already-running container and returns combined output.
	RunInside(ctx context.Context, containerID string, argv ...string) (string, error)
	// Inspect returns container IDs that match the given label filter (e.g. "multica.issue=ISSUE1").
	Inspect(ctx context.Context, label string) ([]string, error)
	// Remove force-removes a container.
	Remove(ctx context.Context, containerID string) error
}

// cliDocker implements DockerClient by shelling out to the docker CLI.
type cliDocker struct {
	// run is the seam used for testing; set by NewDockerClient to execDocker.
	run func(ctx context.Context, args ...string) (string, error)
}

// NewDockerClient returns a DockerClient backed by the host docker CLI.
func NewDockerClient() DockerClient {
	c := &cliDocker{}
	c.run = execDocker
	return c
}

// execDocker is the real implementation of the run seam.
func execDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w\nstderr: %s", args[0], err, stderr.String())
	}
	return stdout.String(), nil
}

// Run implements DockerClient.
func (c *cliDocker) Run(ctx context.Context, spec RunSpec) (string, error) {
	args := []string{"run", "-d", "--name", spec.Name}

	for k, v := range spec.Labels {
		args = append(args, "--label", k+"="+v)
	}

	if spec.Network != "" {
		args = append(args, "--network", spec.Network)
	}

	for _, h := range spec.AddHosts {
		args = append(args, "--add-host", h)
	}

	if spec.CPUs != "" {
		args = append(args, "--cpus", spec.CPUs)
	}
	if spec.Memory != "" {
		args = append(args, "--memory", spec.Memory)
	}

	for _, m := range spec.Mounts {
		v := m.HostPath + ":" + m.ContainerPath
		if m.ReadOnly {
			v += ":ro"
		}
		args = append(args, "-v", v)
	}

	args = append(args, spec.ExtraArgs...)
	args = append(args, spec.Image)
	args = append(args, spec.Entrypoint...)

	out, err := c.run(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ExecCmd implements DockerClient.
func (c *cliDocker) ExecCmd(ctx context.Context, spec ExecSpec) *exec.Cmd {
	args := []string{"exec", "-i"}
	if spec.TTY {
		args = append(args, "-t")
	}
	if spec.WorkingDir != "" {
		args = append(args, "-w", spec.WorkingDir)
	}
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, spec.ContainerID)
	args = append(args, spec.Argv...)
	return exec.CommandContext(ctx, "docker", args...)
}

// RunInside implements DockerClient.
func (c *cliDocker) RunInside(ctx context.Context, containerID string, argv ...string) (string, error) {
	args := append([]string{"exec", containerID}, argv...)
	return c.run(ctx, args...)
}

// Inspect implements DockerClient.
func (c *cliDocker) Inspect(ctx context.Context, label string) ([]string, error) {
	out, err := c.run(ctx, "ps", "-q", "--filter", "label="+label)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

// Remove implements DockerClient.
func (c *cliDocker) Remove(ctx context.Context, containerID string) error {
	_, err := c.run(ctx, "rm", "-f", containerID)
	return err
}
