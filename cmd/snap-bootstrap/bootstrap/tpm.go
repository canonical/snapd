// -*- Mode: Go; indent-tabs-mode: t -*-

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
package bootstrap

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"

	"github.com/chrisccoulson/ubuntu-core-fde-utils"

	"github.com/snapcore/snapd/osutil"
)

const (
	// Handles are in the block reserved for owner objects (0x01800000 - 0x01bfffff)
	pinHandle          = 0x01800000
	policyRevokeHandle = 0x01800001
)

var (
	provisionTPM = fdeutil.ProvisionTPM
	sealKeyToTPM = fdeutil.SealKeyToTPM
)

type TPMConnectionError string

func (s TPMConnectionError) Error() string {
	return "cannot connect to TPM: " + string(s)
}

type tpmSupport struct {
	// Connection to the TPM device
	tconn *fdeutil.TPMConnection
	// Lockout authorization
	lockoutAuth []byte
	// Owner authorization
	ownerAuth []byte
	// List of paths to shim files
	shimFiles []string
	// List of paths to bootloader files
	bootloaderFiles []string
	// List of paths to kernel files
	kernelFiles []string
}

func NewTPMSupport() (*tpmSupport, error) {
	tconn, err := fdeutil.ConnectToDefaultTPM()
	if err != nil {
		return nil, TPMConnectionError(err.Error())
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
	return setOSComponents(&t.shimFiles, pathList)
}

// SetBootloaderFiles verifies and sets the list of bootloader binaries.
func (t *tpmSupport) SetBootloaderFiles(pathList ...string) error {
	return setOSComponents(&t.bootloaderFiles, pathList)
}

// SetKernelFiles verifies and sets the list of kernel binaries.
func (t *tpmSupport) SetKernelFiles(pathList ...string) error {
	return setOSComponents(&t.kernelFiles, pathList)
}

func setOSComponents(c *[]string, pathList []string) error {
	for _, p := range pathList {
		if !osutil.FileExists(p) {
			return fmt.Errorf("file %s does not exist", p)
		}
	}
	*c = pathList
	return nil
}

// Provision tries to clear and provision the TPM.
func (t *tpmSupport) Provision() error {
	if err := provisionTPM(t.tconn, fdeutil.ProvisionModeFull, t.lockoutAuth, nil); err != nil {
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

// Seal seals the given key to the TPM and writes the sealed object to a file
// at the path specified by keyDest. Additional data required for updating the
// authorization policy is written to a file at the path specified by privDest.
// This file must live inside an encrypted volume protected by this key.
func (t *tpmSupport) Seal(key []byte, keyDest, privDest string) error {
	policyParams := &fdeutil.PolicyParams{}
	for _, shim := range t.shimFiles {
		s := &fdeutil.OSComponent{
			LoadType: fdeutil.FirmwareLoad,
			Image:    fdeutil.FileOSComponent(shim),
		}
		for _, bl := range t.bootloaderFiles {
			g := &fdeutil.OSComponent{
				LoadType: fdeutil.DirectLoadWithShimVerify,
				Image:    fdeutil.FileOSComponent(bl),
			}
			for _, kernel := range t.kernelFiles {
				k := &fdeutil.OSComponent{
					LoadType: fdeutil.DirectLoadWithShimVerify,
					Image:    fdeutil.FileOSComponent(kernel),
				}
				g.Next = append(g.Next, k)
			}
			s.Next = append(s.Next, g)
		}
		policyParams.LoadPaths = append(policyParams.LoadPaths, s)
	}

	createParams := fdeutil.CreationParams{
		PolicyRevocationHandle: policyRevokeHandle,
		PinHandle:              pinHandle,
		OwnerAuth:              t.ownerAuth,
	}

	if err := sealKeyToTPM(t.tconn, keyDest, privDest, &createParams, policyParams, key); err != nil {
		return fmt.Errorf("cannot seal data: %v", err)
	}
	return nil
}
