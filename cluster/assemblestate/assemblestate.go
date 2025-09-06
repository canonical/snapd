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
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/randutil"
)

// AssembleState contains this device's knowledge of the state of an assembly
// session.
type AssembleState struct {
	config    AssembleConfig
	commit    func(AssembleSession)
	initiated time.Time
	clock     func() time.Time

	cert     tls.Certificate
	authHMAC []byte

	// fields below this are mutated from multiple threads, and must be accessed
	// with the lock held.
	lock sync.Mutex

	// trusted keeps track of all trusted peers.
	trusted map[Fingerprint]Peer

	// fingerprints keeps track of the TLS certificate fingerprints we know and
	// the RDTs that they are is associated with.
	fingerprints map[DeviceToken]Fingerprint

	// addresses keeps track of which address we can reach each device at.
	// Presence in this map does not imply trust. Additionally, a device can be
	// trusted before we have an address.
	addresses map[Fingerprint]string

	// discovered keeps track of which addresses we've already discovered. We
	// won't re-send auth messages to these addresses.
	discovered map[string]bool

	// devices keeps track of device identities. Additionally, it helps manage
	// the events that trigger responses to device queries and the events that
	// result in us sending our own queries.
	devices DeviceQueryTracker

	// selector keeps track of our routes and decides the strategy for
	// publishing routes to our peers.
	selector RouteSelector

	// completed is used to signal to the [AssembleState.Run] method that we
	// have assembled a fully connected graph of the expected number of devices.
	// This is only relevant when an expected size is provided.
	completed chan struct{}
}

// AssembleSession provides a method for serializing our current state of
// assembly to JSON.
type AssembleSession struct {
	Initiated  time.Time              `json:"initiated"`
	Trusted    map[string]Peer        `json:"trusted"`
	Addresses  map[string]string      `json:"addresses"`
	Discovered []string               `json:"discovered"`
	Routes     Routes                 `json:"routes"`
	Devices    DeviceQueryTrackerData `json:"devices"`
}

func (as *AssembleState) export() AssembleSession {
	var trusted map[string]Peer
	if len(as.trusted) > 0 {
		trusted = make(map[string]Peer, len(as.trusted))
		for fp, p := range as.trusted {
			trusted[base64.StdEncoding.EncodeToString(fp[:])] = p
		}
	}

	var addresses map[string]string
	if len(as.addresses) > 0 {
		addresses = make(map[string]string, len(as.addresses))
		for fp, addr := range as.addresses {
			addresses[base64.StdEncoding.EncodeToString(fp[:])] = addr
		}
	}

	var discovered []string
	if len(as.discovered) > 0 {
		discovered = make([]string, 0, len(as.discovered))
		for addr := range as.discovered {
			discovered = append(discovered, addr)
		}
	}

	return AssembleSession{
		Initiated:  as.initiated,
		Trusted:    trusted,
		Addresses:  addresses,
		Discovered: discovered,
		Routes:     as.selector.Routes(),
		Devices:    as.devices.Export(),
	}
}

// Peer is a peer that has established trust via proof of the shared secret in
// an assemble session.
type Peer struct {
	// RDT is the device token that this peer used to identity itself.
	RDT DeviceToken `json:"rdt"`
	// Cert is the TLS certificate that this peer used to send its messages.
	Cert []byte `json:"cert"`
}

// AssembleConfig contains the configuration parameters required to initialize
// an AssembleState and participate in an assembly session.
type AssembleConfig struct {
	// Secret is the shared secret used for HMAC-based peer authentication.
	Secret string
	// RDT is this device's random device token used to uniquely identity this
	// device.
	RDT DeviceToken
	// TLSCert is the PEM-encoded TLS certificate for this device.
	TLSCert []byte
	// TLSKey is the PEM-encoded private key corresponding to TLSCert.
	TLSKey []byte
	// Clock is an optional function to retrieve the current time. If nil,
	// defaults to time.Now.
	Clock func() time.Time
	// ExpectedSize is the expected size of the cluster. If unset, cluster
	// assembly will not terminate automatically.
	ExpectedSize int
}

const AssembleSessionLength = time.Hour

