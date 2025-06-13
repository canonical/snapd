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

	"github.com/snapcore/snapd/overlord/state"
)

// Messenger is used to communicate with our peers.
type Messenger interface {
	// Trusted sends a message to a trusted peer. Implementations must verify
	// that the peer is using the given certificate.
	Trusted(ctx context.Context, rdt RDT, addr string, cert []byte, kind string, message any) error
	// Untrusted sends a message to a peer that we do not yet trust. The
	// certificate that the peer used to communicate is returned.
	Untrusted(ctx context.Context, addr string, kind string, message any) (cert []byte, err error)
}

// ClusterState contains this device's knowledge of the state of an assembly
// session.
type ClusterState struct {
	st     *state.State
	config ClusterConfig

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

	// devices keeps track of which devices for which we've seen their
	// identifying information. Additionally, it keeps track of the set of
	// devices that each of our peers knows about.
	devices deviceIDs

	// publisher keeps track of our routes and decides the strategy for
	// publishing routes to our peers.
	publisher RoutePublisher
}

// AssembleSession provides a method for serializing our current state of
// assembly to JSON.
type AssembleSession struct {
	Trusted      map[string]peer   `json:"trusted"`
	Fingerprints map[RDT]FP        `json:"fingerprints"`
	Addresses    map[string]string `json:"addresses"`
	Discovered   map[string]bool   `json:"discovered"`
	Devices      deviceIDs         `json:"devices"`
	Routes       Routes            `json:"routes"`
}

func (cs *ClusterState) export() AssembleSession {
	trusted := make(map[string]peer, len(cs.trusted))
	for fp, p := range cs.trusted {
		trusted[base64.StdEncoding.EncodeToString(fp[:])] = p
	}

	addresses := make(map[string]string, len(cs.addresses))
	for fp, addr := range cs.addresses {
		addresses[base64.StdEncoding.EncodeToString(fp[:])] = addr
	}

	return AssembleSession{
		Trusted:      trusted,
		Fingerprints: cs.fingerprints,
		Addresses:    addresses,
		Discovered:   cs.discovered,
		Devices:      cs.devices,
		Routes:       cs.publisher.Routes(),
	}
}

func (cs *ClusterState) commit() {
	exported := cs.export()

	cs.st.Lock()
	defer cs.st.Unlock()
	cs.st.Set("cluster-assemble-session", exported)
}

type peer struct {
	RDT  RDT    `json:"rdt"`
	Cert []byte `json:"cert"`
}

// NewClusterState create a new [ClusterState]. This currently pulls data from
// the given [state.State] and will resume an existing assemble session. This
// might go away, and we'd take in a more conventional configuration struct.
func NewClusterState(st *state.State, publisher func(self RDT) (RoutePublisher, error)) (*ClusterState, error) {
	st.Lock()
	defer st.Unlock()

	// these probably will end up going on a task, maybe?
	var config ClusterConfig
	if err := st.Get("cluster-config", &config); err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair([]byte(config.TLSCert), []byte(config.TLSKey))
	if err != nil {
		return nil, err
	}

	var session AssembleSession
	if err := st.Get("cluster-assemble-session", &session); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return nil, err
		}

		session = AssembleSession{
			Trusted:      make(map[string]peer),
			Fingerprints: make(map[RDT]FP),
			Addresses:    make(map[string]string),
			Discovered:   make(map[string]bool),
			Devices: newDeviceIDs(Identity{
				RDT: config.RDT,
				FP:  CalculateFP(config.TLSCert),
			}),
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

	pub, err := publisher(config.RDT)
	if err != nil {
		return nil, err
	}

	// inform the publisher of any routes that we already know. we state that
	// their provenance is this local node, since we don't persist which routes
	// came from which peer. this will lead to our local node doing some wasted
	// publications, but that is okay.
	if _, _, err := pub.AddRoutes(config.RDT, session.Routes, session.Devices.Identified); err != nil {
		return nil, err
	}

	// for any peers that we trust and we know their address, we can safely
	// inform the publisher that the route from our local node to that peer can
	// be published
	for fp, peer := range trusted {
		addr, ok := addresses[fp]
		if !ok {
			continue
		}

		pub.AddAuthoritativeRoute(peer.RDT, addr)
	}

	cs := ClusterState{
		st:           st,
		config:       config,
		cert:         cert,
		trusted:      trusted,
		fingerprints: session.Fingerprints,
		addresses:    addresses,
		discovered:   session.Discovered,
		devices:      session.Devices,
		publisher:    pub,
	}

	return &cs, nil
}

