package auth

import "context"

type ctxKey int

const principalKey ctxKey = iota

// Principal is the authenticated caller derived from a validated bearer token.
type Principal struct {
	Token    string
	UserID   string
	Scopes   map[string]bool
	Entitled bool
}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey).(*Principal)
	return p, ok
}
