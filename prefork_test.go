// ⚡️ Fiber is an Express inspired web framework written in Go with ☕️
// 📄 GitHub Repository: https://github.com/gofiber/fiber
// 📌 API Documentation: https://docs.gofiber.io
// 💖 Maintained and modified for Fiber by @renewerner87
package fiber

import (
	"crypto/tls"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp/prefork"
)

func Test_App_Prefork_Child_Process(t *testing.T) {
	skipIfNoIPv6(t)

	enableTestPreforkMaster(t)

	setupIsChild(t)

	app := New()

	cfg := listenConfigDefault()
	err := app.prefork("invalid", nil, &cfg)
	require.Error(t, err)

	go func() {
		time.Sleep(1000 * time.Millisecond)
		assert.NoError(t, app.Shutdown())
	}()

	ipv6Cfg := ListenConfig{ListenerNetwork: NetworkTCP6}
	require.NoError(t, app.prefork("[::1]:", nil, &ipv6Cfg))

	// Create tls certificate
	cer, err := tls.LoadX509KeyPair("./.github/testdata/ssl.pem", "./.github/testdata/ssl.key")
	if err != nil {
		require.NoError(t, err)
	}
	config := &tls.Config{Certificates: []tls.Certificate{cer}}

	go func() {
		time.Sleep(1000 * time.Millisecond)
		assert.NoError(t, app.Shutdown())
	}()

	cfg = listenConfigDefault()
	require.NoError(t, app.prefork("127.0.0.1:", config, &cfg))

	// The child installs the Content-Length/Transfer-Encoding close hook before it serves.
	require.NotNil(t, app.Server().HeaderReceived)
}

func Test_App_Prefork_Master_Process(t *testing.T) {
	enableTestPreforkMaster(t)

	app := New()

	// With dummy commands that exit immediately, fasthttp recovers children
	// until RecoverThreshold is exceeded, then returns ErrOverRecovery.
	// Use low threshold for fast test execution.
	cfg := listenConfigDefault()
	cfg.PreforkRecoverThreshold = 1
	cfg.PreforkRecoverInterval = 10 * time.Millisecond
	cfg.PreforkShutdownGracePeriod = 100 * time.Millisecond
	err := app.prefork(":0", nil, &cfg)
	require.ErrorIs(t, err, prefork.ErrOverRecovery)

	// With invalid command, should get a start error immediately
	// (error happens during initial spawning, before recovery loop)
	dummyChildCmd.Store("invalid")

	cfg = listenConfigDefault()
	cfg.PreforkRecoverThreshold = 1
	err = app.prefork("127.0.0.1:", nil, &cfg)
	require.Error(t, err)

	dummyChildCmd.Store("go")
}

func Test_App_Prefork_Child_Process_Never_Show_Startup_Message(t *testing.T) {
	setupIsChild(t)

	rescueStdout := os.Stdout
	defer func() { os.Stdout = rescueStdout }()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w

	cfg := listenConfigDefault()
	app := New()
	app.startupProcess()
	listenData := app.prepareListenData(":0", false, &cfg, nil)
	app.startupMessage(listenData, &cfg)

	require.NoError(t, w.Close())

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Empty(t, out)
}

func Test_IsChild(t *testing.T) {
	// Without env var, should be false
	require.False(t, IsChild())

	// With env var, should be true
	setupIsChild(t)
	require.True(t, IsChild())
}

func Test_Prefork_Logger(t *testing.T) {
	t.Parallel()

	l := preforkLogger{}
	// Should not panic
	l.Printf("test %s", "message")
}

// The function a prefork child serves the listener it bound with has to stop on a GracefulContext
// done before it serves, as Listen does: canceled right before it serves, or done before the child
// ran it. It starts to watch the context itself, so this covers the real arming. The fixture and
// the table come from listen_test.go.
func Test_preforkServe_GracefulContext_DoneBeforeServe(t *testing.T) {
	t.Parallel()

	for _, start := range gracefulStarts {
		t.Run(start.name, func(t *testing.T) {
			t.Parallel()

			f := newGracefulFixture(t)
			// The child calls this right before it serves.
			f.cfg.ListenerAddrFunc = func(net.Addr) { f.cancelAndAwait(t) }
			if start.prepare != nil {
				start.prepare(f)
			}

			f.requireStopped(t, f.start(func(f *gracefulFixture) error { return f.app.preforkServe(nil, &f.cfg)(f.ln) }))
			listenerEntry.requireClosed(t, f)
		})
	}
}

func setupIsChild(t *testing.T) {
	t.Helper()

	// Set the environment variable that fasthttp's prefork.IsChild() checks
	t.Setenv("FASTHTTP_PREFORK_CHILD", "1")
}

func enableTestPreforkMaster(t *testing.T) {
	t.Helper()

	previous := testPreforkMaster
	testPreforkMaster = true
	t.Cleanup(func() {
		testPreforkMaster = previous
		dummyChildCmd.Store("go")
	})
}

func enableTestOnPrefork(t *testing.T) {
	t.Helper()

	previous := testOnPrefork
	testOnPrefork = true
	t.Cleanup(func() {
		testOnPrefork = previous
	})
}
