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
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"golang.org/x/crypto/openpgp/packet"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

type ExternalKeyInfo struct {
	Name string
	ID   string
}

// ExternalKeypairManager is key pair manager implemented via an external program interface.
// TODO: points to interface docs
type ExternalKeypairManager struct {
	keyMgrPath string
	nameToID   map[string]string
	cache      map[string]*cachedExtKey
}

// NewExternalKeypairManager creates a new ExternalKeypairManager using the program at keyMgrPath.
func NewExternalKeypairManager(keyMgrPath string) (*ExternalKeypairManager, error) {
	em := &ExternalKeypairManager{
		keyMgrPath: keyMgrPath,
		nameToID:   make(map[string]string),
		cache:      make(map[string]*cachedExtKey),
	}
	mylog.Check(em.checkFeatures())

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
	mylog.Check(cmd.Run())

	switch o := out.(type) {
	case *[]byte:
		*o = outBuf.Bytes()
	default:
		mylog.Check(json.Unmarshal(outBuf.Bytes(), out))

	}
	return nil
}

func (em *ExternalKeypairManager) checkFeatures() error {
	var feats struct {
		Signing    []string `json:"signing"`
		PublicKeys []string `json:"public-keys"`
	}
	mylog.Check(em.keyMgr("features", nil, nil, &feats))

	if !strutil.ListContains(feats.Signing, "RSA-PKCS") {
		return fmt.Errorf("external keypair manager %q missing support for RSA-PKCS signing", em.keyMgrPath)
	}
	if !strutil.ListContains(feats.PublicKeys, "DER") {
		return fmt.Errorf("external keypair manager %q missing support for public key DER output format", em.keyMgrPath)
	}
	return nil
}

func (em *ExternalKeypairManager) keyNames() ([]string, error) {
	var knames struct {
		Names []string `json:"key-names"`
	}
	mylog.Check(em.keyMgr("key-names", nil, nil, &knames))

	return knames.Names, nil
}

func (em *ExternalKeypairManager) findByName(name string) (PublicKey, *rsa.PublicKey, error) {
	var k []byte
	mylog.Check(em.keyMgr("get-public-key", []string{"-f", "DER", "-k", name}, nil, &k))

	pubk := mylog.Check2(x509.ParsePKIXPublicKey(k))

	rsaPub, ok := pubk.(*rsa.PublicKey)
	if !ok {
		return nil, nil, fmt.Errorf("expected RSA public key, got instead: %T", pubk)
	}
	pubKey := RSAPublicKey(rsaPub)
	return pubKey, rsaPub, nil
}

func (em *ExternalKeypairManager) Export(keyName string) ([]byte, error) {
	pubKey, _ := mylog.Check3(em.findByName(keyName))

	return EncodePublicKey(pubKey)
}

func (em *ExternalKeypairManager) loadKey(name string) (*cachedExtKey, error) {
	id, ok := em.nameToID[name]
	if ok {
		return em.cache[id], nil
	}
	pubKey, rsaPub := mylog.Check3(em.findByName(name))

	id = pubKey.ID()
	em.nameToID[name] = id
	cachedKey := &cachedExtKey{
		pubKey: pubKey,
		signer: &extSigner{
			keyName: name,
			rsaPub:  rsaPub,
			// signWith is filled later
		},
	}
	em.cache[id] = cachedKey
	return cachedKey, nil
}

func (em *ExternalKeypairManager) privateKey(cachedKey *cachedExtKey) PrivateKey {
	if cachedKey.privKey == nil {
		extSigner := cachedKey.signer
		// fill signWith
		extSigner.signWith = em.signWith
		signer := packet.NewSignerPrivateKey(v1FixedTimestamp, extSigner)
		signk := openpgpPrivateKey{privk: signer}
		extKey := &extPGPPrivateKey{
			pubKey:     cachedKey.pubKey,
			from:       fmt.Sprintf("external keypair manager %q", em.keyMgrPath),
			externalID: extSigner.keyName,
			bitLen:     extSigner.rsaPub.N.BitLen(),
			doSign:     signk.sign,
		}
		cachedKey.privKey = extKey
	}
	return cachedKey.privKey
}