type ClusterConfig struct {
	Secret  string `json:"secret"`
	RDT     RDT    `json:"rdt"`
	IP      net.IP `json:"ip"`
	Port    int    `json:"port"`
	TLSCert []byte `json:"cert"`
	TLSKey  []byte `json:"key"`
}

// Routes returns all of the routes that we've collected and verified.
func (cs *ClusterState) Routes() Routes {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	return cs.publisher.Routes()
}

// Address returns the address of this local node.
func (cs *ClusterState) Address() string {
	return fmt.Sprintf("%s:%d", cs.config.IP, cs.config.Port)
}

// Cert returns the TLS certificate that this local node should use when
// communicating with other peers.
func (cs *ClusterState) Cert() tls.Certificate {
	return cs.cert
}

// PublishAuth calls send for each given address. If send succeeds, then the
// certificate returned by send (the certificate that the peer used to
// communicate with us) is associated with the address that we reached them at.
//
// If the certificate the peer used is already trusted (they've already
// published their auth message to us), then we verify the route from this local
// node to that peer.
func (cs *ClusterState) PublishAuth(ctx context.Context, addresses []string, msg Messenger) error {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	send := func(addr string, a Auth) ([]byte, error) {
		cs.lock.Unlock()
		defer cs.lock.Lock()

		return msg.Untrusted(ctx, addr, "auth", a)
	}

	hmac := CalculateHMAC(cs.config.RDT, CalculateFP(cs.cert.Certificate[0]), cs.config.Secret)

	for _, addr := range addresses {
		if cs.discovered[addr] {
			continue
		}

		cert, err := send(addr, Auth{
			HMAC: hmac,
			RDT:  cs.config.RDT,
		})
		if err != nil {
			continue
		}

		fp := CalculateFP(cert)

		if expected, ok := cs.addresses[fp]; ok && expected != addr {
			return fmt.Errorf("found new address %s using same certificate as other address %s", addr, expected)
		}

		// TODO: consider devices with multiple addresses
		cs.addresses[fp] = addr
		cs.discovered[addr] = true

		if p, ok := cs.trusted[fp]; ok {
			cs.publisher.AddAuthoritativeRoute(p.RDT, addr)
		}
	}

	cs.commit()

	return nil
}

// PublishDeviceQueries requests device information from our trusted peers. We
// request information for devices that have appeared in a route but we don't
// yet have identifying information for.
func (cs *ClusterState) PublishDeviceQueries(ctx context.Context, msg Messenger) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	send := func(rdt RDT, addr string, cert []byte, u UnknownDevices) error {
		cs.lock.Unlock()
		defer cs.lock.Lock()

		return msg.Trusted(ctx, rdt, addr, cert, "unknown", u)
	}

	// TODO: this could be smarter, but the complexity probably isn't worth it
	// since the large majority of bandwidth is used for route propagation
	for fp, p := range cs.trusted {
		addr, ok := cs.addresses[fp]
		if !ok {
			continue
		}

		queries := cs.devices.UnknownKnownBy(p.RDT)
		if len(queries) == 0 {
			continue
		}

		if err := send(p.RDT, addr, p.Cert, UnknownDevices{
			Devices: queries,
		}); err != nil {
			continue
		}
	}
}

// PublishDevices responds to queries for device information that our peers
// have sent us.
func (cs *ClusterState) PublishDevices(ctx context.Context, msg Messenger) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	send := func(rdt RDT, addr string, cert []byte, d Devices) error {
		cs.lock.Unlock()
		defer cs.lock.Lock()

		return msg.Trusted(ctx, rdt, addr, cert, "devices", d)
	}

	for fp, p := range cs.trusted {
		addr, ok := cs.addresses[fp]
		if !ok {
			continue
		}

		ids := cs.devices.QueryResponses(p.RDT)
		if len(ids) == 0 {
			continue
		}

		if err := send(p.RDT, addr, p.Cert, Devices{
			Devices: ids,
		}); err != nil {
			continue
		}

		cs.devices.AckQueries(p.RDT, ids)
	}

	// committing here prevents us from responding to requests more than once
	// after something like a reboot. not really a big real to remove this if we
	// need to reduce writes.
	cs.commit()
}

