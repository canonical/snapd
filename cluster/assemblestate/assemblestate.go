package assemblestate

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
)

// Transport provides an abstraction for defining how incoming and outgoing
// messages are handled in an assembly session.
type Transport interface {
	// Serve starts a server that handles incoming requests and routes them to
	// the provided [AssembleState].
	Serve(ctx context.Context, addr string, cert tls.Certificate, as *AssembleState) error

	// NewClient creates a client for sending outbound messages compatible with
	// this [Transport].
	NewClient(cert tls.Certificate) Client

	// Stats returns the sent and received byte counts for this assembly
	// session.
	Stats() (tx, rx int64)
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

// Discoverer returns a set of addresses that should be considered for assembly.
type Discoverer = func(context.Context) ([]string, error)

// AssembleState contains this device's knowledge of the state of an assembly
// session.
type AssembleState struct {
	st     *state.State
	config AssembleConfig
	logger *slog.Logger

	cert tls.Certificate

	// fields below this are mutated from multiple threads, and must be accessed
	// with the lock held.
	lock sync.Mutex

	// trusted keeps track of all trusted peers.
	trusted map[FP]peer

	// fingerprints keeps track of the TLS certificate fingerprints we know and
	// the RDTs that they are is associated with.
	fingerprints map[RDT]FP

	// addresses keeps track of which address we can reach each device at.
	// Presence in this map does not imply trust. Additionally, a device can be
	// trusted before we have an address.
	addresses map[FP]string

	// discovered keeps track of which addresses we've already discovered. We
	// won't re-send auth messages to these addresses.
	discovered map[string]bool

	// devices keeps track of device identities. Additionally, it helps manage
	// the events that trigger responses to device queries and the events that
	// result in us sending our own queries.
	devices DeviceTracker

	// selector keeps track of our routes and decides the strategy for
	// publishing routes to our peers.
	selector RouteSelector
}

// AssembleSession provides a method for serializing our current state of
// assembly to JSON.
type AssembleSession struct {
	Trusted      map[string]peer   `json:"trusted"`
	Fingerprints map[RDT]FP        `json:"fingerprints"`
	Addresses    map[string]string `json:"addresses"`
	Discovered   map[string]bool   `json:"discovered"`
	Routes       Routes            `json:"routes"`
	Devices      DeviceTrackerData `json:"devices"`
}

func (as *AssembleState) export() AssembleSession {
	trusted := make(map[string]peer, len(as.trusted))
	for fp, p := range as.trusted {
		trusted[base64.StdEncoding.EncodeToString(fp[:])] = p
	}

	addresses := make(map[string]string, len(as.addresses))
	for fp, addr := range as.addresses {
		addresses[base64.StdEncoding.EncodeToString(fp[:])] = addr
	}

	return AssembleSession{
		Trusted:      trusted,
		Fingerprints: as.fingerprints,
		Addresses:    addresses,
		Discovered:   as.discovered,
		Routes:       as.selector.Routes(),
		Devices:      as.devices.Export(),
	}
}

func (as *AssembleState) commit() {
	exported := as.export()

	as.st.Lock()
	defer as.st.Unlock()
	as.st.Set("assemble-session", exported)
}

type peer struct {
	RDT  RDT    `json:"rdt"`
	Cert []byte `json:"cert"`
}

// NewAssembleState create a new [AssembleState]. This currently pulls data from
// the given [state.State] and will resume an existing assemble session. This
// might go away, and we'd take in a more conventional configuration struct.
func NewAssembleState(st *state.State, selector func(self RDT) (RouteSelector, error), logger *slog.Logger) (*AssembleState, error) {
	st.Lock()
	defer st.Unlock()

	// these probably will end up going on a task, maybe?
	var config AssembleConfig
	if err := st.Get("assemble-config", &config); err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair([]byte(config.TLSCert), []byte(config.TLSKey))
	if err != nil {
		return nil, err
	}

	var session AssembleSession
	if err := st.Get("assemble-session", &session); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return nil, err
		}

		session = AssembleSession{
			Trusted:      make(map[string]peer),
			Fingerprints: make(map[RDT]FP),
			Addresses:    make(map[string]string),
			Discovered:   make(map[string]bool),
		}
	}

	trusted := make(map[FP]peer, len(session.Trusted))
	for strFP, peer := range session.Trusted {
		rawFP, err := base64.StdEncoding.DecodeString(strFP)
		if err != nil {
			return nil, err
		}

		if len(rawFP) != 64 {
			return nil, errors.New("certificate fingerprint expected to be 64 bytes")
		}

		var fp FP
		copy(fp[:], rawFP)
		trusted[fp] = peer
	}

	addresses := make(map[FP]string, len(session.Addresses))
	for strFP, addr := range session.Addresses {
		rawFP, err := base64.StdEncoding.DecodeString(strFP)
		if err != nil {
			return nil, err
		}

		if len(rawFP) != 64 {
			return nil, errors.New("certificate fingerprint expected to be 64 bytes")
		}

		var fp FP
		copy(fp[:], rawFP)
		addresses[fp] = addr
	}

	devices := NewDeviceTracker(Identity{
		RDT: config.RDT,
		FP:  CalculateFP(config.TLSCert),
	}, time.Minute*5, session.Devices)

	sel, err := selector(config.RDT)
	if err != nil {
		return nil, err
	}

	// inform the selector of any routes that we already know. we state that
	// their provenance is this local node, since we don't persist which routes
	// came from which peer. this will lead to our local node doing some wasted
	// publications, but that is okay.
	if _, _, err := sel.AddRoutes(config.RDT, session.Routes, devices.Identified); err != nil {
		return nil, err
	}

	// for any peers that we trust and we know their address, we can safely
	// inform the selector that the route from our local node to that peer can
	// be published
	for fp, peer := range trusted {
		addr, ok := addresses[fp]
		if !ok {
			continue
		}

		sel.AddAuthoritativeRoute(peer.RDT, addr)
	}

	as := AssembleState{
		st:           st,
		config:       config,
		logger:       logger,
		cert:         cert,
		trusted:      trusted,
		fingerprints: session.Fingerprints,
		addresses:    addresses,
		discovered:   session.Discovered,
		devices:      devices,
		selector:     sel,
	}

	return &as, nil
}

