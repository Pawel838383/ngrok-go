package ngrok

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"

	"golang.ngrok.com/ngrok/config"
)

func skipUnless(t *testing.T, varname string, message ...any) {
	if os.Getenv(varname) == "" && os.Getenv("NGROK_TEST_ALL") == "" {
		t.Skip(message...)
	}
}

func onlineTest(t *testing.T) {
	skipUnless(t, "NGROK_TEST_ONLINE", "Skipping online test")
	// This is an annoying quirk of the free account limitations. It looks like
	// the tests run quickly enough in series that they trigger simultaneous
	// session errors for free accounts. "Something something eventual
	// consistency" most likely.
	if os.Getenv("NGROK_AUTHTOKEN") != "" {
		skipUnless(t, "NGROK_TEST_PAID", "Skipping test for paid features")
	}
}

func authenticatedTest(t *testing.T) {
	skipUnless(t, "NGROK_TEST_AUTHED", "Skipping test for authenticated features")
}

func paidTest(t *testing.T) {
	skipUnless(t, "NGROK_TEST_PAID", "Skipping test for paid features")
}

func setupSession(ctx context.Context, t *testing.T, opts ...ConnectOption) Session {
	onlineTest(t)
	opts = append(opts, WithAuthtokenFromEnv())
	sess, err := Connect(ctx, opts...)
	require.NoError(t, err, "Session Connect")
	return sess
}

func startTunnel(ctx context.Context, t *testing.T, sess Session, opts config.Tunnel) Tunnel {
	onlineTest(t)
	tun, err := sess.Listen(ctx, opts)
	require.NoError(t, err, "Listen")
	return tun
}

var helloHandler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintln(rw, "Hello, world!")
})

func serveHTTP(ctx context.Context, t *testing.T, connectOpts []ConnectOption, opts config.Tunnel, handler http.Handler) (Tunnel, <-chan error) {
	sess := setupSession(ctx, t, connectOpts...)

	tun := startTunnel(ctx, t, sess, opts)
	exited := make(chan error)

	go func() {
		exited <- http.Serve(tun, handler)
	}()
	return tun, exited
}

func TestListen(t *testing.T) {
	onlineTest(t)
	_, err := Listen(context.Background(),
		config.HTTPEndpoint(),
		WithAuthtokenFromEnv(),
	)
	require.NoError(t, err, "Session Connect")
}

func TestTunnel(t *testing.T) {
	ctx := context.Background()
	sess := setupSession(ctx, t)

	tun := startTunnel(ctx, t, sess, config.HTTPEndpoint(
		config.WithMetadata("Hello, world!"),
		config.WithForwardsTo("some application")))

	require.NotEmpty(t, tun.URL(), "Tunnel URL")
	require.Equal(t, "Hello, world!", tun.Metadata())
	require.Equal(t, "some application", tun.ForwardsTo())
}

func TestTunnelConnMetadata(t *testing.T) {
	ctx := context.Background()
	sess := setupSession(ctx, t)

	tun := startTunnel(ctx, t, sess, config.HTTPEndpoint())

	go func() {
		_, _ = http.Get(tun.URL())
	}()

	conn, err := tun.Accept()
	require.NoError(t, err)

	proxyconn, ok := conn.(Conn)
	require.True(t, ok, "conn doesn't implement proxy conn interface")

	require.Equal(t, "https", proxyconn.Proto())
	require.Equal(t, EdgeTypeUndefined, proxyconn.EdgeType())
}

func TestWithHTTPHandler(t *testing.T) {
	ctx := context.Background()
	sess := setupSession(ctx, t)

	tun := startTunnel(ctx, t, sess, config.HTTPEndpoint(
		config.WithMetadata("Hello, world!"),
		config.WithForwardsTo("some application"),
		config.WithHTTPHandler(helloHandler),
	))

	resp, err := http.Get(tun.URL())
	require.NoError(t, err, "GET tunnel url")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NotNil(t, resp.TLS, "TLS established")

	// Closing the tunnel should be fine
	require.NoError(t, tun.CloseWithContext(ctx))
}

func TestHTTPS(t *testing.T) {
	ctx := context.Background()
	tun, exited := serveHTTP(ctx, t, nil,
		config.HTTPEndpoint(),
		helloHandler,
	)

	resp, err := http.Get(tun.URL())
	require.NoError(t, err, "GET tunnel url")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NotNil(t, resp.TLS, "TLS established")

	// Closing the tunnel should be fine
	require.NoError(t, tun.CloseWithContext(ctx))

	// The http server should exit with a "closed" error
	require.Error(t, <-exited)
}

