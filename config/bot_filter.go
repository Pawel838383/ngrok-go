package config

import (
	"golang.ngrok.com/ngrok/internal/pb"
)

// Configuration for Bot Filtering.
type botFilter struct {
	Allow               []string
	Deny                []string
	Description         string
	AllowEmptyUserAgent bool
	UseDefaultDeny      bool
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

// BotFilter configures botfilter for this edge.
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
