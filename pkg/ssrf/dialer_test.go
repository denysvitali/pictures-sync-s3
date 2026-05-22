package ssrf

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubResolver is a Resolver that returns a fixed reply on the first call
// and a different reply on subsequent calls — mimicking a DNS-rebinding
// attacker that returns a public IP for the validation lookup and an
// internal IP for the actual connection lookup.
type stubResolver struct {
	calls   int32
	replies [][]net.IPAddr
	err     error
}

func (s *stubResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	if s.err != nil {
		return nil, s.err
	}
	idx := atomic.AddInt32(&s.calls, 1) - 1
	if int(idx) >= len(s.replies) {
		// Repeat the last reply if more lookups happen than expected.
		idx = int32(len(s.replies) - 1)
	}
	return s.replies[idx], nil
}

// recordingDial captures the address the dialer actually attempts to
// connect to. It then redirects to a real httptest.Server so the rest of
// the HTTP machinery can complete without a real network call.
type recordingDial struct {
	target  string // host:port to redirect every dial to
	dialed  []string
	dialErr error
}

func (r *recordingDial) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	r.dialed = append(r.dialed, address)
	if r.dialErr != nil {
		return nil, r.dialErr
	}
	var d net.Dialer
	return d.DialContext(ctx, network, r.target)
}

func TestSafeDialer_PreventsDNSRebinding(t *testing.T) {
	// Stand up a real loopback HTTP server so we can confirm a connection
	// succeeds (or fails) end-to-end. Note: SafeDialer should refuse to
	// connect because the resolver's second answer points at a private IP.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 1.1.1.1 (public) for the validation lookup, 10.0.0.1 (private) for
	// the connection lookup. A naive implementation that re-resolves
	// after validation would connect to the private IP.
	resolver := &stubResolver{
		replies: [][]net.IPAddr{
			{{IP: net.ParseIP("1.1.1.1")}},
			{{IP: net.ParseIP("10.0.0.1")}},
		},
	}

	v := NewValidator(100, time.Minute)
	defer v.Stop()

	// Use a recording dial that would otherwise succeed against the
	// loopback test server, so if any private/rebinding IP slipped
	// through it would be visible in r.dialed.
	rec := &recordingDial{target: strings.TrimPrefix(srv.URL, "http://")}

	d := &SafeDialer{
		Validator: v,
		ClientIP:  "192.0.2.1",
		Resolver:  resolver,
		DialFunc:  rec.DialContext,
	}

	// Connect — should succeed by dialing 1.1.1.1 (the validated IP),
	// NOT re-resolve and dial 10.0.0.1. recordingDial swallows whatever
	// IP we hand it and redirects to the loopback test server.
	conn, err := d.DialContext(context.Background(), "tcp", "evil.example.com:80")
	if err != nil {
		t.Fatalf("expected dial to succeed, got %v", err)
	}
	conn.Close()

	if atomic.LoadInt32(&resolver.calls) != 1 {
		t.Errorf("expected exactly 1 DNS lookup, got %d", resolver.calls)
	}
	if len(rec.dialed) != 1 {
		t.Fatalf("expected exactly 1 dial, got %d (%v)", len(rec.dialed), rec.dialed)
	}
	// The actual TCP destination must be the validated 1.1.1.1, not
	// whatever the second DNS reply would be.
	host, _, _ := net.SplitHostPort(rec.dialed[0])
	if host != "1.1.1.1" {
		t.Errorf("dialer connected to %q; want 1.1.1.1 (validated IP). "+
			"This indicates a DNS-rebinding bypass: the dialer re-resolved "+
			"after validation.", host)
	}
}

func TestSafeDialer_RejectsPrivateResolution(t *testing.T) {
	resolver := &stubResolver{
		replies: [][]net.IPAddr{
			{{IP: net.ParseIP("192.168.1.1")}},
		},
	}
	v := NewValidator(100, time.Minute)
	defer v.Stop()

	d := &SafeDialer{
		Validator: v,
		ClientIP:  "192.0.2.1",
		Resolver:  resolver,
		DialFunc: func(_ context.Context, _, _ string) (net.Conn, error) {
			t.Fatal("DialFunc should not be invoked when validation fails")
			return nil, nil
		},
	}

	_, err := d.DialContext(context.Background(), "tcp", "totally-public.example.com:80")
	if err == nil {
		t.Fatal("expected validation error for private-IP resolution, got nil")
	}
	vErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if !strings.Contains(vErr.Reason, "private IP") {
		t.Errorf("expected private IP reason, got %q", vErr.Reason)
	}
}

