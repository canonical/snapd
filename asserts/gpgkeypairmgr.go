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

package asserts

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

func ensureGPGHomeDirectory(homedir string) (string, error) {
	real, err := osutil.RealUser()
	if err != nil {
		return "", err
	}

	uid, err := strconv.Atoi(real.Uid)
	if err != nil {
		return "", err
	}

	gid, err := strconv.Atoi(real.Gid)
	if err != nil {
		return "", err
	}

	if homedir == "" {
		homedir = os.Getenv("SNAP_GNUPGHOME")
	}
	if homedir == "" {
		homedir = filepath.Join(real.HomeDir, ".snap")
	}

	if err := osutil.MkdirAllChown(homedir, 0700, uid, gid); err != nil {
		return "", err
	}
	return homedir, nil
}

func runGPGImpl(homedir string, input []byte, args ...string) ([]byte, error) {
	homedir, err := ensureGPGHomeDirectory(homedir)
	if err != nil {
		return nil, err
	}

	general := []string{"--homedir", homedir, "-q", "--no-auto-check-trustdb"}
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
		return nil, fmt.Errorf("gpg %s failed: %v (%q)", strings.Join(args, " "), err, errBuf.Bytes())
	}

	return outBuf.Bytes(), nil
}

var runGPG = runGPGImpl

// A key pair manager backed by a local GnuPG setup.
type GPGKeypairManager struct {
	homedir string
}

func (gkm *GPGKeypairManager) gpg(input []byte, args ...string) ([]byte, error) {
	return runGPG(gkm.homedir, input, args...)
}

// NewGPGKeypairManager creates a new key pair manager backed by a local GnuPG setup
// using the given GPG homedir, and asking GPG to fallback "~/.gnupg"
// to default if empty.
// Importing keys through the keypair manager interface is not
// suppored.
// Main purpose is allowing signing using keys from a GPG setup.
func NewGPGKeypairManager(homedir string) *GPGKeypairManager {
	return &GPGKeypairManager{
		homedir: homedir,
	}
}

func (gkm *GPGKeypairManager) retrieve(fpr string) (PrivateKey, error) {
	out, err := gkm.gpg(nil, "--batch", "--export", "--export-options", "export-minimal,export-clean,no-export-attributes", "0x"+fpr)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("cannot retrieve key with fingerprint %q in GPG keyring", fpr)
	}

	pubKeyBuf := bytes.NewBuffer(out)
	privKey, err := newExtPGPPrivateKey(pubKeyBuf, "GPG", func(content []byte) ([]byte, error) {
		return gkm.sign(fpr, content)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot load GPG public key with fingerprint %q: %v", fpr, err)
	}
	gotFingerprint := privKey.fingerprint()
	if gotFingerprint != fpr {
		return nil, fmt.Errorf("got wrong public key from GPG, expected fingerprint %q: %s", fpr, gotFingerprint)
	}
	return privKey, nil
}

// Walk iterates over all the RSA private keys in the local GPG setup calling the provided callback until this returns an error
// TODO: revisit exposing this
func (gkm *GPGKeypairManager) Walk(consider func(privk PrivateKey, fingerprint string, uid string) error) error {
	// see GPG source doc/DETAILS
	out, err := gkm.gpg(nil, "--batch", "--list-secret-keys", "--fingerprint", "--with-colons")
	if err != nil {
		return err
	}
	lines := strings.Split(string(out), "\n")
	n := len(lines)
	if n > 0 && lines[n-1] == "" {
		n--
	}
	if n == 0 {
		return nil
	}
	lines = lines[:n]
	for j := 0; j < n; j++ {
		line := lines[j]
		if !strings.HasPrefix(line, "sec:") {
			continue
		}
		secFields := strings.Split(line, ":")
		if len(secFields) < 5 {
			continue
		}
		if secFields[3] != "1" { // not RSA
			continue
		}
		keyID := secFields[4]
		uid := ""
		if len(secFields) >= 10 {
			uid = secFields[9]
		}
		if j+1 >= n || !strings.HasPrefix(lines[j+1], "fpr:") {
			continue
		}
		fprFields := strings.Split(lines[j+1], ":")
		if len(fprFields) < 10 {
			continue
		}
		fpr := fprFields[9]
		if !strings.HasSuffix(fpr, keyID) {
			continue // strange, skip
		}
		privKey, err := gkm.retrieve(fpr)
		if err != nil {
			return err
		}
		err = consider(privKey, fpr, uid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (gkm *GPGKeypairManager) Put(privKey PrivateKey) error {
	// NOTE: we don't need this initially at least and this keypair mgr is not for general arbitrary usage
	return fmt.Errorf("cannot import private key into GPG keyring")
}

func (gkm *GPGKeypairManager) Get(keyID string) (PrivateKey, error) {
	stop := errors.New("stop marker")
	var hit PrivateKey
	match := func(privk PrivateKey, fpr string, uid string) error {
		if privk.PublicKey().ID() == keyID {
			hit = privk
			return stop
		}
		return nil
	}
	err := gkm.Walk(match)
	if err == stop {
		return hit, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("cannot find key %q in GPG keyring", keyID)
}

func (gkm *GPGKeypairManager) sign(fingerprint string, content []byte) ([]byte, error) {
	out, err := gkm.gpg(content, "--personal-digest-preferences", "SHA512", "--default-key", "0x"+fingerprint, "--detach-sign")
	if err != nil {
		return nil, fmt.Errorf("cannot sign using GPG: %v", err)
	}
	return out, nil
}

type GPGKeypairInfo struct {
	pubKey      PublicKey
	fingerprint string
}

func (gkm *GPGKeypairManager) findByName(name string) (*GPGKeypairInfo, error) {
	stop := errors.New("stop marker")
	var hit GPGKeypairInfo
	match := func(privk PrivateKey, fpr string, uid string) error {
		if uid == name {
			hit = GPGKeypairInfo{
				pubKey:      privk.PublicKey(),
				fingerprint: fpr,
			}
			return stop
		}
		return nil
	}
	err := gkm.Walk(match)
	if err == stop {
		return &hit, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("cannot find key named %q in GPG keyring", name)
}

var generateTemplate = `
Key-Type: RSA
Key-Length: 4096
Subkey-Type: RSA
Subkey-Length: 4096
Name-Real: %s
Preferences: SHA512
`

func (gkm *GPGKeypairManager) Generate(passphrase string, name string) error {
	_, err := gkm.findByName(name)
	if err == nil {
		return fmt.Errorf("key named %q already exists in GPG keyring", name)
	}
	generateParams := fmt.Sprintf(generateTemplate, name)
	if passphrase != "" {
		generateParams += "Passphrase: " + passphrase + "\n"
	}
	_, err = gkm.gpg([]byte(generateParams), "--batch", "--gen-key")
	if err != nil {
		return err
	}
	return nil
}

func (gkm *GPGKeypairManager) Export(name string) ([]byte, error) {
	keyInfo, err := gkm.findByName(name)
	if err != nil {
		return nil, err
	}
	encoded, err := EncodePublicKey(keyInfo.pubKey)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func (gkm *GPGKeypairManager) Delete(name string) error {
	keyInfo, err := gkm.findByName(name)
	if err != nil {
		return err
	}
	_, err = gkm.gpg(nil, "--batch", "--delete-secret-and-public-key", "0x"+keyInfo.fingerprint)
	if err != nil {
		return err
	}
	return nil
}