type AssembleConfig struct {
	Secret  string `json:"secret"`
	RDT     RDT    `json:"rdt"`
	IP      net.IP `json:"ip"`
	Port    int    `json:"port"`
	TLSCert []byte `json:"cert"`
	TLSKey  []byte `json:"key"`
}

// publishAuth calls send for each given address. If send succeeds, then the
// certificate returned by send (the certificate that the peer used to
// communicate with us) is associated with the address that we reached them at.
//
// If the certificate the peer used is already trusted (they've already
// published their auth message to us), then we verify the route from this local
// node to that peer.
func (as *AssembleState) publishAuth(ctx context.Context, addresses []string, client Client) error {
	as.lock.Lock()
	defer as.lock.Unlock()

	hmac := CalculateHMAC(as.config.RDT, CalculateFP(as.cert.Certificate[0]), as.config.Secret)

	for _, addr := range addresses {
		if as.discovered[addr] {
			continue
		}

		as.lock.Unlock()
		cert, err := client.Untrusted(ctx, addr, "auth", Auth{
			HMAC: hmac,
			RDT:  as.config.RDT,
		})
		as.lock.Lock()

		if err != nil {
			continue
		}

		as.logger.Debug("sent auth message", "peer-address", addr)

		fp := CalculateFP(cert)

		if expected, ok := as.addresses[fp]; ok && expected != addr {
			return fmt.Errorf("found new address %s using same certificate as other address %s", addr, expected)
		}

		// TODO: consider devices with multiple addresses
		as.addresses[fp] = addr
		as.discovered[addr] = true

		if p, ok := as.trusted[fp]; ok {
			as.selector.AddAuthoritativeRoute(p.RDT, addr)
		}
	}

	as.commit()

	return nil
}

// publishDeviceQueries requests device information from our trusted peers. We
// request information for devices that have appeared in a route but we don't
// yet have identifying information for.
func (as *AssembleState) publishDeviceQueries(ctx context.Context, client Client) {
	as.lock.Lock()
	defer as.lock.Unlock()

	failure := false
	for fp, p := range as.trusted {
		addr, ok := as.addresses[fp]
		if !ok {
			continue
		}

		queries, ack := as.devices.QueryableFrom(p.RDT)
		if len(queries) == 0 {
			continue
		}

		as.lock.Unlock()
		err := client.Trusted(ctx, addr, p.Cert, "unknown", UnknownDevices{
			Devices: queries,
		})
		as.lock.Lock()

		if err != nil {
			failure = true
			continue
		}

		ack()
		as.logger.Debug("sent device queries", "peer-rdt", p.RDT, "peer-address", addr, "queries-count", len(queries))
	}

	// if anything failed, we need to schedule a retry
	if failure {
		as.devices.RetryQueries()
	}
}