func TestHTTP(t *testing.T) {
	ctx := context.Background()
	tun, exited := serveHTTP(ctx, t, nil,
		config.HTTPEndpoint(
			config.WithScheme(config.SchemeHTTP)),
		helloHandler,
	)

	resp, err := http.Get(tun.URL())
	require.NoError(t, err, "GET tunnel url")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.Nil(t, resp.TLS, "No TLS")

	// Closing the tunnel should be fine
	require.NoError(t, tun.CloseWithContext(ctx))

	// The http server should exit with a "closed" error
	require.Error(t, <-exited)
}

func TestHTTPCompression(t *testing.T) {
	paidTest(t)
	ctx := context.Background()
	opts := config.HTTPEndpoint(config.WithCompression())
	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	req, err := http.NewRequest(http.MethodGet, tun.URL(), nil)
	require.NoError(t, err, "Create request")
	req.Header.Add("Accept-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET tunnel url")

	require.Equal(t, http.StatusOK, resp.StatusCode)

	gzReader, err := gzip.NewReader(resp.Body)
	require.NoError(t, err, "gzip reader")

	body, err := io.ReadAll(gzReader)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

// *testing.T wrapper to force `require` to Fail() then panic() rather than
// FailNow(). Permits better flow control in test functions.
type failPanic struct {
	t *testing.T
}

func (f failPanic) Errorf(format string, args ...interface{}) {
	f.t.Errorf(format, args...)
}

func (f failPanic) FailNow() {
	f.t.Fail()
	panic("test failed")
}

func TestHTTPHeaders(t *testing.T) {
	paidTest(t)
	ctx := context.Background()
	opts := config.HTTPEndpoint(
		config.WithRequestHeader("foo", "bar"),
		config.WithRemoveRequestHeader("baz"),
		config.WithResponseHeader("spam", "eggs"),
		config.WithRemoveResponseHeader("python"))

	tun, exited := serveHTTP(ctx, t, nil, opts, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		defer func() { _ = recover() }()
		t := failPanic{t}

		require.NotContains(t, r.Header, "Baz", "Baz Removed")
		require.Contains(t, r.Header, "Foo", "Foo added")
		require.Equal(t, "bar", r.Header.Get("Foo"), "Foo=bar")

		rw.Header().Add("Python", "bad header")
		_, _ = fmt.Fprintln(rw, "Hello, world!")
	}))

	req, err := http.NewRequest(http.MethodGet, tun.URL(), nil)
	require.NoError(t, err, "Create request")
	req.Header.Add("Baz", "bad header")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET tunnel url")

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NotContains(t, resp.Header, "Python", "Python removed")
	require.Contains(t, resp.Header, "Spam", "Spam added")
	require.Equal(t, "eggs", resp.Header.Get("Spam"), "Spam=eggs")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestBasicAuth(t *testing.T) {
	paidTest(t)
	ctx := context.Background()

	opts := config.HTTPEndpoint(config.WithBasicAuth("user", "foobarbaz"))

	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	req, err := http.NewRequest(http.MethodGet, tun.URL(), nil)
	require.NoError(t, err, "Create request")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET tunnel url")

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	req.SetBasicAuth("user", "foobarbaz")

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err, "GET tunnel url")

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestCircuitBreaker(t *testing.T) {
	// Don't run this one by default - it has to make ~50 requests.
	skipUnless(t, "NGROK_TEST_LONG", "Skipping long circuit breaker test")
	paidTest(t)
	ctx := context.Background()

	opts := config.HTTPEndpoint(config.WithCircuitBreaker(0.1))

	n := 0
	tun, exited := serveHTTP(ctx, t, nil, opts, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n = n + 1
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	var (
		resp *http.Response
		err  error
	)

	for i := 0; i < 50; i++ {
		resp, err = http.Get(tun.URL())
		require.NoError(t, err)
	}

	// Should see fewer than 50 requests come through.
	require.Less(t, n, 50)

	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestProxyProto(t *testing.T) {
	onlineTest(t)
	paidTest(t)
	ctx := context.Background()

	type testCase struct {
		name          string
		optsFunc      func(config.ProxyProtoVersion) config.Tunnel
		reqFunc       func(*testing.T, string)
		version       config.ProxyProtoVersion
		shouldContain string
	}

	base := []testCase{
		{
			version:       config.ProxyProtoV1,
			shouldContain: "PROXY TCP4",
		},
		{
			version:       config.ProxyProtoV2,
			shouldContain: "\x0D\x0A\x0D\x0A\x00\x0D\x0A\x51\x55\x49\x54\x0A",
		},
	}

	var cases []testCase

	for _, c := range base {
		cases = append(cases,
			testCase{
				name: fmt.Sprintf("HTTP/Version%d", c.version),
				optsFunc: func(v config.ProxyProtoVersion) config.Tunnel {
					return config.HTTPEndpoint(config.WithProxyProto(v))
				},
				reqFunc: func(t *testing.T, url string) {
					_, _ = http.Get(url)
				},
				version:       c.version,
				shouldContain: c.shouldContain,
			},
			testCase{
				name: fmt.Sprintf("TCP/Version%d", c.version),
				optsFunc: func(v config.ProxyProtoVersion) config.Tunnel {
					return config.TCPEndpoint(config.WithProxyProto(v))
				},
				reqFunc: func(t *testing.T, u string) {
					url, err := url.Parse(u)
					require.NoError(t, err)
					conn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", url.Hostname(), url.Port()))
					require.NoError(t, err)
					_, _ = fmt.Fprint(conn, "Hello, world!")
				},
				version:       c.version,
				shouldContain: c.shouldContain,
			},
		)
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			sess := setupSession(ctx, t)
			tun := startTunnel(ctx, t, sess, tcase.optsFunc(tcase.version))

			go tcase.reqFunc(t, tun.URL())

			conn, err := tun.Accept()
			require.NoError(t, err, "Accept connection")

			buf := make([]byte, 12)
			_, err = io.ReadAtLeast(conn, buf, 12)
			require.NoError(t, err, "Read connection contents")

			conn.Close()

			require.Contains(t, string(buf), tcase.shouldContain)
		})
	}
}

