// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/chrisccoulson/ubuntu-core-fde-utils"
	"golang.org/x/sys/unix"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/logger"
)

type recoveryReason uint8

const (
	recoveryReasonUnexpectedError recoveryReason = iota + 1
	recoveryReasonForced
	recoveryReasonInvalidKeyFile
	recoveryReasonTPMVerificationError
	recoveryReasonProvisioningError
	recoveryReasonTPMLockout
	recoveryReasonPinFail

	systemdCryptsetupPath = "/lib/systemd/systemd-cryptsetup"
)

type execError struct {
	path string
	err  error
}

func (e *execError) Error() string {
	return fmt.Sprintf("%s failed: %s", e.path, e.err)
}

func (e *execError) Unwrap() error {
	return e.err
}

func wrapExecError(cmd *exec.Cmd, err error) error {
	if err == nil {
		return nil
	}
	return &execError{path: cmd.Path, err: err}
}

// unlockEncryptedPartition unseals the keyfile and opens an encrypted device.
func unlockEncryptedPartition(name, device, keyfile, pinfile string) error {
	logger.Noticef("activate encrypted device %s", device)

	// XXX: we're currently not using the endorsement key certificate
	ekcert := ""
	pinTries := 1

	if err := activateWithTPM(
		name,      // the volume name
		device,    // the source device node
		keyfile,   // the sealed key file
		ekcert,    // the endorsement key certificate, if not insecure
		pinfile,   // the PIN file
		false,     // lock
		true,      // whether we should establish an insecure connection
		pinTries,  // number of PIN tries
		"tries=1", // extra systemd-cryptsetup options
	); err != nil {
		logger.Noticef("cannot activate device with TPM: %v", err)

		var ikfe fdeutil.InvalidKeyFileError
		var ee1 *execError
		var ee2 *exec.ExitError
		var ecve fdeutil.EkCertVerificationError
		var tpmve fdeutil.TPMVerificationError
		var pe *os.PathError

		recoveryReason := recoveryReasonUnexpectedError

		switch {
		case xerrors.As(err, &ikfe):
			recoveryReason = recoveryReasonInvalidKeyFile
		case xerrors.As(err, &ee1) && xerrors.As(ee1, &ee2) && ee1.path == systemdCryptsetupPath:
			// systemd-cryptsetup only provides 2 exit codes - success or fail - so we don't know
			// the reason it failed yet. If activation with the recovery key is successful, then it's
			// safe to assume that it failed because the key unsealed from the TPM is incorrect.
			recoveryReason = recoveryReasonInvalidKeyFile
		case xerrors.Is(err, fdeutil.ErrProvisioning):
			recoveryReason = recoveryReasonProvisioningError
		case xerrors.As(err, &ecve):
			recoveryReason = recoveryReasonTPMVerificationError
		case xerrors.As(err, &tpmve):
			recoveryReason = recoveryReasonTPMVerificationError
		case xerrors.As(err, &pe) && pe.Path == ekcert:
			recoveryReason = recoveryReasonTPMVerificationError
		case xerrors.Is(err, fdeutil.ErrLockout):
			recoveryReason = recoveryReasonTPMLockout
		case xerrors.Is(err, fdeutil.ErrPinFail):
			recoveryReason = recoveryReasonPinFail
		}

		if err := activateWithRecoveryKey(
			name,           // the volume name
			device,         // the source device
			"",             // authorization policy update file (not used)
			1,              // number of recovery tries
			recoveryReason, // the recovery reason
			"tries=1",      // extra systemd-cryptsetup options
		); err != nil {
			return fmt.Errorf("cannot activate device with recovery key: %v", err)
		}

		logger.Noticef("successfully activated device %s with recovery key (reason: %d)", device, recoveryReason)
		return nil
	}

	logger.Noticef("successfully activated device %s with TPM", device)
	return nil
}

