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
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/snapcore/snapd/logger"
	"golang.org/x/time/rate"
)

// HTTPTransport implements the Transport interface using HTTPS.
type HTTPTransport struct {
	received int64
	rx       int64
	logger   logger.Logger
	client   *HTTPClient
}

func NewHTTPTransport(logger logger.Logger) *HTTPTransport {
	return &HTTPTransport{
		logger: logger,
	}
}

func (h *HTTPTransport) Serve(ctx context.Context, addr string, cert tls.Certificate, as *AssembleState) error {
	mux := http.NewServeMux()
	mux.Handle("/assemble/auth", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handleAuth(w, r, as)
	}))
	mux.Handle("/assemble/routes", h.trustedHandler(h.handleRoutes, as))
	mux.Handle("/assemble/unknown", h.trustedHandler(h.handleUnknown, as))
	mux.Handle("/assemble/devices", h.trustedHandler(h.handleDevices, as))

	server := &http.Server{
		Handler: mux,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		listener := tls.NewListener(ln, &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAnyClientCert,
		})

		_ = server.Serve(listener)
	}()

	// wait for context cancellation
	<-ctx.Done()

	// shutdown the server
	_ = server.Shutdown(context.Background())
	wg.Wait()

	return nil
}

type countingReader struct {
	r     io.Reader
	count int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.count += int64(n)
	return n, err
}

func (h *HTTPTransport) trustedHandler(next func(http.ResponseWriter, *http.Request, *PeerHandle), as *AssembleState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			w.WriteHeader(403)
			return
		}

		if len(r.TLS.PeerCertificates) != 1 {
			w.WriteHeader(403)
			return
		}

		cert := r.TLS.PeerCertificates[0].Raw
		peer, err := as.VerifyPeer(cert)
		if err != nil {
			h.logger.Debug("dropping message from untrusted peer")
			w.WriteHeader(403)
			return
		}

		next(w, r, peer)
	}
}

func (h *HTTPTransport) handleAuth(w http.ResponseWriter, r *http.Request, as *AssembleState) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}

	if r.TLS == nil {
		w.WriteHeader(400)
		return
	}

	if len(r.TLS.PeerCertificates) != 1 {
		w.WriteHeader(400)
		return
	}

	// set a max size so an untrusted peer can't send some massive JSON
	const maxAuthSize = 1024 * 4
	counter := countingReader{
		r: http.MaxBytesReader(w, r.Body, maxAuthSize),
	}

	var auth Auth
	if err := json.NewDecoder(&counter).Decode(&auth); err != nil {
		w.WriteHeader(400)
		return
	}

	cert := r.TLS.PeerCertificates[0].Raw
	if err := as.Authenticate(auth, cert); err != nil {
		w.WriteHeader(403)
		return
	}

	atomic.AddInt64(&h.received, 1)
	atomic.AddInt64(&h.rx, counter.count)
}

func (h *HTTPTransport) handleRoutes(w http.ResponseWriter, r *http.Request, peer *PeerHandle) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}

	counter := countingReader{r: r.Body}

	var routes Routes
	if err := json.NewDecoder(&counter).Decode(&routes); err != nil {
		w.WriteHeader(400)
		return
	}

	err := peer.AddRoutes(routes)
	if err != nil {
		w.WriteHeader(400)
		return
	}

	atomic.AddInt64(&h.received, 1)
	atomic.AddInt64(&h.rx, counter.count)
}

func (h *HTTPTransport) handleUnknown(w http.ResponseWriter, r *http.Request, peer *PeerHandle) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}

	counter := countingReader{r: r.Body}

	var unknown UnknownDevices
	if err := json.NewDecoder(&counter).Decode(&unknown); err != nil {
		w.WriteHeader(400)
		return
	}

	if err := peer.AddQueries(unknown); err != nil {
		w.WriteHeader(400)
		h.logger.Debug("cannot add queries for device info: " + err.Error())
		return
	}

	atomic.AddInt64(&h.received, 1)
	atomic.AddInt64(&h.rx, counter.count)
}

func (h *HTTPTransport) handleDevices(w http.ResponseWriter, r *http.Request, peer *PeerHandle) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}

	counter := countingReader{r: r.Body}

	var devices Devices
	if err := json.NewDecoder(&counter).Decode(&devices); err != nil {
		w.WriteHeader(400)
		return
	}

	if err := peer.AddDevices(devices); err != nil {
		w.WriteHeader(400)
		return
	}

	atomic.AddInt64(&h.received, 1)
	atomic.AddInt64(&h.rx, counter.count)
}

