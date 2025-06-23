package cluster

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	as "github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
)

// AssembleOpts carries all of the options the caller can provide to [Assemble].
type AssembleOpts struct {
	ListenIP    net.IP
	ListenPort  int
	Observer    Observer
	Logger      *slog.Logger
	Secret      string
	RDTOverride string
}

// Observer lets a caller watch the assembly process. This will gain some
// methods to handle more status updates so that we can report back to the
// caller/user.
type Observer interface {
	Errors(error)
}

// Discoverer returns a set of addresses that should be considered for assembly.
type Discoverer = func(context.Context) ([]string, error)

// Assemble starts an assembly session. Without a known number of expected
// devices assembly will run until the given [context.Context] is cancelled.
//
// All of the routes that are found and associated with identified devices are
// returned. We'll probably have to return some more information here in the
// future.
//
// Eventually, this function will use the accepted [state.State] to resume a
// stopped assemble session.
func Assemble(st *state.State, ctx context.Context, discover Discoverer, opts AssembleOpts) (as.Routes, error) {
	// TODO: pick how we're going to generate RDTs
	rdt := as.RDT(opts.RDTOverride)
	if rdt == "" {
		return as.Routes{}, errors.New("rdt must be provided")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	logger = logger.With("local-rdt", rdt)

	observer := opts.Observer
	if observer == nil {
		observer = &loggingObserver{logger: logger}
	}

	cert, key, err := createCertAndKey(opts.ListenIP)
	if err != nil {
		return as.Routes{}, err
	}

	config := as.ClusterConfig{
		Secret:  opts.Secret,
		RDT:     rdt,
		IP:      opts.ListenIP,
		Port:    opts.ListenPort,
		TLSCert: cert,
		TLSKey:  key,
	}

	st.Lock()
	st.Set("cluster-config", config)
	st.Unlock()

	cs, err := as.NewClusterState(st, func(self as.RDT) (as.RouteSelector, error) {
		return as.NewPrioritySelector(self, nil), nil
	})
	if err != nil {
		return as.Routes{}, err
	}

	return assemble(ctx, cs, discover, logger, observer)
}

func assemble(
	ctx context.Context,
	cs *as.ClusterState,
	discover Discoverer,
	logger *slog.Logger,
	observer Observer,
) (as.Routes, error) {
	server, err := newAssembleServer(cs, logger, observer)
	if err != nil {
		return as.Routes{}, err
	}

	messenger := HTTPMessenger{
		logger:   logger,
		cert:     cs.Cert(),
		observer: observer,
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		periodic(ctx, time.Second*5, time.Second*1, func(ctx context.Context) {
			discoveries, err := discover(ctx)
			if err != nil {
				observer.Errors(err)
				return
			}

			// filter out our address, maybe this should be done somewhere else?
			addrs := make([]string, 0, len(discoveries))
			for _, addr := range discoveries {
				if addr == cs.Address() {
					continue
				}

				addrs = append(addrs, addr)
			}

			// if this returns an error, someone is doing something
			// wrong/malicious
			if err := cs.PublishAuth(ctx, addrs, &messenger); err != nil {
				observer.Errors(err)
				return
			}
		})
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		periodic(ctx, time.Second*5, time.Second, func(ctx context.Context) {
			cs.PublishRoutes(ctx, &messenger)
		})
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		periodic(ctx, time.Second*1, time.Second/2, func(ctx context.Context) {
			cs.PublishDevices(ctx, &messenger)
		})
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		periodic(ctx, time.Second*1, time.Second/2, func(ctx context.Context) {
			cs.PublishDeviceQueries(ctx, &messenger)
		})
	}()

	// cancelling the context will cause the goroutines to terminate. until
	// then, we wait here.
	wg.Wait()

	server.stop()

	logger.Info("assemble stopped",
		"sent-bytes", messenger.sent.Load(),
		"received-bytes", server.received.Load(),
	)

	return cs.Routes(), nil
}

func periodic(
	ctx context.Context,
	interval time.Duration,
	jitter time.Duration,
	work func(ctx context.Context),
) {
	delay := func() time.Duration {
		if jitter <= 0 {
			return interval
		}

		// +- jitter from the given interval
		j := time.Duration(randutil.Int63n(int64(jitter)*2)) - jitter
		return interval + j
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay()):
		}

		// even if the timer won the select, we should still check if the
		// context has been cancelled
		if ctx.Err() != nil {
			return
		}

		work(ctx)
	}
}

type assembleServer struct {
	cs       *as.ClusterState
	server   *http.Server
	received atomic.Int64

	logger   *slog.Logger
	observer Observer

	wg sync.WaitGroup
}

func newAssembleServer(cs *as.ClusterState, logger *slog.Logger, observer Observer) (*assembleServer, error) {
	svr := assembleServer{
		cs:       cs,
		logger:   logger,
		observer: observer,
	}

	mux := http.NewServeMux()
	mux.Handle("/assemble/auth", http.HandlerFunc(svr.handleAuth))
	mux.Handle("/assemble/routes", svr.trustedHandler(svr.handleRoutes))
	mux.Handle("/assemble/unknown", svr.trustedHandler(svr.handleUnknown))
	mux.Handle("/assemble/devices", svr.trustedHandler(svr.handleDevices))

	svr.server = &http.Server{
		Handler: mux,
	}

	// this will be closed by svr.stop
	ln, err := net.Listen("tcp", cs.Address())
	if err != nil {
		return nil, err
	}

	svr.wg.Add(1)
	go func() {
		defer svr.wg.Done()

		listener := tls.NewListener(ln, &tls.Config{
			Certificates: []tls.Certificate{cs.Cert()},
			ClientAuth:   tls.RequireAnyClientCert,
		})
		_ = svr.server.Serve(listener)
	}()

	return &svr, nil
}

