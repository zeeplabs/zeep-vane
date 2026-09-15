package checks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newLoopbackV6Server starts an httptest.Server bound to [::1] (IPv6
// loopback) instead of httptest's IPv4 default (127.0.0.1) - 127.0.0.0/8 is
// one of the blocked ranges this package's own dialer enforces (spec.md
// Assumptions/MP-03), so a real local test server must use an address the
// SSRF-safe dialer does NOT block in order to exercise RunCheck's actual
// HTTP logic (as opposed to a dedicated SSRF-rejection test, which
// deliberately targets a blocked address).
func newLoopbackV6Server(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("could not listen on [::1] in this environment: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

// newLoopbackV6Listener opens a raw TCP listener on [::1] for the TCP/Ping
// check tests, for the same blocked-range reason as newLoopbackV6Server.
func newLoopbackV6Listener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("could not listen on [::1] in this environment: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func TestRunCheck_HTTP_SucceedsOn200(t *testing.T) {
	server := newLoopbackV6Server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if err := RunCheck(context.Background(), PollTypeHTTP, server.URL, 5*time.Second); err != nil {
		t.Errorf("RunCheck(http, 200) = %v, want nil", err)
	}
}

func TestRunCheck_HTTP_SucceedsOn301(t *testing.T) {
	server := newLoopbackV6Server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMovedPermanently)
	})

	if err := RunCheck(context.Background(), PollTypeHTTP, server.URL, 5*time.Second); err != nil {
		t.Errorf("RunCheck(http, 301) = %v, want nil", err)
	}
}

func TestRunCheck_HTTP_FailsOn500(t *testing.T) {
	server := newLoopbackV6Server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if err := RunCheck(context.Background(), PollTypeHTTP, server.URL, 5*time.Second); err == nil {
		t.Errorf("RunCheck(http, 500) = nil error, want an error")
	}
}

func TestRunCheck_HTTP_FailsOnConnectionRefused(t *testing.T) {
	listener := newLoopbackV6Listener(t)
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close() returned unexpected error: %v", err)
	}

	target := fmt.Sprintf("http://%s/", addr)
	if err := RunCheck(context.Background(), PollTypeHTTP, target, 2*time.Second); err == nil {
		t.Errorf("RunCheck(http, closed port) = nil error, want an error")
	}
}

func TestRunCheck_TCP_SucceedsAgainstOpenPort(t *testing.T) {
	listener := newLoopbackV6Listener(t)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	if err := RunCheck(context.Background(), PollTypeTCP, listener.Addr().String(), 5*time.Second); err != nil {
		t.Errorf("RunCheck(tcp, open port) = %v, want nil", err)
	}
}

func TestRunCheck_TCP_FailsAgainstClosedPort(t *testing.T) {
	listener := newLoopbackV6Listener(t)
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close() returned unexpected error: %v", err)
	}

	if err := RunCheck(context.Background(), PollTypeTCP, addr, 2*time.Second); err == nil {
		t.Errorf("RunCheck(tcp, closed port) = nil error, want an error")
	}
}

func TestRunCheck_Ping_SucceedsAgainstOpenPort(t *testing.T) {
	listener := newLoopbackV6Listener(t)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	if err := RunCheck(context.Background(), PollTypePing, listener.Addr().String(), 5*time.Second); err != nil {
		t.Errorf("RunCheck(ping, open port) = %v, want nil", err)
	}
}

func TestRunCheck_Ping_FailsAgainstClosedPort(t *testing.T) {
	listener := newLoopbackV6Listener(t)
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close() returned unexpected error: %v", err)
	}

	if err := RunCheck(context.Background(), PollTypePing, addr, 2*time.Second); err == nil {
		t.Errorf("RunCheck(ping, closed port) = nil error, want an error")
	}
}

// TestRunCheck_RejectsLiteralBlockedTarget_ViaControlHook asserts MP-15/T4's
// "Done when": RunCheck against a literal blocked-range target fails via
// the dialer's own Control hook - the same safety net applies at check
// time, not only at ValidateTargetSafety time. Asserted via errors.Is
// against the dialer's own sentinel error (not merely "some error"), so a
// plain connection-refused wouldn't be mistaken for the SSRF guard tripping.
func TestRunCheck_RejectsLiteralBlockedTarget_ViaControlHook(t *testing.T) {
	cases := []struct {
		name     string
		pollType string
		target   string
	}{
		{"http", PollTypeHTTP, "http://127.0.0.1:1/"},
		{"tcp", PollTypeTCP, "127.0.0.1:1"},
		{"ping", PollTypePing, "127.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := RunCheck(context.Background(), tc.pollType, tc.target, 2*time.Second)
			if err == nil {
				t.Fatalf("RunCheck(%s, blocked literal) = nil error, want an error", tc.pollType)
			}
			if !errors.Is(err, errBlockedTarget) {
				t.Errorf("RunCheck(%s, blocked literal) error = %v, want it to wrap errBlockedTarget", tc.pollType, err)
			}
		})
	}
}

// TestRunCheck_RespectsTimeout asserts T4's "Done when": a deliberately
// slow/non-responding server fails within the given timeout, not hanging
// past it.
func TestRunCheck_RespectsTimeout(t *testing.T) {
	server := newLoopbackV6Server(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	})

	const timeout = 200 * time.Millisecond
	start := time.Now()
	err := RunCheck(context.Background(), PollTypeHTTP, server.URL, timeout)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("RunCheck(http, slow server) = nil error, want a timeout error")
	}
	// Generous upper bound (10x timeout) to absorb scheduler/CI noise while
	// still proving RunCheck didn't wait anywhere near the handler's 3s
	// sleep.
	if elapsed > 10*timeout {
		t.Errorf("RunCheck took %v, want well under the 3s handler sleep (timeout was %v)", elapsed, timeout)
	}
}