func TestSafeDialer_RejectsRebindToMetadata(t *testing.T) {
	// Even if the resolver returns multiple addresses, ANY one being a
	// blocked address must fail the entire dial.
	resolver := &stubResolver{
		replies: [][]net.IPAddr{
			{
				{IP: net.ParseIP("1.1.1.1")},
				{IP: net.ParseIP("169.254.169.254")},
			},
		},
	}
	v := NewValidator(100, time.Minute)
	defer v.Stop()

	d := &SafeDialer{
		Validator: v,
		ClientIP:  "192.0.2.1",
		Resolver:  resolver,
		DialFunc: func(_ context.Context, _, _ string) (net.Conn, error) {
			t.Fatal("DialFunc should not be invoked when any resolved IP is blocked")
			return nil, nil
		},
	}

	_, err := d.DialContext(context.Background(), "tcp", "evil.example.com:80")
	if err == nil {
		t.Fatal("expected error when metadata IP is among resolved addresses")
	}
	vErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if !strings.Contains(vErr.Reason, "link-local") &&
		!strings.Contains(vErr.Reason, "metadata") {
		t.Errorf("expected metadata/link-local rejection, got %q", vErr.Reason)
	}
}

func TestSafeDialer_RedirectsAreReValidated(t *testing.T) {
	// Simulate net/http following a redirect: the client invokes
	// Transport.DialContext again for the redirect target. Verify that a
	// redirect to an internal hostname is blocked even though the
	// original request was to an allowed host.
	resolver := &stubResolver{
		replies: [][]net.IPAddr{
			// First lookup: public IP for "good.example.com"
			{{IP: net.ParseIP("1.1.1.1")}},
			// Second lookup (redirect target): metadata IP
			{{IP: net.ParseIP("169.254.169.254")}},
		},
	}

	v := NewValidator(100, time.Minute)
	defer v.Stop()

	// Real loopback server we redirect successful dials to.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First hit issues a 302 to an "internal" host.
		http.Redirect(w, r, "http://internal.example.com/redirected", http.StatusFound)
	}))
	defer upstream.Close()

	rec := &recordingDial{target: strings.TrimPrefix(upstream.URL, "http://")}

	d := &SafeDialer{
		Validator: v,
		ClientIP:  "192.0.2.1",
		Resolver:  resolver,
		DialFunc:  rec.DialContext,
	}

	client := &http.Client{
		Transport: &http.Transport{DialContext: d.DialContext},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get("http://good.example.com/")
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected redirect to internal host to be blocked, got success")
	}
	if !strings.Contains(err.Error(), "SSRF protection") {
		t.Errorf("expected SSRF protection error on redirect, got %v", err)
	}
	if atomic.LoadInt32(&resolver.calls) < 2 {
		t.Errorf("expected redirect to trigger a second DNS lookup, got %d", resolver.calls)
	}
}

func TestSafeDialer_RejectsBadNetwork(t *testing.T) {
	v := NewValidator(100, time.Minute)
	defer v.Stop()
	d := NewSafeDialer(v, "192.0.2.1")
	_, err := d.DialContext(context.Background(), "udp", "1.1.1.1:53")
	if err == nil {
		t.Fatal("expected udp network to be rejected")
	}
}

func TestSafeDialer_LiteralIPGoesThroughValidator(t *testing.T) {
	v := NewValidator(100, time.Minute)
	defer v.Stop()
	d := &SafeDialer{
		Validator: v,
		ClientIP:  "192.0.2.1",
		DialFunc: func(_ context.Context, _, _ string) (net.Conn, error) {
			t.Fatal("dial must not happen for blocked literal IP")
			return nil, nil
		},
	}
	_, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("expected validation error for literal 127.0.0.1")
	}
}
