package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestBotFilter(t *testing.T) {
	cases := testCases[httpOptions, proto.HTTPEndpoint]{
		{
			name: "nil",
			opts: HTTPEndpoint(),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.BotFilter
				require.Nil(t, actual)
			},
		},
		{
			name: "testAllowAndDeny",
			opts: HTTPEndpoint(WithBotFilter([]string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`}, []string{`(made_up_bot)/(\d+)\.(\d+)`}, "testing allow/deny", false, false)),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.BotFilter
				require.NotNil(t, actual)
				require.Equal(t, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`}, actual.Allow)
				require.Equal(t, []string{`(made_up_bot)/(\d+)\.(\d+)`}, actual.Deny)
				require.Equal(t, false, actual.AllowEmptyUserAgent)
				require.Equal(t, false, actual.UseDefaultDeny)

			},
		},
	}

	cases.runAll(t)
}
