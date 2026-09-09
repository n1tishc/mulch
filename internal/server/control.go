package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/session"
)

type BranchStore interface {
	Branch(context.Context, string, int64) (event.Session, error)
}

type ManagerControl struct {
	manager *session.Manager
	store   BranchStore
}

func NewControl(manager *session.Manager, store BranchStore) *ManagerControl {
	return &ManagerControl{manager: manager, store: store}
}

func (c *ManagerControl) Start(ctx context.Context, request StartRequest) (string, error) {
	wd := request.Opts.Workdir
	if wd == "" {
		wd = "."
	}
	wd, err := filepath.Abs(wd)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(wd)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("workspace must be a directory")
	}
	wd, err = filepath.EvalSymlinks(wd)
	if err != nil {
		return "", err
	}
	request.Opts.Workdir = wd
	return c.manager.StartWith(context.WithoutCancel(ctx), request.Task, session.RunOpts{Workdir: request.Opts.Workdir})
}

func (c *ManagerControl) Running(id string) bool {
	state, ok := c.manager.Status(id)
	return ok && !state.Done
}
func (c *ManagerControl) Resume(ctx context.Context, id, text string) (string, error) {
	store, ok := c.store.(interface {
		Session(context.Context, string) (event.Session, error)
	})
	if !ok {
		return "", errors.New("session metadata unavailable")
	}
	saved, err := store.Session(ctx, id)
	if err != nil {
		return "", err
	}
	if saved.Status == event.StatusRunning || c.Running(id) {
		return "", errors.New("session is already running")
	}
	return c.manager.ResumeWith(context.WithoutCancel(ctx), id, text, session.RunOpts{Workdir: saved.Workdir})
}
func (c *ManagerControl) Steer(id, text string) error { return c.manager.Steer(id, text) }
func (c *ManagerControl) Cancel(id string) error      { return c.manager.Cancel(id) }
func (c *ManagerControl) Subscribe(ctx context.Context, id string) (<-chan event.Event, error) {
	return c.manager.Subscribe(ctx, id)
}
func (c *ManagerControl) Branch(ctx context.Context, id string, request BranchRequest) (event.Session, error) {
	child, err := c.store.Branch(ctx, id, request.At)
	if err != nil {
		return event.Session{}, err
	}
	_, err = c.manager.ResumeWith(context.WithoutCancel(ctx), child.ID, "", session.RunOpts{Workdir: child.Workdir})
	return child, err
}
