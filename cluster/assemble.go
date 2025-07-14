package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"time"

	"github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
)

// AssembleOpts carries all of the options the caller can provide to [Assemble].
type AssembleOpts struct {
	ListenIP     net.IP
	ListenPort   int
	Logger       logger.Logger
	Secret       string
	RDTOverride  string
	ExpectedSize int
}

// Assemble starts an assembly session. Without a known number of expected
// devices assembly will run until the given [context.Context] is cancelled.
//
// All of the routes that are found and associated with identified devices are
// returned. We'll probably have to return some more information here in the
// future.
//
// Eventually, this function will use the accepted [state.State] to resume a
// stopped assemble session.
//
// TODO: get rid of this entrypoint
func Assemble(st *state.State, ctx context.Context, discover assemblestate.Discoverer, opts AssembleOpts) (assemblestate.Routes, error) {
	// TODO: pick how we're going to generate RDTs
	rdt := assemblestate.DeviceToken(opts.RDTOverride)
	if rdt == "" {
		return assemblestate.Routes{}, errors.New("rdt must be provided")
	}

	log := opts.Logger
	if log == nil {
		log = logger.NullLogger
	}

	cert, key, err := createCertAndKey(opts.ListenIP)
	if err != nil {
		return assemblestate.Routes{}, err
	}

	config := assemblestate.AssembleConfig{
		Secret:       opts.Secret,
		RDT:          rdt,
		IP:           opts.ListenIP,
		Port:         opts.ListenPort,
		TLSCert:      cert,
		TLSKey:       key,
		ExpectedSize: opts.ExpectedSize,
	}

	commit := func(s assemblestate.AssembleSession) {
		// st.Lock()
		// defer st.Unlock()
		// st.Set("assemble-session", s)
	}

	// TODO: once this is incorporated into a change, this will be how we resume
	// an assemble session
	session := assemblestate.AssembleSession{}

	as, err := assemblestate.NewAssembleState(config, session, func(self assemblestate.DeviceToken) (assemblestate.RouteSelector, error) {
		return assemblestate.NewPrioritySelector(self, nil), nil
	}, log, commit)
	if err != nil {
		return assemblestate.Routes{}, err
	}

	transport := assemblestate.NewHTTPTransport(log)
	return as.Run(ctx, transport, discover)
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