func TestSubdomain(t *testing.T) {
	paidTest(t)
	ctx := context.Background()

	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, rand.Uint64())

	subdomain := hex.EncodeToString(buf)

	tun, exited := serveHTTP(ctx, t, nil,
		config.HTTPEndpoint(config.WithDomain(subdomain+".ngrok.io")),
		helloHandler,
	)

	require.Contains(t, tun.URL(), subdomain)

	resp, err := http.Get(tun.URL())
	require.NoError(t, err)

	content, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "Hello, world!\n", string(content))

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestOAuth(t *testing.T) {
	paidTest(t)
	ctx := context.Background()

	opts := config.HTTPEndpoint(config.WithOAuth("google"))

	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	resp, err := http.Get(tun.URL())
	require.NoError(t, err, "GET tunnel url")

	content, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotContains(t, string(content), "Hello, world!")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestHTTPIPRestriction(t *testing.T) {
	paidTest(t)
	ctx := context.Background()

	_, cidr, err := net.ParseCIDR("0.0.0.0/0")
	require.NoError(t, err)

	opts := config.HTTPEndpoint(
		config.WithAllowCIDRString("127.0.0.1/32"),
		config.WithDenyCIDR(cidr))

	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	resp, err := http.Get(tun.URL())
	require.NoError(t, err, "GET tunnel url")

	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestTCP(t *testing.T) {
	authenticatedTest(t)
	ctx := context.Background()

	opts := config.TCPEndpoint()

	// Easier to test by pretending it's HTTP on this end.
	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	url, err := url.Parse(tun.URL())
	require.NoError(t, err)
	url.Scheme = "http"
	resp, err := http.Get(url.String())
	require.NoError(t, err, "GET tunnel url")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestTCPIPRestriction(t *testing.T) {
	paidTest(t)
	ctx := context.Background()

	_, cidr, err := net.ParseCIDR("127.0.0.1/32")
	require.NoError(t, err)

	opts := config.TCPEndpoint(
		config.WithAllowCIDR(cidr),
		config.WithDenyCIDRString("0.0.0.0/0"))

	// Easier to test by pretending it's HTTP on this end.
	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	url, err := url.Parse(tun.URL())
	require.NoError(t, err)
	url.Scheme = "http"
	resp, err := http.Get(url.String())

	// Rather than layer-7 error, we should see it at the connection level
	require.Nil(t, resp)
	require.Error(t, err, "GET Tunnel URL")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestWebsocketConversion(t *testing.T) {
	paidTest(t)
	ctx := context.Background()
	sess := setupSession(ctx, t)
	tun := startTunnel(ctx, t, sess,
		config.HTTPEndpoint(
			config.WithWebsocketTCPConversion()),
	)

	// HTTP over websockets? suuuure lol
	exited := make(chan error)
	go func() {
		exited <- http.Serve(tun, helloHandler)
	}()

	resp, err := http.Get(tun.URL())
	require.NoError(t, err)

	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "Normal http should be rejected")

	url, err := url.Parse(tun.URL())
	require.NoError(t, err)

	url.Scheme = "wss"

	client := http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return websocket.Dial(url.String(), "", tun.URL())
			},
		},
	}

	resp, err = client.Get("http://example.com")
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestConnectionCallbacks(t *testing.T) {
	// Don't run this one by default - it's timing-sensitive and prone to flakes
	skipUnless(t, "NGROK_TEST_FLAKEY", "Skipping flakey network test")

	ctx := context.Background()
	connects := 0
	disconnectErrs := 0
	disconnectNils := 0
	sess := setupSession(ctx, t,
		WithConnectHandler(func(ctx context.Context, sess Session) {
			connects += 1
		}),
		WithDisconnectHandler(func(ctx context.Context, sess Session, err error) {
			if err == nil {
				disconnectNils += 1
			} else {
				disconnectErrs += 1
			}
		}),
		WithDialer(&sketchyDialer{1 * time.Second}))

	time.Sleep(2*time.Second + 500*time.Millisecond)

	_ = sess.Close()

	time.Sleep(2 * time.Second)

	require.Equal(t, 3, connects, "should've seen some connect events")
	require.Equal(t, 3, disconnectErrs, "should've seen some errors from disconnecting")
	require.Equal(t, 1, disconnectNils, "should've seen a final nil from disconnecting")
}

type sketchyDialer struct {
	limit time.Duration
}

func (sd *sketchyDialer) Dial(network, addr string) (net.Conn, error) {
	return sd.DialContext(context.Background(), network, addr)
}

func (sd *sketchyDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := net.Dial(network, addr)
	go func() {
		time.Sleep(sd.limit)
		conn.Close()
	}()
	return conn, err
}

func TestHeartbeatCallback(t *testing.T) {
	// Don't run this one by default - it's long
	skipUnless(t, "NGROK_TEST_LONG", "Skipping long network test")

	ctx := context.Background()
	heartbeats := 0
	sess := setupSession(ctx, t,
		WithHeartbeatHandler(func(ctx context.Context, sess Session, latency time.Duration) {
			heartbeats += 1
		}),
		WithHeartbeatInterval(10*time.Second))

	time.Sleep(20*time.Second + 500*time.Millisecond)

	_ = sess.Close()

	require.Equal(t, 2, heartbeats, "should've seen some heartbeats")
}

func TestPermanentErrors(t *testing.T) {
	onlineTest(t)
	var err error
	ctx := context.Background()
	u, _ := url.Parse("notarealscheme://example.com")

	_, err = Connect(ctx, WithProxyURL(u))
	var proxyErr errProxyInit
	require.ErrorIs(t, err, proxyErr)
	require.ErrorAs(t, err, &proxyErr)

	sess, err := Connect(ctx)
	require.NoError(t, err)
	_, err = sess.Listen(ctx, config.TCPEndpoint())
	var startErr errListen
	require.ErrorIs(t, err, startErr)
	require.ErrorAs(t, err, &startErr)

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, err = Connect(timeoutCtx, WithServer("127.0.0.234:123"))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRetryableErrors(t *testing.T) {
	onlineTest(t)
	var err error
	ctx := context.Background()

	// give up on connecting after first attempt
	disconnect := WithDisconnectHandler(func(_ context.Context, sess Session, disconnectErr error) {
		sess.Close()
	})
	connect := WithConnectHandler(func(_ context.Context, sess Session) {
		sess.Close()
	})

	_, err = Connect(ctx, WithServer("127.0.0.234:123"), connect, disconnect)
	var dialErr errSessionDial
	require.ErrorIs(t, err, dialErr)
	require.ErrorAs(t, err, &dialErr)

	_, err = Connect(ctx, WithAuthtoken("lolnope"), connect, disconnect)
	var authErr errAuthFailed
	require.ErrorIs(t, err, authErr)
	require.ErrorAs(t, err, &authErr)
	require.True(t, authErr.Remote)
}

func TestNonExported(t *testing.T) {
	ctx := context.Background()

	sess := setupSession(ctx, t)

	require.NotEmpty(t, sess.(interface{ Region() string }).Region())
}

func echo(ws *websocket.Conn) {
	_, _ = io.Copy(ws, ws)
}

func TestWebsockets(t *testing.T) {
	onlineTest(t)

	ctx := context.Background()

	srv := &http.ServeMux{}
	srv.Handle("/", helloHandler)
	srv.Handle("/ws", websocket.Handler(echo))

	tun, errCh := serveHTTP(ctx, t, nil, config.HTTPEndpoint(config.WithScheme(config.SchemeHTTPS)), srv)

	tunnelURL, err := url.Parse(tun.URL())
	require.NoError(t, err)

	conn, err := websocket.Dial(fmt.Sprintf("wss://%s/ws", tunnelURL.Hostname()), "", tunnelURL.String())
	require.NoError(t, err)

	go func() {
		_, _ = fmt.Fprintln(conn, "Hello, world!")
	}()

	bufConn := bufio.NewReader(conn)
	out, err := bufConn.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "Hello, world!\n", out)

	conn.Close()
	tun.Close()

	require.Error(t, <-errCh)
}