// NewClient creates a [Client] for sending outbound messages using this
// transport.
func (h *HTTPTransport) NewClient(cert tls.Certificate) Client {
	// TODO: this is hacky and bad, come back to this later
	if h.client == nil {
		h.client = NewHTTPClient(cert)
	}
	return h.client
}

func (h *HTTPTransport) Stats() (sent, received, tx, rx int64) {
	received = atomic.LoadInt64(&h.received)
	rx = atomic.LoadInt64(&h.rx)
	if h.client != nil {
		sent = atomic.LoadInt64(&h.client.sent)
		tx = atomic.LoadInt64(&h.client.tx)
	}
	return sent, received, tx, rx
}

type HTTPClient struct {
	// cert is the TLS certificate that we should use when sending messages.
	cert tls.Certificate
	// sent keeps track of how many messages we've sent.
	sent int64
	// tx keeps track of how many bytes we've sent.
	tx int64
	// limiter helps us rate limit how many outbound messages we send per
	// second.
	limiter *rate.Limiter
}

func NewHTTPClient(cert tls.Certificate) *HTTPClient {
	return &HTTPClient{
		// TODO: this can and probably should be based on byte counts, not
		// message counts
		limiter: rate.NewLimiter(rate.Limit(20), 1),
		cert:    cert,
	}
}

func (m *HTTPClient) Trusted(ctx context.Context, addr string, cert []byte, kind string, data any) error {
	return m.trusted(ctx, cert, addr, kind, data)
}

func (m *HTTPClient) trusted(ctx context.Context, cert []byte, addr string, kind string, data any) error {
	verify := func(certs [][]byte, chains [][]*x509.Certificate) error {
		if len(certs) != 1 {
			return fmt.Errorf("exactly one peer certificate expected, got %d", len(certs))
		}

		if !bytes.Equal(certs[0], cert) {
			return errors.New("refusing to communicate with unexpected peer certificate")
		}

		return nil
	}

	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify:    true,
				VerifyPeerCertificate: verify,
				Certificates:          []tls.Certificate{m.cert},
			},
		},
		Timeout: time.Minute,
	}

	if err := m.limiter.Wait(ctx); err != nil {
		return err
	}

	tx, err := send(ctx, &client, addr, kind, data)
	if err != nil {
		return err
	}

	atomic.AddInt64(&m.sent, 1)
	atomic.AddInt64(&m.tx, tx)

	return nil
}

func (m *HTTPClient) Untrusted(ctx context.Context, addr string, kind string, data any) ([]byte, error) {
	return m.untrusted(ctx, addr, kind, data)
}

func (m *HTTPClient) untrusted(ctx context.Context, addr string, kind string, data any) ([]byte, error) {
	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				Certificates:       []tls.Certificate{m.cert},
			},
		},
		Timeout: time.Minute,
	}

	if err := m.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	res, tx, err := sendWithResponse(ctx, &client, addr, kind, data)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	atomic.AddInt64(&m.sent, 1)
	atomic.AddInt64(&m.tx, tx)

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("got non-200 status code in response to auth message: %d", res.StatusCode)
	}

	// TODO: we should fail earlier on this case, somewhere in the HTTP client's
	// hooks maybe
	if res.TLS == nil {
		return nil, errors.New("peer attempting to communicate over unencrypted connection")
	}

	if len(res.TLS.PeerCertificates) != 1 {
		return nil, fmt.Errorf("exactly one peer certificate expected, got %d", len(res.TLS.PeerCertificates))
	}

	return res.TLS.PeerCertificates[0].Raw, nil
}

func send(ctx context.Context, client *http.Client, addr string, kind string, data any) (int64, error) {
	res, count, err := sendWithResponse(ctx, client, addr, kind, data)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return 0, fmt.Errorf("response to '%s' message contains status code %d", kind, res.StatusCode)
	}

	return count, nil
}

func sendWithResponse(ctx context.Context, client *http.Client, addr string, kind string, data any) (*http.Response, int64, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, 0, err
	}

	url := fmt.Sprintf("https://%s/assemble/%s", addr, kind)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}

	return res, int64(len(payload)), nil
}