// publishDevices responds to queries for device information that our peers
// have sent us.
func (as *AssembleState) publishDevices(ctx context.Context, client Client) {
	as.lock.Lock()
	defer as.lock.Unlock()

	failure := false
	for fp, p := range as.trusted {
		addr, ok := as.addresses[fp]
		if !ok {
			continue
		}

		ids, ack := as.devices.QueryResponses(p.RDT)
		if len(ids) == 0 {
			continue
		}

		as.lock.Unlock()
		err := client.Trusted(ctx, addr, p.Cert, "devices", Devices{
			Devices: ids,
		})
		as.lock.Lock()

		// skip acking if this publication failed
		if err != nil {
			failure = true
			continue
		}

		ack()
		as.logger.Debug("sent device information", "peer-rdt", p.RDT, "peer-address", addr, "devices-count", len(ids))
	}

	// if anything failed, we need to schedule a retry
	if failure {
		as.devices.RetryResponses()
	}

	// committing here prevents us from responding to requests more than once
	// after something like a reboot. not really a big real to remove this if we
	// need to reduce writes.
	as.commit()
}

// publishRoutes publishes routes that we know about to our trusted peers. See
// implementation of [RouteSelector] for more details on route and peer
// selection strategy.
func (as *AssembleState) publishRoutes(ctx context.Context, client Client, peers, maxRoutes int) {
	as.lock.Lock()
	defer as.lock.Unlock()

	// collect all trusted peers that have also addresses
	var available []peer
	for fp, p := range as.trusted {
		if _, ok := as.addresses[fp]; ok {
			available = append(available, p)
		}
	}

	if len(available) == 0 {
		return
	}

	// shuffle available peers to enable random selection
	for i := len(available) - 1; i > 0; i-- {
		j := randutil.Intn(i + 1)
		available[i], available[j] = available[j], available[i]
	}

	selected := available[:min(peers, len(available))]

	// for each randomly selected peer, get routes and send them
	for _, p := range selected {
		routes, ack, ok := as.selector.Select(p.RDT, maxRoutes)
		if !ok {
			continue // nothing to publish to this peer
		}

		fp, ok := as.fingerprints[p.RDT]
		if !ok {
			continue // skip publishing to an untrusted peer
		}

		// we might trust a peer that we don't know the address of yet.
		addr, ok := as.addresses[fp]
		if !ok {
			continue // skip publishing to an undiscovered peer
		}

		// unlock for the duration of the send. this is safe, since all of the
		// data that is passed in here is owned.
		as.lock.Unlock()
		err := client.Trusted(ctx, addr, p.Cert, "routes", routes)
		as.lock.Lock()

		if err != nil {
			continue
		}

		ack()
		as.logger.Debug("sent routes", "peer-rdt", p.RDT, "peer-address", addr, "routes-count", len(routes.Routes)/3)
	}

	// we don't commit on route publishes, since we don't keep track of which
	// routes we've sent to our peers in the state.
}

// Authenticate checks that the given [Auth] message is valid and proves
// knowledge of the shared secret. If this check is passed, we allow mutation of
// this [AssembleState] via future calls to [AssembleState.VerifyPeer] with the same
// certificate.
//
// An error is returned if the message's HMAC is found to not prove knowledge of
// the shared secret.
func (as *AssembleState) Authenticate(auth Auth, cert []byte) error {
	as.lock.Lock()
	defer as.lock.Unlock()

	fp := CalculateFP(cert)

	expectedHMAC := CalculateHMAC(auth.RDT, fp, as.config.Secret)
	if !hmac.Equal(expectedHMAC, auth.HMAC) {
		return errors.New("received invalid HMAC from peer")
	}

	if _, ok := as.trusted[fp]; ok {
		if as.trusted[fp].RDT != auth.RDT {
			return fmt.Errorf("peer with rdt %v is using a new TLS certificate", auth.RDT)
		}
	} else {
		as.trusted[fp] = peer{
			RDT:  auth.RDT,
			Cert: cert,
		}
	}

	as.fingerprints[auth.RDT] = fp

	// if we have discovered the route to this peer, we should record an
	// authoritative route to it. this ensures that we send the route from our
	// local node to this peer when we publish
	if addr, ok := as.addresses[fp]; ok {
		as.selector.AddAuthoritativeRoute(auth.RDT, addr)
	}

	as.commit()

	as.logger.Debug("got valid auth message", "peer-rdt", auth.RDT)

	return nil
}

// VerifyPeer checks if the given certificate is trusted and maps to a known RDT.
// If it is, then a [PeerHandle] is returned that can be used to modify the state
// of the cluster on this peer's behalf.
//
// An error is returned if the certificate isn't trusted.
func (as *AssembleState) VerifyPeer(cert []byte) (*PeerHandle, error) {
	as.lock.Lock()
	defer as.lock.Unlock()

	fp := CalculateFP(cert)

	p, ok := as.trusted[fp]
	if !ok {
		return nil, errors.New("given TLS certificate is not associated with a trusted RDT")
	}

	return &PeerHandle{
		as:   as,
		peer: p.RDT,
	}, nil
}

