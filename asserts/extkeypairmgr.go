// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package asserts

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"golang.org/x/crypto/openpgp/packet"

	"github.com/snapcore/snapd/strutil"
)

// ExternalKeypairManager is key pair manager implemented via an external program interface.
// TODO: points to interface docs
type ExternalKeypairManager struct {
	keyMgrPath string
	cache      map[string]PrivateKey
}

// NewExternalKeypairManager creates a new ExternalKeypairManager using the program at keyMgrPath.
func NewExternalKeypairManager(keyMgrPath string) (*ExternalKeypairManager, error) {
	em := &ExternalKeypairManager{
		keyMgrPath: keyMgrPath,
		cache:      make(map[string]PrivateKey),
	}
	if err := em.checkFeatures(); err != nil {
		return nil, err
	}
	return em, nil
}

func (em *ExternalKeypairManager) keyMgr(op string, args []string, in []byte, out interface{}) error {
	args = append([]string{op}, args...)
	cmd := exec.Command(em.keyMgrPath, args...)
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer

	if len(in) != 0 {
		cmd.Stdin = bytes.NewBuffer(in)
	}
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("external keypair manager %q %v failed: %v (%q)", em.keyMgrPath, args, err, errBuf.Bytes())

	}
	switch o := out.(type) {
	case *[]byte:
		*o = outBuf.Bytes()
	default:
		err := json.Unmarshal(outBuf.Bytes(), out)
		if err != nil {
			return fmt.Errorf("cannot decode external keypair manager %q %v output: %v", em.keyMgrPath, args, err)
		}
	}
	return nil
}

func (em *ExternalKeypairManager) checkFeatures() error {
	var feats struct {
		Signing    []string `json:"signing"`
		PublicKeys []string `json:"public-keys"`
	}
	if err := em.keyMgr("features", nil, nil, &feats); err != nil {
		return err
	}
	if !strutil.ListContains(feats.Signing, "RSA-PKCS") {
		return fmt.Errorf("external keypair manager %q unexpectedly does not support RSA-PKCS", em.keyMgrPath)
	}
	if !strutil.ListContains(feats.PublicKeys, "DER") {
		return fmt.Errorf("external keypair manager %q unexpectedly does not support public key DER output format", em.keyMgrPath)
	}
	return nil
}

func (em *ExternalKeypairManager) Put(privKey PrivateKey) error {
	return fmt.Errorf("cannot import private key into external keypair manager")
}

func (em *ExternalKeypairManager) Get(keyID string) (PrivateKey, error) {
	pk, ok := em.cache[keyID]
	if !ok {
		// XXX try to load all keys first
		return nil, fmt.Errorf("cannot find external key with id %q", keyID)
	}
	return pk, nil
}

func (em *ExternalKeypairManager) GetByName(keyName string) (PrivateKey, error) {
	var k []byte
	err := em.keyMgr("get-public-key", []string{"-f", "DER", "-k", keyName}, nil, &k)
	if err != nil {
		return nil, fmt.Errorf("cannot find external key: %v", err)
	}
	pk, err := x509.ParsePKCS1PublicKey(k)
	if err != nil {
		return nil, fmt.Errorf("cannot decode external key %q: %v", keyName, err)
	}
	signer := packet.NewSignerPrivateKey(v1FixedTimestamp, &extSigner{
		keyName:  keyName,
		pubKey:   pk,
		signWith: em.signWith,
	})
	signk := openpgpPrivateKey{privk: signer}
	extKey := &extPGPPrivateKey{
		pubKey:     RSAPublicKey(pk),
		from:       fmt.Sprintf("external keypair manager %q", em.keyMgrPath),
		externalID: keyName,
		bitLen:     pk.N.BitLen(),
		doSign:     signk.sign,
	}
	// XXX cache by name too
	em.cache[extKey.PublicKey().ID()] = extKey
	return extKey, nil
}

var digestInfoSHA512Prefix = []byte{0x30, 0x51, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x03, 0x05, 0x00, 0x04, 0x40}

func (em *ExternalKeypairManager) signWith(keyName string, digest []byte) (signature []byte, err error) {
	// wrap the digest into the needed DigestInfo, the RSA-PKCS
	// mechanism or equivalent is expected not to do this on its
	// own
	toSign := &bytes.Buffer{}
	toSign.Write(digestInfoSHA512Prefix)
	toSign.Write(digest)

	err = em.keyMgr("sign", []string{"-m", "RSA-PKCS", "-k", keyName}, toSign.Bytes(), &signature)
	if err != nil {
		return nil, err
	}
	return signature, nil
}

type extSigner struct {
	keyName  string
	pubKey   crypto.PublicKey
	signWith func(keyName string, digest []byte) (signature []byte, err error)
}

func (es *extSigner) Public() crypto.PublicKey {
	return es.pubKey
}

func (es *extSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	if opts.HashFunc() != crypto.SHA512 {
		return nil, fmt.Errorf("unexpected pgp signature digest")
	}

	return es.signWith(es.keyName, digest)
}
