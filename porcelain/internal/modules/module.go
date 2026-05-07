// Package modules defines the cross-cutting contract every host control
// module satisfies and a small registry the router uses to surface them.
//
// A Module owns its own D-Bus or filesystem clients but exposes only a
// declarative summary plus an optional, page-specific data builder. This
// keeps the router free of feature-specific imports while letting modules
// evolve independently.
package modules

import (
	"context"
	"sort"
	"sync"
)

// Health categorises the runtime state of a module for badge rendering.
type Health string

const (
	// HealthOK indicates the module is fully functional.
	HealthOK Health = "ok"
	// HealthDegraded indicates partial functionality, for example when only
	// a fake backend is available on a developer workstation.
	HealthDegraded Health = "degraded"
	// HealthUnavailable indicates the module cannot operate in the current
	// environment (missing bus, missing kernel feature, etc.).
	HealthUnavailable Health = "unavailable"
)

// Status is the declarative summary the sidebar renders next to the module
// name. Detail is a human-readable explanation rendered as a tooltip.
type Status struct {
	Health Health
	Detail string
}

// Module is the contract every host control feature implements. Implementations
// must be safe for concurrent calls because the router invokes Status during
// every request.
type Module interface {
	// ID is the stable identifier used in URLs and sidebar markers.
	ID() string
	// Name is the human-readable label shown in the sidebar.
	Name() string
	// Status returns the current runtime health summary. Implementations
	// should be cheap; long-running checks must be cached internally.
	Status(ctx context.Context) Status
	// Close releases any resources (D-Bus connections, watches) the module
	// holds. Idempotent.
	Close() error
}

// Registry holds the Modules the daemon exposes. It is safe for concurrent
// use from request handlers.
type Registry struct {
	mu      sync.RWMutex
	modules map[string]Module
	order   []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{modules: map[string]Module{}}
}

// Register adds m to the registry. The first registration wins for a given
// ID so callers can layer overrides during tests.
func (r *Registry) Register(m Module) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := m.ID()
	if _, exists := r.modules[id]; exists {
		return
	}
	r.modules[id] = m
	r.order = append(r.order, id)
}

// Get returns the module registered under id and whether it was found.
func (r *Registry) Get(id string) (Module, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.modules[id]
	return m, ok
}

// All returns every registered module in registration order.
func (r *Registry) All() []Module {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Module, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.modules[id])
	}
	return out
}

// SortedIDs returns the registered module IDs in lexical order.
func (r *Registry) SortedIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := append([]string(nil), r.order...)
	sort.Strings(out)
	return out
}

// Close terminates every module and returns the first error encountered.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for _, id := range r.order {
		if err := r.modules[id].Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