// NewAssembleState creates a new [AssembleState] from the given configuration
// and session data.
func NewAssembleState(
	config AssembleConfig,
	session AssembleSession,
	selector func(self DeviceToken, identified func(DeviceToken) bool) (RouteSelector, error),
	commit func(AssembleSession),
) (*AssembleState, error) {
	// default clock to time.Now if not provided
	if config.Clock == nil {
		config.Clock = time.Now
	}

	// validate the given session parse it into more useful data structures
	validated, err := validateSession(session, config.Clock)
	if err != nil {
		return nil, fmt.Errorf("invalid session data: %w", err)
	}

	cert, err := tls.X509KeyPair([]byte(config.TLSCert), []byte(config.TLSKey))
	if err != nil {
		return nil, err
	}

	if err := ensureLocalDevicePresent(&validated.devices, Identity{
		RDT: config.RDT,
		FP:  CalculateFP(cert.Certificate[0]),
	}); err != nil {
		return nil, err
	}

	devices := NewDeviceQueryTracker(validated.devices, time.Minute*5, config.Clock)

	sel, err := selector(config.RDT, devices.Identified)
	if err != nil {
		return nil, err
	}

	// inform the selector of any routes that we already know. we state that
	// their provenance is this local node, since we don't persist which routes
	// came from which peer. this will lead to our local node doing some wasted
	// publications, but that is okay.
	if _, _, err := sel.RecordRoutes(config.RDT, validated.routes); err != nil {
		return nil, err
	}

	// for any peers that we already trust and know their address, we can safely
	// inform the selector that the route from our local node to that peer can
	// be published
	for fp, peer := range validated.trusted {
		addr, ok := validated.addresses[fp]
		if !ok {
			continue
		}

		sel.AddAuthoritativeRoute(peer.RDT, addr)
	}

	// calculate the HMAC once for this device's authentication
	authHMAC := CalculateHMAC(config.RDT, CalculateFP(cert.Certificate[0]), config.Secret)

	as := AssembleState{
		initiated:    validated.initiated,
		config:       config,
		commit:       commit,
		clock:        config.Clock,
		cert:         cert,
		authHMAC:     authHMAC,
		trusted:      validated.trusted,
		fingerprints: validated.fingerprints,
		addresses:    validated.addresses,
		discovered:   validated.discovered,
		devices:      devices,
		selector:     sel,
		completed:    make(chan struct{}, 1),
	}

	// there is a chance that we resume an assemble session that was already
	// finished. make sure to mark it as complete in that case.
	if err := as.shutdownAtExpectedSize(); err != nil {
		return nil, err
	}

	return &as, nil
}