// recoverEncryptedPartition opens an encrypted device prompting for the recovery key.
func recoverEncryptedPartition(name, device, authfile string) error {
	if err := activateWithRecoveryKey(
		name,                 // the volume name
		device,               // the source device
		authfile,             // authorization policy update file
		1,                    // number of recovery tries,
		recoveryReasonForced, // the recovery reason
		"tries=1",            // extra systemd-cryptsetup options
	); err != nil {
		return fmt.Errorf("cannot activate device with recovery key: %v", err)
	}

	logger.Noticef("successfully activated device %s with recovery key", device)
	return nil
}

func activate(name, device string, key []byte, options []string) error {
	// create a named pipe to pass the key
	fpath := filepath.Join("/run", "tmp-key")
	if err := syscall.Mkfifo(fpath, 0600); err != nil {
		return fmt.Errorf("cannot create named pipe: %v", err)
	}
	defer os.RemoveAll(fpath)

	cmd := exec.Command(systemdCryptsetupPath, "attach", name, device, fpath, strings.Join(options, ","))
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SYSTEMD_LOG_TARGET=console")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	// open the named pipe and write the key
	file, err := os.OpenFile(fpath, os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot open recovery key pipe: %v", err)
	}
	n, err := file.Write(key)
	if n != len(key) {
		file.Close()
		return fmt.Errorf("cannot write key: short write (%d bytes written)", n)
	}
	if err != nil {
		cmd.Process.Kill()
		file.Close()
		return fmt.Errorf("cannot write key: %v", err)
	}
	if err := file.Close(); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("cannot close recovery key pipe: %v", err)
	}

	return wrapExecError(cmd, cmd.Wait())
}

func askPassword(device, msg string) (string, error) {
	cmd := exec.Command(
		"systemd-ask-password",
		"--icon", "drive-harddisk",
		"--id", "ubuntu-core-cryptsetup:"+device,
		msg)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return "", wrapExecError(cmd, err)
	}
	result, err := out.ReadString('\n')
	if err != nil {
		return "", xerrors.Errorf("cannot read result from systemd-ask-password: %w", err)
	}
	return strings.TrimRight(result, "\n"), nil
}

func getRecoveryKey(device, recoveryKeyPath string) (string, error) {
	if recoveryKeyPath == "" {
		return askPassword(device, "Please enter the recovery key for disk "+device+":")
	}

	file, err := os.Open(recoveryKeyPath)
	if err != nil {
		return "", xerrors.Errorf("cannot open recovery key file: %w", err)
	}
	defer file.Close()

	key, err := ioutil.ReadAll(file)
	if err != nil {
		return "", xerrors.Errorf("cannot read recovery key file contents: %w", err)
	}
	return strings.TrimRight(string(key), "\n"), nil
}

func activateWithRecoveryKey(name, device, recoveryKeyPath string, tries int, reason recoveryReason, activateOptions ...string) error {
	var lastErr error
Retry:
	for i := 0; i < tries; i++ {
		recoveryPassphrase, err := getRecoveryKey(device, recoveryKeyPath)
		if err != nil {
			return xerrors.Errorf("cannot obtain recovery key: %w", err)
		}

		lastErr = nil

		// The recovery key should be provided as 8 groups of 5 base-10 digits, with each 5 digits being converted to a 2-byte number
		// to make a 16-byte key.
		var key bytes.Buffer
		for len(recoveryPassphrase) > 0 {
			if len(recoveryPassphrase) < 5 {
				// Badly formatted: not enough digits.
				lastErr = errors.New("incorrectly formatted recovery key (insufficient characters)")
				continue Retry
			}
			x, err := strconv.ParseUint(recoveryPassphrase[0:5], 10, 16)
			if err != nil {
				// Badly formatted: the 5 digits are not a base-10 number that fits in to 2-bytes.
				lastErr = errors.New("incorrectly formatted recovery key (invalid base-10 number)")
				continue Retry
			}
			binary.Write(&key, binary.LittleEndian, uint16(x))
			// Move to the next 5 digits
			recoveryPassphrase = recoveryPassphrase[5:]
			// Permit each set of 5 digits to be separated by '-', but don't allow the recovery key to end or begin with one.
			if len(recoveryPassphrase) > 1 && recoveryPassphrase[0] == '-' {
				recoveryPassphrase = recoveryPassphrase[1:]
			}
		}

		if err := activate(name, device, key.Bytes(), activateOptions); err != nil {
			lastErr = err
			if _, isExitErr := err.(*exec.ExitError); isExitErr {
				continue
			}
			return err
		}
		if _, err := unix.AddKey("user", fmt.Sprintf("snap-bootstrap-open-encrypted:%s:reason=%d", name, reason), key.Bytes(), -4); err != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot add recovery key to user keyring: %v\n", err)
		}
		break
	}

	return lastErr
}

