package proxy

import (
	"context"

	"github.com/zymawy/hz/pkg/types"
)

type contextKey string

const routeKey contextKey = "hz-route"

// withRoute stores route in request context
func withRoute(ctx context.Context, route *types.Route) context.Context {
	return context.WithValue(ctx, routeKey, route)
}

// routeFromContext retrieves route from request context
func routeFromContext(ctx context.Context) *types.Route {
	if route, ok := ctx.Value(routeKey).(*types.Route); ok {
		return route
	}
	return nil
}