// publishAuthAndCommit uses the given [Client] to publish to each device. If
// publication succeeds, then the certificate returned by [Client.Untrusted]
// (the certificate that the peer used to communicate with us) is associated
// with the address that we reached them at.
//
// If the certificate the peer used is already trusted (they've already
// published their auth message to us), then we verify the route from this local
// node to that peer.
//
// This method calls AssembleState.commit with the current state.
func (as *AssembleState) publishAuthAndCommit(ctx context.Context, addresses []string, client Client) error {
	as.lock.Lock()
	defer as.lock.Unlock()

	for _, addr := range addresses {
		if as.discovered[addr] {
			continue
		}

		cert, err := untrustedSend(ctx, &as.lock, client, addr, "auth", Auth{
			HMAC: as.authHMAC,
			RDT:  as.config.RDT,
		})
		if err != nil {
			logger.Debugf("cannot send auth message: %v", err)
			continue
		}

		logger.Debugf("sent auth message to %s", addr)

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

	as.commit(as.export())

	return nil
}

// publishDeviceQueries requests device information from our trusted peers. We
// request information for devices that have appeared in a route but we don't
// yet have identifying information for.
func (as *AssembleState) publishDeviceQueries(ctx context.Context, client Client) {
	as.lock.Lock()
	defer as.lock.Unlock()

	for fp, p := range as.trusted {
		addr, ok := as.addresses[fp]
		if !ok {
			continue
		}

		queries, ack := as.devices.OutgoingQueriesTo(p.RDT)
		if len(queries) == 0 {
			continue
		}

		err := trustedSend(ctx, &as.lock, client, addr, p.Cert, "unknown", UnknownDevices{
			Devices: queries,
		})
		ack(err == nil)
		if err != nil {
			logger.Debugf("cannot publish device query: %v", err)
			continue
		}

		logger.Debugf("sent device queries to %s at %s, count: %d", p.RDT, addr, len(queries))
	}

	// we don't commit when publishing device queries since we don't keep track
	// of which queries are in-flight in persistent state. at worst, we send out
	// a duplicate query for device identities when resuming a session.
}

// publishDevicesAndCommit responds to queries for device information that our
// peers have sent us.
//
// This method calls AssembleState.commit with the current state.
func (as *AssembleState) publishDevicesAndCommit(ctx context.Context, client Client) {
	as.lock.Lock()
	defer as.lock.Unlock()

	for fp, p := range as.trusted {
		addr, ok := as.addresses[fp]
		if !ok {
			continue
		}

		ids, ack := as.devices.ResponsesTo(p.RDT)
		if len(ids) == 0 {
			continue
		}

		err := trustedSend(ctx, &as.lock, client, addr, p.Cert, "devices", Devices{
			Devices: ids,
		})
		ack(err == nil)
		if err != nil {
			logger.Debugf("cannot publish device identities: %v", err)
			continue
		}

		logger.Debugf("sent device information to %s at %s, count: %d", p.RDT, addr, len(ids))
	}

	as.commit(as.export())
}

// publishRoutes publishes routes that we know about to our trusted peers. See
// implementation of [RouteSelector] for more details on route and peer
// selection strategy.
func (as *AssembleState) publishRoutes(ctx context.Context, client Client, maxPeers, maxRoutes int) {
	as.lock.Lock()
	defer as.lock.Unlock()

	// collect all trusted peers that have also addresses
	var available []Peer
	for fp, p := range as.trusted {
		if _, ok := as.addresses[fp]; ok {
			available = append(available, p)
		}
	}

	if len(available) == 0 {
		return
	}

	// shuffle available peers to enable random selection
	shuffle(available)

	selected := available[:min(maxPeers, len(available))]

	// for each randomly selected peer, get routes and send them
	for _, p := range selected {
		routes, ack, ok := as.selector.Select(p.RDT, maxRoutes)
		if !ok {
			continue
		}

		fp, ok := as.fingerprints[p.RDT]
		if !ok {
			continue
		}

		addr, ok := as.addresses[fp]
		if !ok {
			continue
		}

		if err := trustedSend(ctx, &as.lock, client, addr, p.Cert, "routes", routes); err != nil {
			logger.Debugf("cannot publish routes: %v", err)
			continue
		}

		ack()
		logger.Debugf("sent routes to %s at %s, count: %d", p.RDT, addr, len(routes.Routes)/3)
	}

	// we don't commit on route publishes since we don't keep track of which
	// routes we've sent to our peers in the state. at worst, we send routes
	// to a peer again after resuming a session.
}

func shuffle[T any](available []T) {
	for i := len(available) - 1; i > 0; i-- {
		j := randutil.Intn(i + 1)
		available[i], available[j] = available[j], available[i]
	}
}

// authenticateAndCommit checks that the given [Auth] message is valid and proves
// knowledge of the shared secret. If this check is passed, we allow mutation of
// this [AssembleState] via future calls to [AssembleState.verifyPeer] with the same
// certificate.
//
// An error is returned if the message's HMAC is found to not prove knowledge of
// the shared secret.
//
// This method is to be called by an implementation of the [Transport] interface.
//
// This method calls AssembleState.commit with the current state.
func (as *AssembleState) AuthenticateAndCommit(auth Auth, cert []byte) error {
	as.lock.Lock()
	defer as.lock.Unlock()

	fp := CalculateFP(cert)

	expectedHMAC := CalculateHMAC(auth.RDT, fp, as.config.Secret)
	if !hmac.Equal(expectedHMAC, auth.HMAC) {
		return errors.New("received invalid HMAC from peer")
	}

	if _, ok := as.trusted[fp]; ok {
		if as.trusted[fp].RDT != auth.RDT {
			return fmt.Errorf("peer %q and %q are using the same TLS certificate", as.trusted[fp].RDT, auth.RDT)
		}
	} else {
		as.trusted[fp] = Peer{
			RDT:  auth.RDT,
			Cert: cert,
		}
	}

	if existing, ok := as.fingerprints[auth.RDT]; ok {
		if existing != fp {
			return fmt.Errorf("peer %q is using a new TLS certificate", auth.RDT)
		}
	} else {
		as.fingerprints[auth.RDT] = fp
	}

	// check fingerprint consistency if we already have an identity for this peer
	if id, ok := as.devices.Lookup(auth.RDT); ok && id.FP != fp {
		return fmt.Errorf("fingerprint mismatch for device %s", auth.RDT)
	}

	// if we have discovered the route to this peer, we should record an
	// authoritative route to it. this ensures that we send the route from our
	// local node to this peer when we publish
	if addr, ok := as.addresses[fp]; ok {
		as.selector.AddAuthoritativeRoute(auth.RDT, addr)
	}

	as.commit(as.export())

	logger.Debugf("got valid auth message from %s", auth.RDT)

	return nil
}

// VerifyPeer checks if the given certificate is trusted and maps to a known
// RDT. If it is, then a [VerifiedPeer] is returned that can be used to modify
// the state of the cluster on this peer's behalf.
//
// An error is returned if the certificate isn't trusted.
//
// This method is to be called by an implementation of the [Transport] interface.
func (as *AssembleState) VerifyPeer(cert []byte) (VerifiedPeer, error) {
	as.lock.Lock()
	defer as.lock.Unlock()

	fp := CalculateFP(cert)

	p, ok := as.trusted[fp]
	if !ok {
		return nil, errors.New("given TLS certificate is not associated with a trusted RDT")
	}

	return &peerHandle{
		as:  as,
		rdt: p.RDT,
	}, nil
}

// RunOptions carries some options relevant to [AssembleState.Run]. These should
// be only runtime-relevant parameters.
type RunOptions struct {
	// Period is how often we publish route information.
	Period time.Duration
}

// Run starts the assembly process, managing both the server and periodic client operations.
// It returns when the context is cancelled, returning the final routes discovered.
//
// TODO: a good chunk of this method is missing coverage, this should be
// addressed once a real [Transport] is merged.
func (as *AssembleState) Run(
	ctx context.Context,
	ln net.Listener,
	transport Transport,
	discoveries <-chan []string,
	opts RunOptions,
) (Routes, error) {
	if as.initiated.IsZero() {
		as.initiated = as.clock()
	}

	if as.clock().Sub(as.initiated) > AssembleSessionLength {
		return Routes{}, errors.New("cannot resume an assembly session that began more than an hour ago")
	}

	addr := ln.Addr().String()
	client := transport.NewClient(as.cert)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	// channel to receive server errors that should cause the process to fail
	// TODO: replace with [context.Cause] when we're on go >= 1.20
	serverError := make(chan error, 1)

	// start the server that handles incoming requests
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := transport.Serve(ctx, ln, as.cert, as); err != nil {
			// only propagate non-context.Canceled errors
			if !errors.Is(err, context.Canceled) {
				logger.Debugf("server error: %v", err)
				cancel()
				serverError <- err
			}
		}
	}()

	// start discovery of peers from channel
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case discoveries := <-discoveries:
				// filter out our address
				addrs := make([]string, 0, len(discoveries))
				for _, d := range discoveries {
					if d == addr {
						continue
					}
					addrs = append(addrs, d)
				}

				if err := as.publishAuthAndCommit(ctx, addrs, client); err != nil {
					logger.Debugf("error publishing auth messages: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	var rounds int

	// start up the periodic publication of routes
	wg.Add(1)
	go func() {
		defer wg.Done()
		const (
			peers  = 5
			routes = 5000
		)
		periodic(ctx, opts.Period, func(ctx context.Context) {
			as.publishRoutes(ctx, client, peers, routes)
			rounds++
		})
	}()

	// start event-driven device operations
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-as.devices.PendingResponses():
				as.publishDevicesAndCommit(ctx, client)
			case <-as.devices.PendingOutgoingQueries():
				as.publishDeviceQueries(ctx, client)
			case <-ctx.Done():
				return
			}
		}
	}()

	// wait for a completion signal
	wg.Add(1)
	go func() {
		defer wg.Done()

		select {
		case <-as.completed:
			logger.Debug("assembly complete: discovered all devices with full connectivity")
			cancel()
		case <-ctx.Done():
		}
	}()

	// wait for context cancellation
	wg.Wait()

	select {
	case err := <-serverError:
		return Routes{}, fmt.Errorf("server failed: %w", err)
	default:
	}

	// perform final fingerprint consistency check
	devices := as.devices.Export()
	for _, identity := range devices.IDs {
		if fp, ok := as.fingerprints[identity.RDT]; ok {
			if fp != identity.FP {
				return Routes{}, fmt.Errorf("consistency check failed: fingerprint mismatch for device %s", identity.RDT)
			}
		}
	}

	stats := transport.Stats()
	logger.Debugf(
		"assemble stopped after %d rounds, sent: %d messages (%d bytes), received: %d messages (%d bytes)",
		rounds, stats.Sent, stats.Tx, stats.Received, stats.Rx,
	)

	return as.selector.Routes(), nil
}