func (svr *assembleServer) stop() {
	_ = svr.server.Shutdown(context.Background())
	svr.wg.Wait()
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

func (svr *assembleServer) trustedHandler(next func(http.ResponseWriter, *http.Request, *as.PeerHandle)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			w.WriteHeader(403)
			return
		}

		if len(r.TLS.PeerCertificates) != 1 {
			w.WriteHeader(403)
			return
		}

		h, err := svr.cs.Trusted(r.TLS.PeerCertificates[0].Raw)
		if err != nil {
			svr.logger.Debug("dropping message from untrusted peer")
			w.WriteHeader(403)
			return
		}

		next(w, r, h)
	}
}

func (a *assembleServer) handleAuth(w http.ResponseWriter, r *http.Request) {
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

	var auth as.Auth
	if err := json.NewDecoder(&counter).Decode(&auth); err != nil {
		w.WriteHeader(400)
		return
	}

	cert := r.TLS.PeerCertificates[0].Raw
	if err := a.cs.Authenticate(auth, cert); err != nil {
		w.WriteHeader(403)
		return
	}

	a.logger.Debug("got valid auth message", "peer-rdt", auth.RDT)
	a.received.Add(counter.count)
}

func (svr *assembleServer) handleRoutes(w http.ResponseWriter, r *http.Request, h *as.PeerHandle) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}

	counter := countingReader{r: r.Body}

	var routes as.Routes
	if err := json.NewDecoder(&counter).Decode(&routes); err != nil {
		w.WriteHeader(400)
		return
	}

	added, total, err := h.AddRoutes(routes)
	if err != nil {
		w.WriteHeader(400)
		return
	}

	received := len(routes.Routes) / 3
	svr.logger.Debug("got routes update",
		"peer-rdt", h.RDT(),
		"added-routes", added,
		"wasted-routes", received-added,
		"total-routes", total,
	)
	svr.received.Add(counter.count)
}

func (svr *assembleServer) handleUnknown(w http.ResponseWriter, r *http.Request, h *as.PeerHandle) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}

	counter := countingReader{r: r.Body}

	var unknown as.UnknownDevices
	if err := json.NewDecoder(&counter).Decode(&unknown); err != nil {
		w.WriteHeader(400)
		return
	}

	if err := h.AddQueries(unknown); err != nil {
		w.WriteHeader(400)
		svr.logger.Error("cannot add queries for device info", "error", err.Error())
		return
	}

	svr.logger.Debug("got device queries", "peer-rdt", h.RDT())
	svr.received.Add(counter.count)
}

func (svr *assembleServer) handleDevices(w http.ResponseWriter, r *http.Request, h *as.PeerHandle) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}

	counter := countingReader{r: r.Body}

	var devices as.Devices
	if err := json.NewDecoder(&counter).Decode(&devices); err != nil {
		w.WriteHeader(400)
		return
	}

	if err := h.AddDevices(devices); err != nil {
		w.WriteHeader(400)
		return
	}

	svr.logger.Debug("got unknown device information", "peer-rdt", h.RDT(), "devices-count", len(devices.Devices))
	svr.received.Add(counter.count)
}

type HTTPMessenger struct {
	// cert is the TLS certificate that we should use when sending messages.
	cert tls.Certificate
	// sent keeps track of how many bytes we've sent
	sent atomic.Int64

	logger   *slog.Logger
	observer Observer
}

func (m *HTTPMessenger) Trusted(ctx context.Context, rdt as.RDT, addr string, cert []byte, kind string, data any) error {
	if err := m.trusted(ctx, cert, addr, kind, data, rdt); err != nil {
		m.observer.Errors(err)
		return err
	}
	return nil
}

func (m *HTTPMessenger) trusted(ctx context.Context, cert []byte, addr string, kind string, data any, rdt as.RDT) error {
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

	sent, err := send(ctx, &client, addr, kind, data)
	if err != nil {
		m.observer.Errors(err)
		return err
	}

	m.logger.Debug("sent message to trusted peer",
		"peer-rdt", rdt,
		"peer-address", addr,
		"kind", kind,
	)

	m.sent.Add(sent)

	return nil
}

func (m *HTTPMessenger) Untrusted(ctx context.Context, addr string, kind string, data any) ([]byte, error) {
	cert, err := m.untrusted(ctx, addr, kind, data)
	if err != nil {
		m.observer.Errors(err)
		return nil, err
	}
	return cert, nil
}

func (m *HTTPMessenger) untrusted(ctx context.Context, addr string, kind string, data any) ([]byte, error) {
	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				Certificates:       []tls.Certificate{m.cert},
			},
		},
		Timeout: time.Minute,
	}

	res, sent, err := sendWithResponse(ctx, &client, addr, kind, data)
	if err != nil {
		m.observer.Errors(err)
		return nil, err
	}
	defer res.Body.Close()

	m.logger.Debug("sent message to untrusted peer",
		"peer-address", addr,
		"kind", kind,
	)

	m.sent.Add(sent)

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

	return bytes.Clone(res.TLS.PeerCertificates[0].Raw), nil
}

type loggingObserver struct {
	logger *slog.Logger
}

func (lo *loggingObserver) Errors(err error) {
	lo.logger.Error(err.Error())
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

func createCertAndKey(ip net.IP) (certPEM []byte, keyPEM []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, err
	}

	// TODO: rotation, renewal? don't worry about it? for now make it last until
	// the next century, when i'll be gone
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost-ed25519"},
		NotBefore:    now,
		NotAfter:     now.AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{ip},
	}

	cert, err := x509.CreateCertificate(rand.Reader, &template, &template, pub, priv)
	if err != nil {
		return nil, nil, err
	}

	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	return certPEM, keyPEM, nil
}