func getPIN(device, pinFilePath string) (string, error) {
	if pinFilePath == "" {
		return askPassword(device, "Please enter the PIN for disk "+device+":")
	}

	file, err := os.Open(pinFilePath)
	if err != nil {
		return "", xerrors.Errorf("cannot open PIN file: %w", err)
	}
	defer file.Close()

	pin, err := ioutil.ReadAll(file)
	if err != nil {
		return "", xerrors.Errorf("cannot read PIN file contents: %w", err)
	}
	return strings.TrimRight(string(pin), "\n"), nil
}

func activateWithTPM(name, device, keyFilePath, ekCertFilePath, pinFilePath string, lock, insecure bool, tries int, activateOptions ...string) error {
	keyDataObject, err := fdeutil.LoadSealedKeyObject(keyFilePath)
	if err != nil {
		return xerrors.Errorf("cannot load sealed key object file: %w", err)
	}

	tpm, err := func() (*fdeutil.TPMConnection, error) {
		if !insecure {
			ekCertReader, err := os.Open(ekCertFilePath)
			if err != nil {
				return nil, xerrors.Errorf("cannot open endorsement key certificate file: %w", err)
			}
			defer ekCertReader.Close()
			return fdeutil.SecureConnectToDefaultTPM(ekCertReader, nil)
		}
		return fdeutil.ConnectToDefaultTPM()
	}()
	if err != nil {
		return xerrors.Errorf("cannot open TPM connection: %w", err)
	}
	defer tpm.Close()

	var key []byte
	reprovisionAttempted := false

	for {
		var err error
		var pin string
		if keyDataObject.AuthMode2F() == fdeutil.AuthModePIN {
			pin, err = getPIN(device, pinFilePath)
			if err != nil {
				return xerrors.Errorf("cannot obtain PIN: %w", err)
			}
			pinFilePath = ""
		}

	RetryUnseal:
		key, err = keyDataObject.UnsealFromTPM(tpm, pin, lock)
		if err != nil {
			switch err {
			case fdeutil.ErrProvisioning:
				// ErrProvisioning in this context indicates that there isn't a valid persistent SRK. Have a go at creating one now and then
				// retrying the unseal operation - if the previous SRK was evicted, the TPM owner hasn't changed and the storage hierarchy still
				// has a null authorization value, then this will allow us to unseal the key without requiring any type of manual recovery. If the
				// storage hierarchy has a non-null authorization value, ProvionTPM will fail. If the TPM owner has changed, ProvisionTPM might
				// succeed, but UnsealFromTPM will fail with InvalidKeyFileError when retried.
				if !reprovisionAttempted {
					reprovisionAttempted = true
					fmt.Fprintf(os.Stderr, "TPM is not provisioned correctly - attempting automatic recovery...\n")
					if err := fdeutil.ProvisionTPM(tpm, fdeutil.ProvisionModeWithoutLockout, nil); err == nil {
						fmt.Fprintf(os.Stderr, " ...automatic recovery succeeded. Retrying key unseal operation now\n")
						goto RetryUnseal
					} else {
						fmt.Fprintf(os.Stderr, " ...automatic recovery failed: %v\n", err)
					}
				}
			case fdeutil.ErrPinFail:
				tries -= 1
				if tries > 0 {
					continue
				}
			}
			return xerrors.Errorf("cannot unseal intermediate key from TPM: %w", err)
		}
		break
	}

	if err := activate(name, device, key, activateOptions); err != nil {
		return err
	}

	return nil
}