func periodic(
	ctx context.Context,
	interval time.Duration,
	work func(ctx context.Context),
) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}

		work(ctx)
	}
}

// peerHandle is a wrapper over [AssembleState] that enables an authenticated
// peer report its knowledge of the state of the cluster.
type peerHandle struct {
	as  *AssembleState
	rdt DeviceToken
}

// CommitDeviceQueries adds the given devices to the queue of queries for this
// peer. If any devices are unknown, no devices are added to the queue and an
// error is returned. If this local node is queried for devices that we do not
// know, either this local node or the requesting peer has a bug.
//
// This method is to be called by an implementation of the [Transport] interface.
func (h *peerHandle) CommitDeviceQueries(unknown UnknownDevices) error {
	h.as.lock.Lock()
	defer h.as.lock.Unlock()

	h.as.devices.RecordIncomingQuery(h.rdt, unknown.Devices)

	h.as.commit(h.as.export())
	logger.Debugf("got device queries from %q", h.rdt)

	return nil
}

// CommitRoutes updates the state of the cluster with the given routes.
//
// This method is to be called by an implementation of the [Transport] interface.
func (h *peerHandle) CommitRoutes(routes Routes) error {
	h.as.lock.Lock()
	defer h.as.lock.Unlock()

	// if this peer is sending us routes that include these devices, then we
	// know that they must have identifying information for those devices.
	h.as.devices.RecordDevicesKnownBy(h.rdt, routes.Devices)

	added, total, err := h.as.selector.RecordRoutes(h.rdt, routes)
	if err != nil {
		return err
	}

	if err := h.as.shutdownAtExpectedSize(); err != nil {
		return err
	}

	h.as.commit(h.as.export())

	// routes are represented by an array of triplets, refer to the doc comment
	// on [Routes] for more information
	received := len(routes.Routes) / 3
	logger.Debugf("got routes update from %s, received: %d, wasted: %d, total: %d", h.rdt, received, received-added, total)

	return nil
}

