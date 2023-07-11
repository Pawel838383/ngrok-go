package config

import (
	"golang.ngrok.com/ngrok/internal/pb"
)

// BotFilter is a pair of strings slices that allow/deny traffic to an endpoint
// and 2 boolean flags that either allow empty user agents or use a
// ngrok defined deny list
type botFilter struct {
	// slice of regex strings for allowed user agents
	Allow []string
	// slice of regex strings for denied user agents
	Deny []string
	// description of what users have defined
	Description string
	// allows for empty user agents traffic to make it endpoint
	AllowEmptyUserAgent bool
	// uses internal list of commonly known bots
	UseDefaultDeny bool
}

func (b *botFilter) toProtoConfig() *pb.MiddlewareConfiguration_BotFilter {
	if b == nil {
		return nil
	}
	return &pb.MiddlewareConfiguration_BotFilter{
		Allow:               b.Allow,
		Deny:                b.Deny,
		Description:         b.Description,
		AllowEmptyUserAgent: b.AllowEmptyUserAgent,
		UseDefaultDeny:      b.UseDefaultDeny,
	}
}

// WithBotFilter configures botfilter from a set passed parameters.
func WithBotFilter(allow []string, deny []string, description string, allowEmptyUA bool, useDefaultDeny bool) HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.BotFilter = &botFilter{
			Allow:               allow,
			Deny:                deny,
			Description:         description,
			AllowEmptyUserAgent: allowEmptyUA,
			UseDefaultDeny:      useDefaultDeny,
		}
	})
}
