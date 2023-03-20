package ngrok

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Errors arising from authentication failure.
type errAuthFailed struct {
	// Whether the error was generated by the remote server, or in the sending
	// of the authentication request.
	Remote bool
	// The underlying error.
	Inner error
}

func (e errAuthFailed) Error() string {
	var msg string
	if e.Remote {
		msg = "authentication failed"
	} else {
		msg = "failed to send authentication request"
	}

	return fmt.Sprintf("%s: %v", msg, e.Inner)
}

func (e errAuthFailed) Unwrap() error {
	return e.Inner
}

func (e errAuthFailed) Is(target error) bool {
	_, ok := target.(errAuthFailed)
	return ok
}

// The error returned by [Tunnel]'s [net.Listener.Accept] method.
type errAcceptFailed struct {
	// The underlying error.
	Inner error
}

func (e errAcceptFailed) Error() string {
	return fmt.Sprintf("failed to accept connection: %v", e.Inner)
}

func (e errAcceptFailed) Unwrap() error {
	return e.Inner
}

func (e errAcceptFailed) Is(target error) bool {
	_, ok := target.(errAcceptFailed)
	return ok
}

// Errors arising from a failure to start a tunnel.
type errListen struct {
	// The underlying error.
	Inner error
}

func (e errListen) Error() string {
	return fmt.Sprintf("failed to start tunnel: %v", e.Inner)
}

func (e errListen) Unwrap() error {
	return e.Inner
}

func (e errListen) Is(target error) bool {
	_, ok := target.(errListen)
	return ok
}

// Errors arising from a failure to construct a [golang.org/x/net/proxy.Dialer] from a [url.URL].
type errProxyInit struct {
	// The provided proxy URL.
	URL *url.URL
	// The underlying error.
	Inner error
}

func (e errProxyInit) Error() string {
	return fmt.Sprintf("failed to construct proxy dialer from \"%s\": %v", e.URL.String(), e.Inner)
}

func (e errProxyInit) Unwrap() error {
	return e.Inner
}

func (e errProxyInit) Is(target error) bool {
	_, ok := target.(errProxyInit)
	return ok
}

// Error arising from a failure to dial the ngrok server.
type errSessionDial struct {
	// The address to which a connection was attempted.
	Addr string
	// The underlying error.
	Inner error
}

func (e errSessionDial) Error() string {
	return fmt.Sprintf("failed to dial ngrok server with address \"%s\": %v", e.Addr, e.Inner)
}

func (e errSessionDial) Unwrap() error {
	return e.Inner
}

func (e errSessionDial) Is(target error) bool {
	_, ok := target.(errSessionDial)
	return ok
}

type errMultiple struct {
	inners []error
}

var _ error = &errMultiple{}

func (e *errMultiple) Add(err error) {
	if err == nil {
		return
	}

	e.inners = append(e.inners, err)
}

func (e *errMultiple) Error() string {
	switch len(e.inners) {
	case 0:
		return "error: no errors recorded"
	case 1:
		return e.inners[0].Error()
	default:
		errBuilder := strings.Builder{}
		errBuilder.WriteString("multiple errors occurred:\n")
		for i := len(e.inners); i > 0; i-- {
			errBuilder.WriteString(e.inners[i-1].Error())
			errBuilder.WriteString("\n")
		}
		return errBuilder.String()
	}
}

func (e *errMultiple) Unwrap() []error {
	return e.inners
}

func (e *errMultiple) Is(err error) bool {
	for _, inner := range e.inners {
		if errors.Is(inner, err) {
			return true
		}
	}
	return false
}

func (e *errMultiple) As(err any) bool {
	for _, inner := range e.inners {
		if errors.As(inner, err) {
			return true
		}
	}
	return false
}