func (as *AssembleState) shutdownAtExpectedSize() error {
	if as.config.ExpectedSize == 0 {
		return nil
	}

	complete, err := as.selector.Complete(as.config.ExpectedSize)
	if err != nil {
		return err
	}

	if complete {
		select {
		case as.completed <- struct{}{}:
		default:
		}
	}

	return nil
}

// CommitDevices records the given device identities. All new device identities
// are recorded. For any devices that we are already aware of, we check that our
// view of the device's identity is consistent with the new data.
//
// This method is to be called by an implementation of the [Transport] interface.
//
// This method calls AssembleState.commit with the current state.
func (h *peerHandle) CommitDevices(devices Devices) error {
	h.as.lock.Lock()
	defer h.as.lock.Unlock()

	for _, id := range devices.Devices {
		if current, ok := h.as.devices.Lookup(id.RDT); ok {
			if current != id {
				return errors.New("got inconsistent device identity")
			}
		}

		// check fingerprint consistency if we know this peer's fingerprint
		if fp, ok := h.as.fingerprints[id.RDT]; ok && fp != id.FP {
			return fmt.Errorf("fingerprint mismatch for device %s", id.RDT)
		}
	}

	for _, id := range devices.Devices {
		h.as.devices.RecordIdentity(id)
	}

	// TODO: i don't really love the implicit connection of as.devices and
	// as.selector here
	//
	// since we got new device info, we have to recalculate which routes are
	// valid to send to our peers
	h.as.selector.VerifyRoutes()

	if err := h.as.shutdownAtExpectedSize(); err != nil {
		return err
	}

	h.as.commit(h.as.export())

	logger.Debugf("got unknown device information from %s, count: %d", h.rdt, len(devices.Devices))

	return nil
}

func CalculateHMAC(rdt DeviceToken, fp Fingerprint, secret string) []byte {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(fp[:])
	mac.Write([]byte(rdt))
	return mac.Sum(nil)
}

func CalculateFP(cert []byte) Fingerprint {
	return sha512.Sum512(cert)
}

// trustedSend releases the given lock and calls client.Trusted.
func trustedSend(
	ctx context.Context, lock *sync.Mutex, client Client,
	addr string, cert []byte, kind string, data any,
) error {
	lock.Unlock()
	defer lock.Lock()
	return client.Trusted(ctx, addr, cert, kind, data)
}

