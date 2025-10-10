// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package assemblestate

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"golang.org/x/time/rate"
)

// Transport provides an abstraction for defining how incoming and outgoing
// messages are handled in an assembly session.
type Transport interface {
	// Serve starts a server that handles incoming requests and routes them to
	// the provided [AssembleState].
	Serve(ctx context.Context, ln net.Listener, cert tls.Certificate, pa PeerAuthenticator) error

	// NewClient creates a client for sending outbound messages compatible with
	// this [Transport].
	NewClient(cert tls.Certificate) Client

	// Stats returns the cumulative statistics for messages sent and received by
	// this [Transport].
	Stats() TransportStats
}

// Client is used to communicate with our peers.
type Client interface {
	// Trusted sends a message to a trusted peer. Implementations must verify
	// that the peer is using the given certificate.
	Trusted(ctx context.Context, addr string, cert []byte, kind string, message any) error

	// Untrusted sends a message to a peer that we do not yet trust. The
	// certificate that the peer used to communicate is returned.
	Untrusted(ctx context.Context, addr string, kind string, message any) (cert []byte, err error)
}

// PeerAuthenticator enables a [Transport] to authenticate peers.
type PeerAuthenticator interface {
	// AuthenticateAndCommit checks that the given [Auth] message is valid and
	// proves knowledge of the shared secret.
	AuthenticateAndCommit(auth Auth, cert []byte) error
	// VerifyPeer returns a [VerifiedPeer] if the given certificate has
	// previously been authenticated via a call to
	// [PeerAuthenticator.AuthenticateAndCommit]. The [VerifiedPeer] allows that
	// peer to change the state of the assemble session.
	VerifyPeer(cert []byte) (VerifiedPeer, error)
}

// VerifiedPeer represents a peer that has been authenticated and is allowed to
// commit changes to the state of the cluster.
type VerifiedPeer interface {
	// CommitDeviceQueries adds the given devices to the queue of queries for this
	// peer.
	CommitDeviceQueries(unknown UnknownDevices) error
	// CommitDevices records the given device identities.
	CommitDevices(devices Devices) error
	// CommitRoutes records the given routes.
	CommitRoutes(routes Routes) error
}

// TransportStats carries the statistics for a [Transport].
type TransportStats struct {
	// Sent is the number of messages sent.
	Sent int64
	// Tx is the number of bytes sent.
	Tx int64
	// Received is the number of messages received.
	Received int64
	// Rx is the number of bytes received.
	Rx int64
}

func (ts *TransportStats) recv(size int64) {
	atomic.AddInt64(&ts.Received, 1)
	atomic.AddInt64(&ts.Rx, size)
}

func (ts *TransportStats) sent(size int64) {
	atomic.AddInt64(&ts.Sent, 1)
	atomic.AddInt64(&ts.Tx, size)
}

func (ts *TransportStats) clone() TransportStats {
	return TransportStats{
		Received: atomic.LoadInt64(&ts.Received),
		Rx:       atomic.LoadInt64(&ts.Rx),
		Sent:     atomic.LoadInt64(&ts.Sent),
		Tx:       atomic.LoadInt64(&ts.Tx),
	}
}

// HTTPSTransport implements the Transport interface using HTTPS with mutual TLS
// authentication. It manages both server and client operations for secure
// cluster assembly communication. The transport tracks message statistics using
// atomic counters for monitoring purposes.
type HTTPSTransport struct {
	stats TransportStats
}

// NewHTTPSTransport creates a new [HTTPSTransport] instance with default
// settings. The returned transport can be used to both serve incoming assembly
// requests and create clients for outbound communication.
func NewHTTPSTransport() *HTTPSTransport {
	return &HTTPSTransport{}
}

