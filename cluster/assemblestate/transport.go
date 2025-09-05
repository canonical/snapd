package assemblestate

import (
	"context"
	"crypto/tls"
	"net"
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
