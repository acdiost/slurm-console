package linuxusers

import (
	"context"
	"errors"
	"sync"

	"github.com/pocketbase/pocketbase/core"
)

type Service struct {
	app      core.App
	registry *Registry
	provider Provider
}

func Register(app core.App, registry *Registry, provider Provider) *Service {
	if registry == nil {
		registry = &Registry{Clusters: map[string]Profile{}}
	}
	if provider == nil {
		provider = NewProcess()
	}
	s := &Service{app: app, registry: registry, provider: provider}
	app.Store().Set("linuxUsers", s)
	return s
}

var initialize sync.Mutex

func For(app core.App) *Service {
	initialize.Lock()
	defer initialize.Unlock()
	if s, ok := app.Store().Get("linuxUsers").(*Service); ok {
		return s
	}
	return Register(app, nil, nil)
}

type Result struct {
	Snapshot
	SourceLabel string `json:"sourceLabel"`
	Revision    int    `json:"revision"`
}

func (s *Service) Read(ctx context.Context, id, name string) (Result, error) {
	record, err := s.app.FindRecordById("clusters", id)
	if err != nil {
		return Result{}, fail("not_found")
	}
	if !record.GetBool("enabled") {
		return Result{}, fail("disabled")
	}
	profile, ok := s.registry.Clusters[id]
	if !ok {
		return Result{}, fail("not_configured")
	}
	revision := record.GetInt("revision")
	result, err := s.provider.Read(ctx, profile, Request{Version: 1, Name: name})
	if err != nil {
		return Result{}, err
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if err := ValidateSnapshot(result, Request{Version: 1, Name: name}); err != nil {
		return Result{}, err
	}
	current, err := s.app.FindRecordById("clusters", id)
	if err != nil {
		return Result{}, fail("changed")
	}
	if current.GetInt("revision") != revision || !current.GetBool("enabled") {
		return Result{}, fail("changed")
	}
	return Result{result, profile.Label, revision}, nil
}
func IsFailure(err error, code string) bool {
	var e *Failure
	return errors.As(err, &e) && e.Code == code
}
