// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

/*
 * Copyright (C) 2020 Canonical Ltd
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

package secboot

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"

	"github.com/chrisccoulson/go-tpm2"
	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/osutil"
)

const (
	// Handles are in the block reserved for owner objects (0x01800000 - 0x01bfffff)
	pinHandle = 0x01800000
)

type tpmSupport struct {
	// Connection to the TPM device
	tconn *sb.TPMConnection
	// Lockout authorization
	lockoutAuth []byte
	// Paths to shim files
	shimFiles []string
	// Paths to bootloader files
	bootloaderFiles []string
	// Paths to kernel files
	kernelFiles []string
	// Kernel command lines
	kernelCmdlines []string
}

func NewTPMSupport() (*tpmSupport, error) {
	tconn, err := sb.ConnectToDefaultTPM()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to default TPM: %v", err)
	}

	lockoutAuth := make([]byte, 16)
	// crypto rand is protected against short reads
	_, err = rand.Read(lockoutAuth)
	if err != nil {
		return nil, fmt.Errorf("cannot create lockout authorization: %v", err)
	}

	t := &tpmSupport{tconn: tconn, lockoutAuth: lockoutAuth}

	return t, nil
}

// StoreLockoutAuth saves the lockout authorization data in a file at the
// path specified by filename.
func (t *tpmSupport) StoreLockoutAuth(filename string) error {
	if err := ioutil.WriteFile(filename, t.lockoutAuth, 0600); err != nil {
		return err
	}
	return nil
}

// SetShimFiles verifies and sets the list of shim binaries.
func (t *tpmSupport) SetShimFiles(pathList ...string) error {
	if err := ensureFilesExist(pathList); err != nil {
		return err
	}
	t.shimFiles = pathList
	return nil
}

// SetBootloaderFiles verifies and sets the list of bootloader binaries.
func (t *tpmSupport) SetBootloaderFiles(pathList ...string) error {
	if err := ensureFilesExist(pathList); err != nil {
		return err
	}
	t.bootloaderFiles = pathList
	return nil
}

// SetKernelFiles verifies and sets the list of kernel binaries.
func (t *tpmSupport) SetKernelFiles(pathList ...string) error {
	if err := ensureFilesExist(pathList); err != nil {
		return err
	}
	t.kernelFiles = pathList
	return nil
}

func ensureFilesExist(pathList []string) error {
	for _, p := range pathList {
		if !osutil.FileExists(p) {
			return fmt.Errorf("file %s does not exist", p)
		}
	}
	return nil
}

var (
	sbProvisionTPM = sb.ProvisionTPM
)

// Provision tries to clear and provision the TPM.
func (t *tpmSupport) Provision() error {
	if err := sbProvisionTPM(t.tconn, sb.ProvisionModeFull, t.lockoutAuth); err != nil {
		return fmt.Errorf("cannot provision TPM: %v", err)
	}

	return nil
}

// Close closes the TPM connection.
func (t *tpmSupport) Close() error {
	if err := t.tconn.Close(); err != nil {
		return fmt.Errorf("cannot close TPM connection: %v", err)
	}
	return nil
}

func (t *tpmSupport) SetKernelCmdlines(cmdlines []string) {
	t.kernelCmdlines = cmdlines
}

var (
	sbSealKeyToTPM                  = sb.SealKeyToTPM
	sbAddEFISecureBootPolicyProfile = sb.AddEFISecureBootPolicyProfile
	sbAddSystemdEFIStubProfile      = sb.AddSystemdEFIStubProfile
)

// Seal seals the given key to the TPM and writes the sealed object to a file at the
// path specified by keyPath. Additional data required for updating the authorization
// policy is written to a file at the path specified by policyUpdatePath. This file
// must live inside an encrypted volume protected by this key.
func (t *tpmSupport) Seal(key []byte, keyPath, policyUpdatePath string) error {
	pcrProfile := sb.NewPCRProtectionProfile()

	// Add EFI secure boot policy profile
	policyParams := sb.EFISecureBootPolicyProfileParams{
		PCRAlgorithm:  tpm2.HashAlgorithmSHA256,
		LoadSequences: make([]*sb.EFIImageLoadEvent, 0, len(t.shimFiles)),
		// TODO:UC20: set SignatureDbUpdateKeystore to support key rotation
	}

	for _, shim := range t.shimFiles {
		s := &sb.EFIImageLoadEvent{
			Source: sb.Firmware,
			Image:  sb.FileEFIImage(shim),
			Next:   make([]*sb.EFIImageLoadEvent, 0, len(t.bootloaderFiles)),
		}
		for _, bl := range t.bootloaderFiles {
			g := &sb.EFIImageLoadEvent{
				Source: sb.Shim,
				Image:  sb.FileEFIImage(bl),
				Next:   make([]*sb.EFIImageLoadEvent, 0, len(t.kernelFiles)),
			}
			for _, kernel := range t.kernelFiles {
				k := &sb.EFIImageLoadEvent{
					Source: sb.Shim,
					Image:  sb.FileEFIImage(kernel),
				}
				g.Next = append(g.Next, k)
			}
			s.Next = append(s.Next, g)
		}
		policyParams.LoadSequences = append(policyParams.LoadSequences, s)
	}

	if err := sbAddEFISecureBootPolicyProfile(pcrProfile, &policyParams); err != nil {
		return fmt.Errorf("cannot add EFI secure boot policy profile: %v", err)
	}

	// Add systemd EFI stub profile
	systemdStubParams := sb.SystemdEFIStubProfileParams{
		PCRAlgorithm:   tpm2.HashAlgorithmSHA256,
		PCRIndex:       12,
		KernelCmdlines: t.kernelCmdlines,
	}
	if err := sbAddSystemdEFIStubProfile(pcrProfile, &systemdStubParams); err != nil {
		return fmt.Errorf("cannot add systemd EFI stub profile: %v", err)
	}

	// Seal key to the TPM
	creationParams := sb.KeyCreationParams{
		PCRProfile: pcrProfile,
		PINHandle:  pinHandle,
	}
	if err := sbSealKeyToTPM(t.tconn, key, keyPath, policyUpdatePath, &creationParams); err != nil {
		return fmt.Errorf("cannot seal data: %v", err)
	}

	return nil
}
