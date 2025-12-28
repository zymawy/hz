// Package registry manages service registration and health checking
package registry

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/zymawy/hz/pkg/types"
)

// Registry manages registered services and their health status
type Registry struct {
	services  map[string]*types.Service
	mu        sync.RWMutex
	eventCh   chan types.RegistryEvent
	client    *http.Client
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// New creates a new service registry
func New() *Registry {
	ctx, cancel := context.WithCancel(context.Background())
	return &Registry{
		services: make(map[string]*types.Service),
		eventCh:  make(chan types.RegistryEvent, 100),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Register adds a service to the registry
func (r *Registry) Register(service *types.Service) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if service.Name == "" {
		return fmt.Errorf("service name is required")
	}

	if service.TargetURL == nil {
		return fmt.Errorf("service target URL is required")
	}

	// Store service
	r.services[service.Name] = service
	service.SetStatus(types.HealthStatusUnknown)

	// Emit event
	r.emitEvent(types.EventServiceAdded, service)

	// Start health checking if configured
	if service.Health != nil && service.Health.Path != "" {
		r.wg.Add(1)
		go r.healthCheckLoop(service)
	}

	return nil
}

// RegisterAll registers multiple services
func (r *Registry) RegisterAll(services []*types.Service) error {
	for _, svc := range services {
		if err := r.Register(svc); err != nil {
			return fmt.Errorf("failed to register service %s: %w", svc.Name, err)
		}
	}
	return nil
}

// Deregister removes a service from the registry
func (r *Registry) Deregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	service, ok := r.services[name]
	if !ok {
		return fmt.Errorf("service not found: %s", name)
	}

	delete(r.services, name)
	r.emitEvent(types.EventServiceRemoved, service)

	return nil
}

// Get returns a service by name
func (r *Registry) Get(name string) (*types.Service, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	service, ok := r.services[name]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", name)
	}

	return service, nil
}

// GetDefault returns the default service
func (r *Registry) GetDefault() *types.Service {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, svc := range r.services {
		if svc.Default {
			return svc
		}
	}

	// Return first service if no default
	for _, svc := range r.services {
		return svc
	}

	return nil
}

// List returns all registered services
func (r *Registry) List() []*types.Service {
	r.mu.RLock()
	defer r.mu.RUnlock()

	services := make([]*types.Service, 0, len(r.services))
	for _, svc := range r.services {
		services = append(services, svc)
	}
	return services
}

// Watch returns a channel that receives registry events
func (r *Registry) Watch() <-chan types.RegistryEvent {
	return r.eventCh
}

// HealthCheck performs an immediate health check on a service
func (r *Registry) HealthCheck(name string) types.HealthStatus {
	r.mu.RLock()
	service, ok := r.services[name]
	r.mu.RUnlock()

	if !ok {
		return types.HealthStatusUnknown
	}

	if service.Health == nil || service.Health.Path == "" {
		return types.HealthStatusHealthy // No health check configured, assume healthy
	}

	return r.doHealthCheck(service)
}

// healthCheckLoop runs periodic health checks for a service
func (r *Registry) healthCheckLoop(service *types.Service) {
	defer r.wg.Done()

	ticker := time.NewTicker(service.Health.Interval)
	defer ticker.Stop()

	// Initial check
	r.doHealthCheck(service)

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.doHealthCheck(service)
		}
	}
}

// doHealthCheck performs the actual health check
func (r *Registry) doHealthCheck(service *types.Service) types.HealthStatus {
	if service.Health == nil || service.Health.Path == "" {
		return types.HealthStatusHealthy
	}

	healthURL := fmt.Sprintf("%s%s", service.Target, service.Health.Path)

	ctx, cancel := context.WithTimeout(r.ctx, service.Health.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		service.SetStatus(types.HealthStatusUnhealthy)
		r.emitEvent(types.EventServiceHealthChanged, service)
		return types.HealthStatusUnhealthy
	}

	resp, err := r.client.Do(req)
	if err != nil {
		service.SetStatus(types.HealthStatusUnhealthy)
		r.emitEvent(types.EventServiceHealthChanged, service)
		return types.HealthStatusUnhealthy
	}
	defer resp.Body.Close()

	oldStatus := service.GetStatus()
	var newStatus types.HealthStatus

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		newStatus = types.HealthStatusHealthy
	} else {
		newStatus = types.HealthStatusUnhealthy
	}

	service.SetStatus(newStatus)

	// Emit event if status changed
	if oldStatus != newStatus {
		r.emitEvent(types.EventServiceHealthChanged, service)
	}

	return newStatus
}

// emitEvent sends an event to watchers
func (r *Registry) emitEvent(eventType types.RegistryEventType, service *types.Service) {
	select {
	case r.eventCh <- types.RegistryEvent{Type: eventType, Service: service}:
	default:
		// Channel full, skip event
	}
}

// Stop shuts down the registry and all health checkers
func (r *Registry) Stop() {
	r.cancel()
	r.wg.Wait()
	close(r.eventCh)
}

// Healthy returns true if all services are healthy
func (r *Registry) Healthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, svc := range r.services {
		if svc.GetStatus() == types.HealthStatusUnhealthy {
			return false
		}
	}
	return true
}

// Stats returns registry statistics
func (r *Registry) Stats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	healthy := 0
	unhealthy := 0
	unknown := 0

	for _, svc := range r.services {
		switch svc.GetStatus() {
		case types.HealthStatusHealthy:
			healthy++
		case types.HealthStatusUnhealthy:
			unhealthy++
		default:
			unknown++
		}
	}

	return map[string]interface{}{
		"total":     len(r.services),
		"healthy":   healthy,
		"unhealthy": unhealthy,
		"unknown":   unknown,
	}
}
