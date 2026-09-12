package poller

import "context"

// tenantIDContextKey carries the tenant a poll cycle is currently iterating
// under. It exists so code deeper in the cycle - specifically SLOAnalyzer's
// auto-created incident notification - can scope itself to that tenant without
// every intermediate signature (pollOnce, pollService, HandleTransition)
// growing a tenantID parameter.
type tenantIDContextKey struct{}

// withTenantID returns ctx carrying tenantID.
func withTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDContextKey{}, tenantID)
}

// tenantIDFromContext returns the tenant id stored by withTenantID, or "" when
// none is present (the non-tenant-iteration poll path).
func tenantIDFromContext(ctx context.Context) string {
	tenantID, _ := ctx.Value(tenantIDContextKey{}).(string)
	return tenantID
}
