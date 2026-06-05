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
	impl *extKeypairMgrImpl
}

// NewExternalKeypairManager creates a new ExternalKeypairManager using the program at keyMgrPath.
func NewExternalKeypairManager(keyMgrPath string) (*ExternalKeypairManager, error) {
	impl, err := newExtKeypairMgrImpl(&externalCmdKeypairMgrBackend{keyMgrPath: keyMgrPath}, ExtKeypairMgrConfig{
		SigningWith: fmt.Sprintf("external keypair manager %q", keyMgrPath),
		KeyStore:    "external keypair manager",
	})
	if err != nil {
		return nil, err
	}
	return &ExternalKeypairManager{impl: impl}, nil
}

// NewExternalKeypairManagerWithBackend creates a new ExternalKeypairManager using backend.
func NewExternalKeypairManagerWithBackend(backend ExtKeypairMgrBackend, config ExtKeypairMgrConfig) (*ExternalKeypairManager, error) {
	impl, err := newExtKeypairMgrImpl(backend, config)
	if err != nil {
		return nil, err
	}
	return &ExternalKeypairManager{impl: impl}, nil
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

// externalCmdKeypairMgrBackend implements extKeypairMgrBackend and
// extKeypairMgrByNameLookupBackend by invoking the configured external
// keypair manager command.
type externalCmdKeypairMgrBackend struct {
	keyMgrPath string
}

// expected interfaces are implemented
var (
	_ ExtKeypairMgrBackend             = (*externalCmdKeypairMgrBackend)(nil)
	_ ExtKeypairMgrByNameLookupBackend = (*externalCmdKeypairMgrBackend)(nil)
)

func (s *externalCmdKeypairMgrBackend) keyMgr(op string, args []string, in []byte, out any) error {
	args = append([]string{op}, args...)
	cmd := exec.Command(s.keyMgrPath, args...)
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer

	if len(in) != 0 {
		cmd.Stdin = bytes.NewBuffer(in)
	}
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("external keypair manager %q %v failed: %v (%q)", s.keyMgrPath, args, err, errBuf.Bytes())

	}
	switch o := out.(type) {
	case *[]byte:
		*o = outBuf.Bytes()
	default:
		if err := json.Unmarshal(outBuf.Bytes(), out); err != nil {
			return fmt.Errorf("cannot decode external keypair manager %q %v output: %v", s.keyMgrPath, args, err)
		}
	}
	return nil
}

func (s *externalCmdKeypairMgrBackend) CheckFeatures() (ExtKeypairMgrSigning, error) {
	var feats struct {
		Signing    []string `json:"signing"`
		PublicKeys []string `json:"public-keys"`
	}
	if err := s.keyMgr("features", nil, nil, &feats); err != nil {
		return "", err
	}
	var signing ExtKeypairMgrSigning
	if strutil.ListContains(feats.Signing, "RSA-PKCS") {
		signing = ExtKeypairMgrSigningRSAPKCS
	} else if strutil.ListContains(feats.Signing, "OPENPGP") {
		signing = ExtKeypairMgrSigningOpenPGP
	} else {
		return "", fmt.Errorf("external keypair manager %q missing support for RSA-PKCS or OPENPGP signing", s.keyMgrPath)
	}
	if !strutil.ListContains(feats.PublicKeys, "DER") {
		return "", fmt.Errorf("external keypair manager %q missing support for public key DER output format", s.keyMgrPath)
	}
	return signing, nil
}

func (s *externalCmdKeypairMgrBackend) keyNames() ([]string, error) {
	var knames struct {
		Names []string `json:"key-names"`
	}
	if err := s.keyMgr("key-names", nil, nil, &knames); err != nil {
		return nil, fmt.Errorf("cannot get all external keypair manager key names: %v", err)
	}
	return knames.Names, nil
}

func (s *externalCmdKeypairMgrBackend) findByName(name string) (PublicKey, error) {
	var k []byte
	err := s.keyMgr("get-public-key", []string{"-f", "DER", "-k", name}, nil, &k)
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

func (s *externalCmdKeypairMgrBackend) LoadByName(name string) (*ExtKeypairMgrLoadedKey, error) {
	pubKey, err := s.findByName(name)
	if err != nil {
		return nil, err
	}
	return &ExtKeypairMgrLoadedKey{
		Name:      name,
		KeyHandle: name,
		PublicKey: pubKey,
	}, nil
}

func (s *externalCmdKeypairMgrBackend) Visit(consider func(loaded *ExtKeypairMgrLoadedKey) error) error {
	names, err := s.keyNames()
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

func (s *externalCmdKeypairMgrBackend) RSAPKCSSign(keyHandle string, prepared []byte) ([]byte, error) {
	var signature []byte
	err := s.keyMgr("sign", []string{"-m", "RSA-PKCS", "-k", keyHandle}, prepared, &signature)
	if err != nil {
		return nil, err
	}
	return signature, nil
}

func (s *externalCmdKeypairMgrBackend) Sign(keyHandle string, content []byte) ([]byte, error) {
	var signature []byte
	err := s.keyMgr("sign", []string{"-m", "OPENPGP", "-k", keyHandle}, content, &signature)
	if err != nil {
		return nil, err
	}
	return signature, nil
}