// Serve implements the Transport interface. It starts an HTTPS server with the
// given [net.Listener] using the provided TLS certificate. The server routes
// incoming assembly protocol messages to the appropriate handlers.
//
// The server handles the following endpoints:
//   - /assemble/auth: Authentication messages from untrusted peers
//   - /assemble/routes: Route information from trusted peers
//   - /assemble/unknown: Device queries from trusted peers
//   - /assemble/devices: Device identity responses from trusted peers
//
// The server runs until the context is cancelled.
func (t *HTTPSTransport) Serve(ctx context.Context, ln net.Listener, cert tls.Certificate, pa PeerAuthenticator) error {
	mux := http.NewServeMux()
	mux.Handle("/assemble/auth", http.HandlerFunc(t.statsHandler(func(w http.ResponseWriter, r *http.Request) {
		t.handleAuth(w, r, pa)
	})))
	mux.Handle("/assemble/routes", t.statsHandler(t.trustedHandler(t.handleRoutes, pa)))
	mux.Handle("/assemble/unknown", t.statsHandler(t.trustedHandler(t.handleUnknown, pa)))
	mux.Handle("/assemble/devices", t.statsHandler(t.trustedHandler(t.handleDevices, pa)))

	server := &http.Server{
		Handler: mux,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
		ErrorLog: log.New(io.Discard, "", 0),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		listener := tls.NewListener(ln, &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAnyClientCert,

			// we support TLS 1.2 as the minimum version. this aligns with the
			// configuration set in httputil.NewHTTPClient.
			MinVersion: tls.VersionTLS12,
		})

		// serve always returns a non-nil error, nothing to handle here
		_ = server.Serve(listener)
	}()

	// wait for context cancellation
	<-ctx.Done()

	// shutdown the server
	_ = server.Shutdown(ctx)
	wg.Wait()

	return nil
}

type countingReader struct {
	r     io.ReadCloser
	count int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.count += int64(n)
	return n, err
}

func (c *countingReader) Close() error {
	return c.r.Close()
}

func (t *HTTPSTransport) statsHandler(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		counter := countingReader{r: r.Body}
		r.Body = &counter

		next(w, r)

		t.stats.recv(counter.count)
	}
}

func (t *HTTPSTransport) handleAuth(w http.ResponseWriter, r *http.Request, pa PeerAuthenticator) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}

	if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
		w.WriteHeader(403)
		return
	}

	// set a max size so an untrusted peer can't send some massive JSON
	const maxAuthSize = 1024 * 4
	body := http.MaxBytesReader(w, r.Body, maxAuthSize)

	var auth Auth
	if err := json.NewDecoder(body).Decode(&auth); err != nil {
		w.WriteHeader(400)
		return
	}

	cert := r.TLS.PeerCertificates[0].Raw
	if err := pa.AuthenticateAndCommit(auth, cert); err != nil {
		w.WriteHeader(403)
		logger.Debugf("cannot authenticate peer: %v", err)
		return
	}
}

func (t *HTTPSTransport) trustedHandler(next func(http.ResponseWriter, *http.Request, VerifiedPeer), pa PeerAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}

		if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
			w.WriteHeader(403)
			return
		}

		cert := r.TLS.PeerCertificates[0].Raw
		peer, err := pa.VerifyPeer(cert)
		if err != nil {
			logger.Debug("dropping message from untrusted peer")
			w.WriteHeader(403)
			return
		}

		next(w, r, peer)
	}
}

func (t *HTTPSTransport) handleRoutes(w http.ResponseWriter, r *http.Request, peer VerifiedPeer) {
	var routes Routes
	if err := json.NewDecoder(r.Body).Decode(&routes); err != nil {
		w.WriteHeader(400)
		return
	}

	err := peer.CommitRoutes(routes)
	if err != nil {
		w.WriteHeader(400)
		logger.Debugf("cannot commit routes: %v", err)
		return
	}
}

func (t *HTTPSTransport) handleUnknown(w http.ResponseWriter, r *http.Request, peer VerifiedPeer) {
	var unknown UnknownDevices
	if err := json.NewDecoder(r.Body).Decode(&unknown); err != nil {
		w.WriteHeader(400)
		return
	}

	if err := peer.CommitDeviceQueries(unknown); err != nil {
		w.WriteHeader(400)
		logger.Debugf("cannot commit queries for device info: %v", err)
		return
	}
}

func (t *HTTPSTransport) handleDevices(w http.ResponseWriter, r *http.Request, peer VerifiedPeer) {
	var devices Devices
	if err := json.NewDecoder(r.Body).Decode(&devices); err != nil {
		w.WriteHeader(400)
		return
	}

	if err := peer.CommitDevices(devices); err != nil {
		w.WriteHeader(400)
		logger.Debugf("cannot commit device info: %v", err)
		return
	}
}

// NewClient creates a Client compatible with this [HTTPSTransport] for sending
// outbound assembly protocol messages. The client will use the provided TLS
// certificate for mutual authentication.
func (t *HTTPSTransport) NewClient(cert tls.Certificate) Client {
	return NewHTTPSClient(cert, &t.stats, rate.NewLimiter(rate.Limit(1_000_000), 5_000_000))
}