// untrustedSend releases the given lock and calls client.Untrusted.
func untrustedSend(
	ctx context.Context, lock *sync.Mutex, client Client,
	addr string, kind string, data any,
) ([]byte, error) {
	lock.Unlock()
	defer lock.Lock()
	return client.Untrusted(ctx, addr, kind, data)
}

// ensureLocalDevicePresent adds the local device identity to the IDs slice if
// not present, or validates consistency if already present.
func ensureLocalDevicePresent(data *DeviceQueryTrackerData, self Identity) error {
	for _, existing := range data.IDs {
		if existing.RDT == self.RDT {
			if existing.FP != self.FP {
				return fmt.Errorf("fingerprint mismatch for local device %q", self.RDT)
			}

			return nil
		}
	}

	data.IDs = append(data.IDs, self)

	return nil
}

// parseAndValidateFingerprint parses a base64-encoded fingerprint and validates
// its length
func parseAndValidateFingerprint(strFP string) (Fingerprint, error) {
	rawFP, err := base64.StdEncoding.DecodeString(strFP)
	if err != nil {
		return Fingerprint{}, err
	}
	if len(rawFP) != 64 {
		return Fingerprint{}, errors.New("certificate fingerprint expected to be 64 bytes")
	}
	var fp Fingerprint
	copy(fp[:], rawFP)
	return fp, nil
}

// validatedSession contains pre-parsed session data with field names matching
// [AssembleState].
type validatedSession struct {
	trusted      map[Fingerprint]Peer
	fingerprints map[DeviceToken]Fingerprint
	addresses    map[Fingerprint]string
	discovered   map[string]bool
	routes       Routes
	devices      DeviceQueryTrackerData
	initiated    time.Time
}

// validateSession checks that the given session maintains internal consistency
// invariants that should be preserved between export and import, and returns
// all parsed data for use by NewAssembleState.
func validateSession(session AssembleSession, clock func() time.Time) (validatedSession, error) {
	if !session.Initiated.IsZero() && clock().Sub(session.Initiated) > AssembleSessionLength {
		return validatedSession{}, errors.New("cannot resume an assembly session that began more than an hour ago")
	}

	if len(session.Routes.Routes)%3 != 0 {
		return validatedSession{}, errors.New("routes array length must be multiple of 3")
	}

	for i := 0; i < len(session.Routes.Routes); i += 3 {
		src := session.Routes.Routes[i]
		dest := session.Routes.Routes[i+1]
		addr := session.Routes.Routes[i+2]

		if src < 0 || src >= len(session.Routes.Devices) {
			return validatedSession{}, fmt.Errorf("invalid source device index %d in routes", src)
		}
		if dest < 0 || dest >= len(session.Routes.Devices) {
			return validatedSession{}, fmt.Errorf("invalid destination device index %d in routes", dest)
		}
		if addr < 0 || addr >= len(session.Routes.Addresses) {
			return validatedSession{}, fmt.Errorf("invalid address index %d in routes", addr)
		}
	}

	trusted := make(map[Fingerprint]Peer, len(session.Trusted))
	fingerprints := make(map[DeviceToken]Fingerprint, len(session.Trusted))
	for strFP, peer := range session.Trusted {
		fp, err := parseAndValidateFingerprint(strFP)
		if err != nil {
			return validatedSession{}, fmt.Errorf("invalid fingerprint in trusted peers: %w", err)
		}

		trusted[fp] = peer
		fingerprints[peer.RDT] = fp
	}

	addresses := make(map[Fingerprint]string, len(session.Addresses))
	addressSet := make(map[string]bool, len(session.Addresses))
	for strFP, addr := range session.Addresses {
		fp, err := parseAndValidateFingerprint(strFP)
		if err != nil {
			return validatedSession{}, fmt.Errorf("invalid fingerprint in addresses: %w", err)
		}

		addresses[fp] = addr
		addressSet[addr] = true
	}

	discovered := make(map[string]bool)
	for _, addr := range session.Discovered {
		if !addressSet[addr] {
			return validatedSession{}, fmt.Errorf("discovered address %q not found in addresses map", addr)
		}
		discovered[addr] = true
	}

	return validatedSession{
		routes:       session.Routes,
		devices:      session.Devices,
		initiated:    session.Initiated,
		trusted:      trusted,
		fingerprints: fingerprints,
		addresses:    addresses,
		discovered:   discovered,
	}, nil
}