// Run starts the assembly process, managing both the server and periodic client operations.
// It returns when the context is cancelled, returning the final routes discovered.
func (as *AssembleState) Run(
	ctx context.Context,
	transport Transport,
	discover Discoverer,
) (Routes, error) {
	addr := fmt.Sprintf("%s:%d", as.config.IP, as.config.Port)
	client := transport.NewClient(as.cert)

	var wg sync.WaitGroup

	// start the server that handles incoming requests
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := transport.Serve(ctx, addr, as.cert, as); err != nil {
			as.logger.Error(err.Error())
		}
	}()

	// start periodic discovery of peers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		periodic(ctx, time.Second*5, time.Second*1, func(ctx context.Context) {
			discoveries, err := discover(ctx)
			if err != nil {
				as.logger.Error(err.Error())
				return
			}

			// filter out our address
			addrs := make([]string, 0, len(discoveries))
			for _, d := range discoveries {
				if d == addr {
					continue
				}
				addrs = append(addrs, d)
			}

			if err := as.publishAuth(ctx, addrs, client); err != nil {
				as.logger.Error(err.Error())
				return
			}
		})
	}()

	// start up the periodic publication of routes
	wg.Add(1)
	go func() {
		defer wg.Done()
		const (
			period = time.Second * 5
			jitter = time.Second
			peers  = 5
			routes = 5000
		)
		periodic(ctx, period, jitter, func(ctx context.Context) {
			as.publishRoutes(ctx, client, peers, routes)
		})
	}()

	// start event-driven device operations
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-as.devices.Responses():
				as.publishDevices(ctx, client)
			case <-as.devices.Queries():
				as.publishDeviceQueries(ctx, client)
			case <-ctx.Done():
				return
			}
		}
	}()

	// wait for context cancellation
	wg.Wait()

	sent, received := transport.Stats()
	as.logger.Info("assemble stopped",
		"sent-bytes", sent,
		"received-bytes", received,
	)

	return as.selector.Routes(), nil
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

type PeerHandle struct {
	as   *AssembleState
	peer RDT
}

// RDT returns the RDT of the device that this [PeerHandle] represents.
func (h *PeerHandle) RDT() RDT {
	return h.peer
}

// RecordDeviceQueries adds the given devices to the queue of queries for this
// peer. If any devices are unknown, no devices are added to the queue and an
// error is returned. If this local node is queried for devices that we do not
// know, either this local node or the requesting peer has a bug.
func (h *PeerHandle) AddQueries(unknown UnknownDevices) error {
	h.as.lock.Lock()
	defer h.as.lock.Unlock()

	h.as.devices.Query(h.peer, unknown.Devices)

	h.as.commit()
	h.as.logger.Debug("got device queries", "peer-rdt", h.peer)

	return nil
}

// AddRoutes updates the state of the cluster with the given routes.
func (h *PeerHandle) AddRoutes(routes Routes) error {
	h.as.lock.Lock()
	defer h.as.lock.Unlock()

	// if this peer is sending us routes that include these devices, then we
	// know that they must have identifying information for those devices.
	h.as.devices.UpdateSource(h.peer, routes.Devices)

	added, total, err := h.as.selector.AddRoutes(h.peer, routes, h.as.devices.Identified)
	if err != nil {
		return err
	}

	h.as.commit()

	received := len(routes.Routes) / 3
	h.as.logger.Debug("got routes update",
		"peer-rdt", h.peer,
		"received-routes", received,
		"wasted-routes", received-added,
		"total-routes", total,
	)

	return nil
}

// AddDevices records the given device identities. All new device identities are
// recorded. For any devices that we are already aware of, we check that our
// view of the device's identity is consistent with the new data.
func (h *PeerHandle) AddDevices(devices Devices) error {
	h.as.lock.Lock()
	defer h.as.lock.Unlock()

	for _, id := range devices.Devices {
		if current, ok := h.as.devices.Lookup(id.RDT); ok {
			if current != id {
				return errors.New("got inconsistent device identity")
			}
		}
	}

	for _, id := range devices.Devices {
		h.as.devices.Identify(id)
	}

	// since we got new device info, we have to recalculate which routes are
	// valid to send to our peers
	h.as.selector.VerifyRoutes(h.as.devices.Identified)

	h.as.commit()

	h.as.logger.Debug("got unknown device information", "peer-rdt", h.peer, "devices-count", len(devices.Devices))

	return nil
}

func CalculateHMAC(rdt RDT, fp FP, secret string) []byte {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(fp[:])
	mac.Write([]byte(rdt))
	return mac.Sum(nil)
}

func CalculateFP(cert []byte) FP {
	return sha512.Sum512(cert)
}