// Stats returns the cumulative statistics for messages sent and received by
// this [Transport].
func (t *HTTPSTransport) Stats() TransportStats {
	return t.stats.clone()
}

// HTTPSClient implements the Client interface for sending outbound assembly
// protocol messages over HTTPS with mutual TLS authentication. It provides rate
// limiting capabilities to prevent overwhelming peers and tracks message
// statistics for monitoring purposes.
type HTTPSClient struct {
	// cert is the TLS certificate that we should use when sending messages.
	cert tls.Certificate
	// stats is provided by the parent [Transport] to keep track of messages
	// sent.
	stats *TransportStats
	// limiter helps us rate limit our output of bytes/second.
	limiter *rate.Limiter
}

// NewHTTPSClient creates a new [HTTPSClient] with custom rate limiting. Pass
// nil for the limiter to disable rate limiting entirely.
func NewHTTPSClient(cert tls.Certificate, stats *TransportStats, limiter *rate.Limiter) *HTTPSClient {
	return &HTTPSClient{
		limiter: limiter,
		cert:    cert,
		stats:   stats,
	}
}

// Trusted sends a message to a trusted peer, verifying that the peer presents
// the expected TLS certificate during the connection.
func (c *HTTPSClient) Trusted(ctx context.Context, addr string, cert []byte, kind string, data any) error {
	verify := func(certs [][]byte, chains [][]*x509.Certificate) error {
		if len(certs) != 1 {
			return fmt.Errorf("exactly one peer certificate expected, got %d", len(certs))
		}

		if !bytes.Equal(certs[0], cert) {
			return errors.New("refusing to communicate with unexpected peer certificate")
		}

		return nil
	}

	client := httputil.NewHTTPClient(&httputil.ClientOptions{
		Timeout: time.Minute,
		TLSConfig: &tls.Config{
			InsecureSkipVerify:    true,
			VerifyPeerCertificate: verify,
			Certificates:          []tls.Certificate{c.cert},
		},
	})
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("redirects are not expected")
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	tx, err := send(ctx, client, addr, kind, payload, c.limiter)
	if err != nil {
		return err
	}

	if c.stats != nil {
		c.stats.sent(tx)
	}

	return nil
}

// Untrusted sends a message to an untrusted peer and returns the TLS
// certificate that the peer presented. This is used for initial authentication
// exchanges where the peer's identity hasn't been verified yet.
func (c *HTTPSClient) Untrusted(ctx context.Context, addr string, kind string, data any) ([]byte, error) {
	client := httputil.NewHTTPClient(&httputil.ClientOptions{
		Timeout: time.Minute,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
			Certificates:       []tls.Certificate{c.cert},
		},
	})
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("redirects are not expected")
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	res, tx, err := sendWithResponse(ctx, client, addr, kind, payload, c.limiter)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if c.stats != nil {
		c.stats.sent(tx)
	}

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("got non-200 status code in response to auth message: %d", res.StatusCode)
	}

	// this should not be possible since we specify https in the URL and disable
	// redirects
	if res.TLS == nil {
		return nil, errors.New("peer attempting to communicate over unencrypted connection")
	}

	if len(res.TLS.PeerCertificates) != 1 {
		return nil, fmt.Errorf("exactly one peer certificate expected, got %d", len(res.TLS.PeerCertificates))
	}

	return res.TLS.PeerCertificates[0].Raw, nil
}

func send(
	ctx context.Context,
	client *http.Client,
	addr string,
	kind string,
	payload []byte,
	rl *rate.Limiter,
) (int64, error) {
	res, count, err := sendWithResponse(ctx, client, addr, kind, payload, rl)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return 0, fmt.Errorf("response to '%s' message contains status code %d", kind, res.StatusCode)
	}

	return count, nil
}

func sendWithResponse(
	ctx context.Context,
	client *http.Client,
	addr string,
	kind string,
	payload []byte,
	rl *rate.Limiter,
) (*http.Response, int64, error) {
	url := fmt.Sprintf("https://%s/assemble/%s", addr, kind)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}

	// rate limit based on the number of bytes/second that we're sending. this
	// will block until we have enough bytes in our budget to send the payload.
	if rl != nil {
		if err := rl.WaitN(ctx, len(payload)); err != nil {
			return nil, 0, err
		}
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}

	return res, int64(len(payload)), nil
}
