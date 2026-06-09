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
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os/exec"

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
	impl       *extKeypairMgrImpl
}

// NewExternalKeypairManager creates a new ExternalKeypairManager using the program at keyMgrPath.
func NewExternalKeypairManager(keyMgrPath string) (*ExternalKeypairManager, error) {
	em := &ExternalKeypairManager{keyMgrPath: keyMgrPath}
	impl, err := newExtKeypairMgrImpl(&externalKeypairMgrBackend{manager: em}, extKeypairMgrConfig{
		signingWith: fmt.Sprintf("external keypair manager %q", keyMgrPath),
		keyStore:    "external keypair manager",
	})
	if err != nil {
		return nil, err
	}
	em.impl = impl
	return em, nil
}

func (em *ExternalKeypairManager) keyMgr(op string, args []string, in []byte, out any) error {
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
		if err := json.Unmarshal(outBuf.Bytes(), out); err != nil {
			return fmt.Errorf("cannot decode external keypair manager %q %v output: %v", em.keyMgrPath, args, err)
		}
	}
	return nil
}

func (em *ExternalKeypairManager) checkFeatures() (extKeypairMgrSigning, error) {
	var feats struct {
		Signing    []string `json:"signing"`
		PublicKeys []string `json:"public-keys"`
	}
	if err := em.keyMgr("features", nil, nil, &feats); err != nil {
		return "", err
	}
	var signing extKeypairMgrSigning
	if strutil.ListContains(feats.Signing, "RSA-PKCS") {
		signing = extKeypairMgrSigningRSAPKCS
	} else if strutil.ListContains(feats.Signing, "OPENPGP") {
		signing = extKeypairMgrSigningOpenPGP
	} else {
		return "", fmt.Errorf("external keypair manager %q missing support for RSA-PKCS or OPENPGP signing", em.keyMgrPath)
	}
	if !strutil.ListContains(feats.PublicKeys, "DER") {
		return "", fmt.Errorf("external keypair manager %q missing support for public key DER output format", em.keyMgrPath)
	}
	return signing, nil
}

func (em *ExternalKeypairManager) keyNames() ([]string, error) {
	var knames struct {
		Names []string `json:"key-names"`
	}
	if err := em.keyMgr("key-names", nil, nil, &knames); err != nil {
		return nil, fmt.Errorf("cannot get all external keypair manager key names: %v", err)
	}
	return knames.Names, nil
}

func (em *ExternalKeypairManager) findByName(name string) (PublicKey, error) {
	var k []byte
	err := em.keyMgr("get-public-key", []string{"-f", "DER", "-k", name}, nil, &k)
	if err != nil {
		return nil, &keyNotFoundError{msg: fmt.Sprintf("cannot find external key pair: %v", err)}
	}
	pubk, err := x509.ParsePKIXPublicKey(k)
	if err != nil {
		return nil, fmt.Errorf("cannot decode external key %q: %v", name, err)
	}
	rsaPub, ok := pubk.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected RSA public key, got instead: %T", pubk)
	}
	return RSAPublicKey(rsaPub), nil
}

func (em *ExternalKeypairManager) Export(keyName string) ([]byte, error) {
	return em.impl.Export(keyName)
}

func (em *ExternalKeypairManager) GetByName(keyName string) (PrivateKey, error) {
	return em.impl.GetByName(keyName)
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

func (em *ExternalKeypairManager) Get(keyID string) (PrivateKey, error) {
	return em.impl.Get(keyID)
}

func (em *ExternalKeypairManager) List() ([]ExternalKeyInfo, error) {
	return em.impl.List()
}

// externalKeypairMgrBackend implements extKeypairMgrBackend and
// extKeypairMgrByNameLookupBackend as a thin wrapper around
// ExternalKeypairManager, allowing it to be used as the backend for an
// extKeypairMgrImpl.
type externalKeypairMgrBackend struct {
	manager *ExternalKeypairManager
}

// expected interfaces are implemented
var (
	_ extKeypairMgrBackend             = (*externalKeypairMgrBackend)(nil)
	_ extKeypairMgrByNameLookupBackend = (*externalKeypairMgrBackend)(nil)
)

func (s *externalKeypairMgrBackend) CheckFeatures() (extKeypairMgrSigning, error) {
	return s.manager.checkFeatures()
}

func (s *externalKeypairMgrBackend) LoadByName(name string) (*extKeypairMgrLoadedKey, error) {
	pubKey, err := s.manager.findByName(name)
	if err != nil {
		return nil, err
	}
	return &extKeypairMgrLoadedKey{
		name:      name,
		keyHandle: name,
		pubKey:    pubKey,
	}, nil
}

func (s *externalKeypairMgrBackend) Visit(consider func(loaded *extKeypairMgrLoadedKey) error) error {
	names, err := s.manager.keyNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		loaded, err := s.LoadByName(name)
		if err != nil {
			return err
		}
		if err := consider(loaded); err != nil {
			return err
		}
	}
	return nil
}

func (s *externalKeypairMgrBackend) RSAPKCSSign(keyHandle string, prepared []byte) ([]byte, error) {
	var signature []byte
	err := s.manager.keyMgr("sign", []string{"-m", "RSA-PKCS", "-k", keyHandle}, prepared, &signature)
	if err != nil {
		return nil, err
	}
	return signature, nil
}

func (s *externalKeypairMgrBackend) Sign(keyHandle string, content []byte) ([]byte, error) {
	var signature []byte
	err := s.manager.keyMgr("sign", []string{"-m", "OPENPGP", "-k", keyHandle}, content, &signature)
	if err != nil {
		return nil, err
	}
	return signature, nil
}