// PublishRoutes publishes routes that we know about to our trusted peers. See
// implementation of [RoutePublisher] for more details on publication strategy.
func (cs *ClusterState) PublishRoutes(ctx context.Context, msg Messenger) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	const (
		maxPeers  = 5
		maxRoutes = 5000
	)

	cs.publisher.Publish(func(to RDT, r Routes) error {
		fp, ok := cs.fingerprints[to]
		if !ok {
			return errors.New("skipped publishing to untrusted peer")
		}

		// this entry should always be present. cs.trusted and cs.fingerprints
		// are only written to within the same critical section.
		p, ok := cs.trusted[fp]
		if !ok {
			return errors.New("skipped publishing to untrusted peer")
		}

		// we might trust a peer that we don't know the address of yet.
		addr, ok := cs.addresses[fp]
		if !ok {
			return errors.New("skipped publishing to undiscovered peer")
		}

		// unlock for the duration of the send. this is safe, since all of the
		// data that is passed to the send function is owned.
		cs.lock.Unlock()
		defer cs.lock.Lock()
		return msg.Trusted(ctx, p.RDT, addr, p.Cert, "routes", r)
	}, maxPeers, maxRoutes)

	// we don't commit on route publishes, since we don't keep track of which
	// routes we've sent to our peers in the state.
}

// Authenticate checks that the given [Auth] message is valid and proves
// knowledge of the shared secret. If this check is passed, we allow mutation of
// this [ClusterState] via future calls to [ClusterState.Trusted] with the same
// certificate.
//
// An error is returned if the message's HMAC is found to not prove knowledge of
// the shared secret.
func (cs *ClusterState) Authenticate(auth Auth, cert []byte) error {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	fp := CalculateFP(cert)

	expectedHMAC := CalculateHMAC(auth.RDT, fp, cs.config.Secret)
	if !hmac.Equal(expectedHMAC, auth.HMAC) {
		return errors.New("received invalid HMAC from peer")
	}

	if _, ok := cs.trusted[fp]; ok {
		if cs.trusted[fp].RDT != auth.RDT {
			return fmt.Errorf("peer with rdt %v is using a new TLS certificate", auth.RDT)
		}
	} else {
		cs.trusted[fp] = peer{
			RDT:  auth.RDT,
			Cert: cert,
		}
	}

	cs.fingerprints[auth.RDT] = fp

	// if we have discovered the route to this peer, we should record an
	// authoritative route to it. this ensures that we send the route from our
	// local node to this peer when we publish
	if addr, ok := cs.addresses[fp]; ok {
		cs.publisher.AddAuthoritativeRoute(auth.RDT, addr)
	}

	cs.commit()

	return nil
}

// Trusted checks if the given certificate is trusted and maps to a known RDT.
// If it is, then a [PeerHandle] is returned that can be used to modify the state
// of the cluster on this peer's behalf.
//
// An error is returned if the certificate isn't trusted.
func (cs *ClusterState) Trusted(cert []byte) (*PeerHandle, error) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	fp := CalculateFP(cert)

	p, ok := cs.trusted[fp]
	if !ok {
		return nil, errors.New("given TLS certificate is not associated with a trusted RDT")
	}

	return &PeerHandle{
		lock: &cs.lock,
		cs:   cs,
		peer: p.RDT,
	}, nil
}

type PeerHandle struct {
	lock *sync.Mutex
	cs   *ClusterState
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
	h.lock.Lock()
	defer h.lock.Unlock()

	if err := h.cs.devices.AddQueries(h.peer, unknown.Devices); err != nil {
		return err
	}

	h.cs.commit()

	return nil
}

