// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package tool

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/crypto/openpgp/packet"

	"github.com/snapcore/snapd/asserts"
)

type gpgKeypairManager struct {
	homedir string
}

func (gkm *gpgKeypairManager) gpg(input []byte, args ...string) ([]byte, error) {
	general := []string{"-q"}
	if gkm.homedir != "" {
		general = append([]string{"--homedir", gkm.homedir}, general...)
	}
	allArgs := append(general, args...)

	cmd := exec.Command("gpg", allArgs...)
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer

	if len(input) != 0 {
		cmd.Stdin = bytes.NewBuffer(input)
	}

	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gpg %q failed: %v (%q)", strings.Join(args, " "), err, errBuf.Bytes())
	}

	return outBuf.Bytes(), nil
}

// NewGPGKeypairManager creates a new key pair manager backed by a local GnuPG setup using the given GPG homedir,
// and asking GPG to fallback "~/.gnupg" to default if
// empty. Importing keys through the keypair manager interface is not
// supported. Main purpose is allowing signing using keys from a GPG setup.
func NewGPGKeypairManager(homedir string) asserts.KeypairManager {
	return &gpgKeypairManager{
		homedir: homedir,
	}
}

func (gkm *gpgKeypairManager) Put(authorityID string, privKey asserts.PrivateKey) error {
	// NOTE: we don't need this initially at least and this keypair mgr is not for general arbitrary usage
	return fmt.Errorf("importing key in a GPG keypair manager is not supported")
}

func (gkm *gpgKeypairManager) Get(authorityID, keyID string) (asserts.PrivateKey, error) {
	out, err := gkm.gpg(nil, "--batch", "--export", "--export-options", "export-minimal,export-clean,no-export-attributes", keyID)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no matching key pair found")
	}

	rd := packet.NewReader(bytes.NewBuffer(out))
	pubk, err := rd.Next()
	if err != nil {
		return nil, fmt.Errorf("cannot read public part of key pair: %v", err)
	}

	pubKey, ok := pubk.(*packet.PublicKey)
	if !ok {
		return nil, fmt.Errorf("cannot read public part of key pair: got %T", pubk)
	}

	sign := func(content []byte, cfg *packet.Config) (*packet.Signature, error) {
		out, err := gkm.gpg(content, "--personal-digest-preferences", "SHA512", "--default-key", keyID, "--detach-sign")
		if err != nil {
			return nil, err
		}

		rd := packet.NewReader(bytes.NewBuffer(out))
		sigpkt, err := rd.Next()
		if err != nil {
			return nil, fmt.Errorf("cannot parse gpg produced signature: %v", err)
		}

		sig, ok := sigpkt.(*packet.Signature)
		if !ok {
			return nil, fmt.Errorf("cannot parse gpg produced signature: got %T", sigpkt)
		}

		return sig, nil

	}

	return asserts.SealedOpenPGPPrivateKey(pubKey, sign), nil
}