func (em *ExternalKeypairManager) GetByName(keyName string) (PrivateKey, error) {
	cachedKey := mylog.Check2(em.loadKey(keyName))

	return em.privateKey(cachedKey), nil
}

// ExternalUnsupportedOpError represents the error situation of operations
// that are not supported/mediated via ExternalKeypairManager.
type ExternalUnsupportedOpError struct {
	msg string
}

func (euoe *ExternalUnsupportedOpError) Error() string {
	return euoe.msg
}

func (em *ExternalKeypairManager) Put(privKey PrivateKey) error {
	return &ExternalUnsupportedOpError{"cannot import private key into external keypair manager"}
}

func (em *ExternalKeypairManager) Delete(keyID string) error {
	return &ExternalUnsupportedOpError{"no support to delete external keypair manager keys"}
}

func (em *ExternalKeypairManager) DeleteByName(keyName string) error {
	return &ExternalUnsupportedOpError{"no support to delete external keypair manager keys"}
}

func (em *ExternalKeypairManager) Generate(keyName string) error {
	return &ExternalUnsupportedOpError{"no support to mediate generating an external keypair manager key"}
}

func (em *ExternalKeypairManager) loadAllKeys() ([]string, error) {
	names := mylog.Check2(em.keyNames())

	for _, name := range names {
		mylog.Check2(em.loadKey(name))
	}
	return names, nil
}

func (em *ExternalKeypairManager) Get(keyID string) (PrivateKey, error) {
	cachedKey, ok := em.cache[keyID]
	if !ok {
		mylog.Check2(
			// try to load all keys
			em.loadAllKeys())

		cachedKey, ok = em.cache[keyID]
		if !ok {
			return nil, &keyNotFoundError{msg: "cannot find external key pair"}
		}
	}
	return em.privateKey(cachedKey), nil
}

func (em *ExternalKeypairManager) List() ([]ExternalKeyInfo, error) {
	names := mylog.Check2(em.loadAllKeys())

	res := make([]ExternalKeyInfo, len(names))
	for i, name := range names {
		res[i].Name = name
		res[i].ID = em.cache[em.nameToID[name]].pubKey.ID()
	}
	return res, nil
}

// see https://datatracker.ietf.org/doc/html/rfc2313 and more recently
// and more precisely about SHA-512:
// https://datatracker.ietf.org/doc/html/rfc3447#section-9.2 Notes 1.
var digestInfoSHA512Prefix = []byte{0x30, 0x51, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x03, 0x05, 0x00, 0x04, 0x40}

func (em *ExternalKeypairManager) signWith(keyName string, digest []byte) (signature []byte, err error) {
	// wrap the digest into the needed DigestInfo, the RSA-PKCS
	// mechanism or equivalent is expected not to do this on its
	// own
	toSign := &bytes.Buffer{}
	toSign.Write(digestInfoSHA512Prefix)
	toSign.Write(digest)
	mylog.Check(em.keyMgr("sign", []string{"-m", "RSA-PKCS", "-k", keyName}, toSign.Bytes(), &signature))

	return signature, nil
}

type cachedExtKey struct {
	pubKey  PublicKey
	signer  *extSigner
	privKey PrivateKey
}

type extSigner struct {
	keyName  string
	rsaPub   *rsa.PublicKey
	signWith func(keyName string, digest []byte) (signature []byte, err error)
}

func (es *extSigner) Public() crypto.PublicKey {
	return es.rsaPub
}

func (es *extSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	if opts.HashFunc() != crypto.SHA512 {
		return nil, fmt.Errorf("unexpected pgp signature digest")
	}

	return es.signWith(es.keyName, digest)
}