// AddRoutes updates the state of the cluster with the given routes.
func (h *PeerHandle) AddRoutes(routes Routes) (int, int, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	// if this peer is sending us routes that include these devices, then we
	// know that they must have identifying information for those devices.
	h.cs.devices.AddSources(h.peer, routes.Devices)

	added, total, err := h.cs.publisher.AddRoutes(h.peer, routes, h.cs.devices.Identified)
	if err != nil {
		return 0, 0, err
	}

	h.cs.commit()

	return added, total, nil
}

// AddDevices records the given device identities. All new device identities are
// recorded. For any devices that we are already aware of, we check that our
// view of the device's identity is consistent with the new data.
func (h *PeerHandle) AddDevices(devices Devices) error {
	h.lock.Lock()
	defer h.lock.Unlock()

	for _, id := range devices.Devices {
		if current, ok := h.cs.devices.Lookup(id.RDT); ok {
			if current != id {
				return errors.New("got inconsistent device identity")
			}
		}
	}

	for _, id := range devices.Devices {
		h.cs.devices.Save(id)
	}

	// since we got new device info, we have to recalculate which routes are
	// valid to send to our peers
	h.cs.publisher.VerifyRoutes(h.cs.devices.Identified)

	h.cs.commit()

	return nil
}

func newDeviceIDs(self Identity) deviceIDs {
	return deviceIDs{
		Identities: map[RDT]Identity{
			self.RDT: self,
		},
		Sources: make(map[RDT]map[RDT]struct{}),
		Queries: make(map[RDT]map[RDT]struct{}),
	}
}

// deviceIDs keeps track of which devices we and our peers know about.
//
// TODO: JSON serialization of this data structure is really wasteful
type deviceIDs struct {
	Identities map[RDT]Identity         `json:"identities"`
	Sources    map[RDT]map[RDT]struct{} `json:"sources"`
	Queries    map[RDT]map[RDT]struct{} `json:"queries"`
}

// Identified returns true if this local node has identifying information for
// the peer with the given [RDT].
func (d *deviceIDs) Identified(rdt RDT) bool {
	_, ok := d.Identities[rdt]
	return ok
}

// Save saves the given device identity.
func (d *deviceIDs) Save(id Identity) {
	d.Identities[id.RDT] = id
}

// Lookup returns the [Identity] of the device with the given [RDT], if we have
// it.
func (d *deviceIDs) Lookup(rdt RDT) (Identity, bool) {
	id, ok := d.Identities[rdt]
	return id, ok
}

// AddQueries records queries for device information from the given [RDT].
func (d *deviceIDs) AddQueries(from RDT, queries []RDT) error {
	for _, rdt := range queries {
		if !d.Identified(rdt) {
			return fmt.Errorf("unknown device: %s", rdt)
		}
	}

	if d.Queries[from] == nil {
		d.Queries[from] = make(map[RDT]struct{})
	}

	for _, rdt := range queries {
		d.Queries[from][rdt] = struct{}{}
	}

	return nil
}

// QueryResponses returns the set of device identities that the given peer has
// requested.
func (d *deviceIDs) QueryResponses(from RDT) []Identity {
	ids := make([]Identity, 0, len(d.Queries[from]))
	for rdt := range d.Queries[from] {
		id, ok := d.Lookup(rdt)
		if !ok {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// AckQueries removes the given set of devices from the given RDT's queries.
// Should be called once we respond to a device's query.
func (d *deviceIDs) AckQueries(from RDT, ids []Identity) {
	for _, id := range ids {
		delete(d.Queries[from], id.RDT)
	}
}

// AddSources records that the given RDT knows about the given set of devices.
// We should be able to query this peer for that device information, if we don't
// have it.
func (d *deviceIDs) AddSources(source RDT, devices []RDT) {
	if d.Sources[source] == nil {
		d.Sources[source] = make(map[RDT]struct{})
	}

	for _, rdt := range devices {
		d.Sources[source][rdt] = struct{}{}
	}
}

// UnknownKnownBy returns the list of devices that we don't have identifying
// info for, but the given peer does.
func (d *deviceIDs) UnknownKnownBy(source RDT) []RDT {
	var unknown []RDT
	for rdt := range d.Sources[source] {
		if d.Identified(rdt) {
			continue
		}

		unknown = append(unknown, rdt)
	}
	return unknown
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
