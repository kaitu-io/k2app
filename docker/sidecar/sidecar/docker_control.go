package sidecar

import (
	"context"

	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// dockerController is the minimal container-control surface the cutoff enforcer
// needs. Abstracted so unit tests inject a fake (no Docker daemon in tests).
type dockerController interface {
	// State reports whether the named container is paused and whether it exists.
	State(ctx context.Context, name string) (paused, exists bool, err error)
	Pause(ctx context.Context, name string) error
	Unpause(ctx context.Context, name string) error
}

// realDocker talks to the Docker daemon over the mounted /var/run/docker.sock,
// negotiating the API version (no hard-coded version → portable across hosts /
// cgroup v1+v2, which the daemon abstracts).
type realDocker struct{ cli *client.Client }

func newRealDocker() (*realDocker, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &realDocker{cli: cli}, nil
}

func (d *realDocker) State(ctx context.Context, name string) (bool, bool, error) {
	j, err := d.cli.ContainerInspect(ctx, name)
	if errdefs.IsNotFound(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return j.State.Paused, true, nil
}

func (d *realDocker) Pause(ctx context.Context, name string) error {
	return d.cli.ContainerPause(ctx, name)
}
func (d *realDocker) Unpause(ctx context.Context, name string) error {
	return d.cli.ContainerUnpause(ctx, name)
}
