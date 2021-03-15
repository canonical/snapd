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
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/canonical/go-tpm2"
	sb "github.com/snapcore/secboot"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/snap/snapfile"
)

const (
	keyringPrefix = "ubuntu-fde"
)

var (
	sbConnectToDefaultTPM                  = sb.ConnectToDefaultTPM
	sbMeasureSnapSystemEpochToTPM          = sb.MeasureSnapSystemEpochToTPM
	sbMeasureSnapModelToTPM                = sb.MeasureSnapModelToTPM
	sbBlockPCRProtectionPolicies           = sb.BlockPCRProtectionPolicies
	sbActivateVolumeWithTPMSealedKey       = sb.ActivateVolumeWithTPMSealedKey
	sbActivateVolumeWithRecoveryKey        = sb.ActivateVolumeWithRecoveryKey
	sbActivateVolumeWithKey                = sb.ActivateVolumeWithKey
	sbAddEFISecureBootPolicyProfile        = sb.AddEFISecureBootPolicyProfile
	sbAddEFIBootManagerProfile             = sb.AddEFIBootManagerProfile
	sbAddSystemdEFIStubProfile             = sb.AddSystemdEFIStubProfile
	sbAddSnapModelProfile                  = sb.AddSnapModelProfile
	sbSealKeyToTPMMultiple                 = sb.SealKeyToTPMMultiple
	sbUpdateKeyPCRProtectionPolicyMultiple = sb.UpdateKeyPCRProtectionPolicyMultiple

	randutilRandomKernelUUID = randutil.RandomKernelUUID

	isTPMEnabled = isTPMEnabledImpl
	provisionTPM = provisionTPMImpl
)

func isTPMEnabledImpl(tpm *sb.TPMConnection) bool {
	return tpm.IsEnabled()
}

func CheckKeySealingSupported() error {
	logger.Noticef("checking if secure boot is enabled...")
	if err := checkSecureBootEnabled(); err != nil {
		logger.Noticef("secure boot not enabled: %v", err)
		return err
	}
	logger.Noticef("secure boot is enabled")

	logger.Noticef("checking if TPM device is available...")
	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		err = fmt.Errorf("cannot connect to TPM device: %v", err)
		logger.Noticef("%v", err)
		return err
	}
	defer tpm.Close()

	if !isTPMEnabled(tpm) {
		logger.Noticef("TPM device detected but not enabled")
		return fmt.Errorf("TPM device is not enabled")
	}

	logger.Noticef("TPM device detected and enabled")

	return nil
}

func checkSecureBootEnabled() error {
	// 8be4df61-93ca-11d2-aa0d-00e098032b8c is the EFI Global Variable vendor GUID
	b, _, err := efi.ReadVarBytes("SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c")
	if err != nil {
		if err == efi.ErrNoEFISystem {
			return err
		}
		return fmt.Errorf("cannot read secure boot variable: %v", err)
	}
	if len(b) < 1 {
		return errors.New("secure boot variable does not exist")
	}
	if b[0] != 1 {
		return errors.New("secure boot is disabled")
	}

	return nil
}

// initramfsPCR is the TPM PCR that we reserve for the EFI image and use
// for measurement from the initramfs.
const initramfsPCR = 12

func secureConnectToTPM(ekcfile string) (*sb.TPMConnection, error) {
	ekCertReader, err := os.Open(ekcfile)
	if err != nil {
		return nil, fmt.Errorf("cannot open endorsement key certificate file: %v", err)
	}
	defer ekCertReader.Close()

	return sb.SecureConnectToDefaultTPM(ekCertReader, nil)
}

func insecureConnectToTPM() (*sb.TPMConnection, error) {
	return sbConnectToDefaultTPM()
}

func measureWhenPossible(whatHow func(tpm *sb.TPMConnection) error) error {
	// the model is ready, we're good to try measuring it now
	tpm, err := insecureConnectToTPM()
	if err != nil {
		if xerrors.Is(err, sb.ErrNoTPM2Device) {
			return nil
		}
		return fmt.Errorf("cannot open TPM connection: %v", err)
	}
	defer tpm.Close()

	if !isTPMEnabled(tpm) {
		return nil
	}

	return whatHow(tpm)
}

// MeasureSnapSystemEpochWhenPossible measures the snap system epoch only if the
// TPM device is available. If there's no TPM device success is returned.
func MeasureSnapSystemEpochWhenPossible() error {
	measure := func(tpm *sb.TPMConnection) error {
		return sbMeasureSnapSystemEpochToTPM(tpm, initramfsPCR)
	}

	if err := measureWhenPossible(measure); err != nil {
		return fmt.Errorf("cannot measure snap system epoch: %v", err)
	}

	return nil
}

// MeasureSnapModelWhenPossible measures the snap model only if the TPM device is
// available. If there's no TPM device success is returned.
func MeasureSnapModelWhenPossible(findModel func() (*asserts.Model, error)) error {
	measure := func(tpm *sb.TPMConnection) error {
		model, err := findModel()
		if err != nil {
			return err
		}
		return sbMeasureSnapModelToTPM(tpm, initramfsPCR, model)
	}

	if err := measureWhenPossible(measure); err != nil {
		return fmt.Errorf("cannot measure snap model: %v", err)
	}

	return nil
}

// LockSealedKeys manually locks access to the sealed keys. Meant to be
// called in place of passing lockKeysOnFinish as true to
// UnlockVolumeUsingSealedKeyIfEncrypted for cases where we don't know if a
// given call is the last one to unlock a volume like in degraded recover mode.
func LockSealedKeys() error {
	if FDEHasRevealKey() {
		return lockFDERevealSealedKeys()
	}
	return lockTPMSealedKeys()
}

func lockFDERevealSealedKeys() error {
	buf, err := json.Marshal(FDERevealKeyRequest{
		Op: "lock",
	})
	if err != nil {
		return fmt.Errorf(`cannot build request for fde-reveal-key "lock": %v`, err)
	}
	if output, err := runFDERevealKeyCommand(buf); err != nil {
		return fmt.Errorf(`cannot run fde-reveal-key "lock": %v`, osutil.OutputErr(output, err))
	}

	return nil
}

func lockTPMSealedKeys() error {
	tpm, tpmErr := sbConnectToDefaultTPM()
	if tpmErr != nil {
		if xerrors.Is(tpmErr, sb.ErrNoTPM2Device) {
			logger.Noticef("cannot open TPM connection: %v", tpmErr)
			return nil
		}
		return fmt.Errorf("cannot lock TPM: %v", tpmErr)
	}
	defer tpm.Close()

	// Lock access to the sealed keys. This should be called whenever there
	// is a TPM device detected, regardless of whether secure boot is enabled
	// or there is an encrypted volume to unlock. Note that snap-bootstrap can
	// be called several times during initialization, and if there are multiple
	// volumes to unlock we should lock access to the sealed keys only after
	// the last encrypted volume is unlocked, in which case lockKeysOnFinish
	// should be set to true.
	//
	// We should only touch the PCR that we've currently reserved for the kernel
	// EFI image. Touching others will break the ability to perform any kind of
	// attestation using the TPM because it will make the log inconsistent.
	return sbBlockPCRProtectionPolicies(tpm, []int{initramfsPCR})
}

// UnlockVolumeUsingSealedKeyIfEncrypted verifies whether an encrypted volume
// with the specified name exists and unlocks it using a sealed key in a file
// with a corresponding name. The options control activation with the
// recovery key will be attempted if a prior activation attempt with
// the sealed key fails.
//
// Note that if the function proceeds to the point where it knows definitely
// whether there is an encrypted device or not, IsEncrypted on the return
// value will be true, even if error is non-nil. This is so that callers can be
// robust and try unlocking using another method for example.
func UnlockVolumeUsingSealedKeyIfEncrypted(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
	res := UnlockResult{}

	// find the encrypted device using the disk we were provided - note that
	// we do not specify IsDecryptedDevice in opts because here we are
	// looking for the encrypted device to unlock, later on in the boot
	// process we will look for the decrypted device to ensure it matches
	// what we expected
	partUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
	if err == nil {
		res.IsEncrypted = true
	} else {
		var errNotFound disks.PartitionNotFoundError
		if !xerrors.As(err, &errNotFound) {
			// some other kind of catastrophic error searching
			return res, fmt.Errorf("error enumerating partitions for disk to find encrypted device %q: %v", name, err)
		}
		// otherwise it is an error not found and we should search for the
		// unencrypted device
		partUUID, err = disk.FindMatchingPartitionUUIDWithFsLabel(name)
		if err != nil {
			return res, fmt.Errorf("error enumerating partitions for disk to find unencrypted device %q: %v", name, err)
		}
	}

	partDevice := filepath.Join("/dev/disk/by-partuuid", partUUID)

	if !res.IsEncrypted {
		// if we didn't find an encrypted device just return, don't try to
		// unlock it
		// the filesystem device for the unencrypted case is the same as the
		// partition device
		res.PartDevice = partDevice
		res.FsDevice = res.PartDevice
		return res, nil
	}

	mapperName := name + "-" + randutilRandomKernelUUID()
	sourceDevice := partDevice
	targetDevice := filepath.Join("/dev/mapper", mapperName)

	if FDEHasRevealKey() {
		return unlockVolumeUsingSealedKeyFDERevealKey(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
	} else {
		return unlockVolumeUsingSealedKeySecboot(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
	}
}

// FDERevealKeyRequest carries the operation and parameters for the
// fde-reveal-key binary to support unsealing keys that were sealed
// with the "fde-setup" hook.
type FDERevealKeyRequest struct {
	Op string `json:"op"`

	SealedKey []byte `json:"sealed-key,omitempty"`
	KeyName   string `json:"key-name,omitempty"`

	// TODO: add VolumeName,SourceDevicePath later
}

// fdeRevealKeyRuntimeMax is the maximum runtime a fde-reveal-key can execute
// XXX: what is a reasonable default here?
var fdeRevealKeyRuntimeMax = 2 * time.Minute

// 50 ms means we check at a frequency 20 Hz, fast enough to not hold
// up boot, but not too fast that we are hogging the CPU from the
// thing we are waiting to finish running
var fdeRevealKeyPollWait = 50 * time.Millisecond

// fdeRevealKeyPollWaitParanoiaFactor controls much longer we wait
// then fdeRevealKeyRuntimeMax before stopping to poll for results
var fdeRevealKeyPollWaitParanoiaFactor = 2

// overridden in tests
var fdeRevealKeyCommandExtra []string

// runFDERevealKeyCommand returns the output of fde-reveal-key run
// with systemd.
//
// Note that systemd-run in the initrd can only talk to the private
// systemd bus so this cannot use "--pipe" or "--wait", see
// https://github.com/snapcore/core-initrd/issues/13
func runFDERevealKeyCommand(stdin []byte) (output []byte, err error) {
	runDir := filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key")
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create tmp dir for fde-reveal-key: %v", err)
	}

	// delete and re-create the std{in,out,err} stream files that we use for the
	// hook to be robust against bugs where the files are created with too
	// permissive permissions or not properly deleted afterwards since the hook
	// will be invoked multiple times during the initrd and we want to be really
	// careful since the stdout file will contain the unsealed encryption key
	for _, stream := range []string{"stdin", "stdout", "stderr"} {
		streamFile := filepath.Join(runDir, "fde-reveal-key."+stream)
		// we want to make sure that the file permissions for stdout are always
		// 0600, so to ensure this is the case and be robust against bugs, we
		// always delete the file and re-create it with 0600

		// note that if the file already exists, WriteFile will not change the
		// permissions, so deleting first is the right thing to do
		os.Remove(streamFile)
		if stream == "stdin" {
			err = ioutil.WriteFile(streamFile, stdin, 0600)
		} else {
			err = ioutil.WriteFile(streamFile, nil, 0600)
		}
		if err != nil {
			return nil, fmt.Errorf("cannot create %s for fde-reveal-key: %v", stream, err)
		}
	}

	// TODO: put this into a new "systemd/run" package
	cmd := exec.Command(
		"systemd-run",
		"--collect",
		"--service-type=exec",
		"--quiet",
		// ensure we get some result from the hook within a
		// reasonable timeout and output from systemd if
		// things go wrong
		fmt.Sprintf("--property=RuntimeMaxSec=%s", fdeRevealKeyRuntimeMax),
		// Do not allow mounting, this ensures hooks in initrd
		// can not mess around with ubuntu-data.
		//
		// Note that this is not about perfect confinement, more about
		// making sure that people using the hook know that we do not
		// want them to mess around outside of just providing unseal.
		"--property=SystemCallFilter=~@mount",
		// WORKAROUNDS
		// workaround the lack of "--pipe"
		fmt.Sprintf("--property=StandardInput=file:%s/fde-reveal-key.stdin", runDir),
		// NOTE: these files are manually created above with 0600 because by
		// default systemd will create them 0644 and we want to be paranoid here
		fmt.Sprintf("--property=StandardOutput=file:%s/fde-reveal-key.stdout", runDir),
		fmt.Sprintf("--property=StandardError=file:%s/fde-reveal-key.stderr", runDir),
		// this ensures we get useful output for e.g. segfaults
		fmt.Sprintf(`--property=ExecStopPost=/bin/sh -c 'if [ "$EXIT_STATUS" = 0 ]; then touch %[1]s/fde-reveal-key.success; else echo "service result: $SERVICE_RESULT" >%[1]s/fde-reveal-key.failed; fi'`, runDir),
	)
	if fdeRevealKeyCommandExtra != nil {
		cmd.Args = append(cmd.Args, fdeRevealKeyCommandExtra...)
	}
	// fde-reveal-key is what we actually need to run
	cmd.Args = append(cmd.Args, "fde-reveal-key")

	// ensure we cleanup our tmp files
	defer func() {
		if err := os.RemoveAll(runDir); err != nil {
			logger.Noticef("cannot remove tmp dir: %v", err)
		}
	}()

	// run the command
	output, err = cmd.CombinedOutput()
	if err != nil {
		return output, err
	}

	// This loop will be terminate by systemd-run, either because
	// fde-reveal-key exists or it gets killed when it reaches the
	// fdeRevealKeyRuntimeMax defined above.
	//
	// However we are paranoid and exit this loop if systemd
	// did not terminate the process after twice the allocated
	// runtime
	maxLoops := int(fdeRevealKeyRuntimeMax/fdeRevealKeyPollWait) * fdeRevealKeyPollWaitParanoiaFactor
	for i := 0; i < maxLoops; i++ {
		switch {
		case osutil.FileExists(filepath.Join(runDir, "fde-reveal-key.failed")):
			stderr, _ := ioutil.ReadFile(filepath.Join(runDir, "fde-reveal-key.stderr"))
			systemdErr, _ := ioutil.ReadFile(filepath.Join(runDir, "fde-reveal-key.failed"))
			buf := bytes.NewBuffer(stderr)
			buf.Write(systemdErr)
			return buf.Bytes(), fmt.Errorf("fde-reveal-key failed")
		case osutil.FileExists(filepath.Join(runDir, "fde-reveal-key.success")):
			return ioutil.ReadFile(filepath.Join(runDir, "fde-reveal-key.stdout"))
		default:
			time.Sleep(fdeRevealKeyPollWait)
		}
	}

	// this should never happen, the loop above should be terminated
	// via systemd
	return nil, fmt.Errorf("internal error: systemd-run did not honor RuntimeMax=%s setting", fdeRevealKeyRuntimeMax)
}

func unlockVolumeUsingSealedKeyFDERevealKey(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
	res := UnlockResult{IsEncrypted: true, PartDevice: sourceDevice}

	sealedKey, err := ioutil.ReadFile(sealedEncryptionKeyFile)
	if err != nil {
		return res, fmt.Errorf("cannot read sealed key file: %v", err)
	}
	buf, err := json.Marshal(FDERevealKeyRequest{
		Op:        "reveal",
		SealedKey: sealedKey,
		KeyName:   name,
	})
	if err != nil {
		return res, fmt.Errorf("cannot build request for fde-reveal-key: %v", err)
	}
	output, err := runFDERevealKeyCommand(buf)
	if err != nil {
		return res, fmt.Errorf("cannot run fde-reveal-key: %v", osutil.OutputErr(output, err))
	}

	// the output of fde-reveal-key is the unsealed key
	unsealedKey := output
	if err := unlockEncryptedPartitionWithKey(mapperName, sourceDevice, unsealedKey); err != nil {
		return res, fmt.Errorf("cannot unlock encrypted partition: %v", err)
	}
	res.FsDevice = targetDevice
	res.UnlockMethod = UnlockedWithSealedKey
	return res, nil
}

func unlockVolumeUsingSealedKeySecboot(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
	// TODO:UC20: use sb.SecureConnectToDefaultTPM() if we decide there's benefit in doing that or
	//            we have a hard requirement for a valid EK cert chain for every boot (ie, panic
	//            if there isn't one). But we can't do that as long as we need to download
	//            intermediate certs from the manufacturer.

	res := UnlockResult{IsEncrypted: true, PartDevice: sourceDevice}
	// Obtain a TPM connection.
	tpm, tpmErr := sbConnectToDefaultTPM()
	if tpmErr != nil {
		if !xerrors.Is(tpmErr, sb.ErrNoTPM2Device) {
			return res, fmt.Errorf("cannot unlock encrypted device %q: %v", name, tpmErr)
		}
		logger.Noticef("cannot open TPM connection: %v", tpmErr)
	} else {
		defer tpm.Close()
	}

	// Also check if the TPM device is enabled. The platform firmware may disable the storage
	// and endorsement hierarchies, but the device will remain visible to the operating system.
	tpmDeviceAvailable := tpmErr == nil && isTPMEnabled(tpm)

	// if we don't have a tpm, and we allow using a recovery key, do that
	// directly
	if !tpmDeviceAvailable && opts.AllowRecoveryKey {
		if err := UnlockEncryptedVolumeWithRecoveryKey(mapperName, sourceDevice); err != nil {
			return res, err
		}
		res.FsDevice = targetDevice
		res.UnlockMethod = UnlockedWithRecoveryKey
		return res, nil
	}

	// otherwise we have a tpm and we should use the sealed key first, but
	// this method will fallback to using the recovery key if enabled
	method, err := unlockEncryptedPartitionWithSealedKey(tpm, mapperName, sourceDevice, sealedEncryptionKeyFile, "", opts.AllowRecoveryKey)
	res.UnlockMethod = method
	if err == nil {
		res.FsDevice = targetDevice
	}
	return res, err
}

// UnlockEncryptedVolumeUsingKey unlocks an existing volume using the provided key.
func UnlockEncryptedVolumeUsingKey(disk disks.Disk, name string, key []byte) (UnlockResult, error) {
	unlockRes := UnlockResult{
		UnlockMethod: NotUnlocked,
	}
	// find the encrypted device using the disk we were provided - note that
	// we do not specify IsDecryptedDevice in opts because here we are
	// looking for the encrypted device to unlock, later on in the boot
	// process we will look for the decrypted device to ensure it matches
	// what we expected
	partUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
	if err != nil {
		return unlockRes, err
	}
	unlockRes.IsEncrypted = true
	// we have a device
	encdev := filepath.Join("/dev/disk/by-partuuid", partUUID)
	unlockRes.PartDevice = encdev
	// make up a new name for the mapped device
	mapperName := name + "-" + randutilRandomKernelUUID()
	if err := unlockEncryptedPartitionWithKey(mapperName, encdev, key); err != nil {
		return unlockRes, err
	}

	unlockRes.FsDevice = filepath.Join("/dev/mapper/", mapperName)
	unlockRes.UnlockMethod = UnlockedWithKey
	return unlockRes, nil
}

// UnlockEncryptedVolumeWithRecoveryKey prompts for the recovery key and uses it
// to open an encrypted device.
func UnlockEncryptedVolumeWithRecoveryKey(name, device string) error {
	options := sb.ActivateVolumeOptions{
		RecoveryKeyTries: 3,
		KeyringPrefix:    keyringPrefix,
	}

	if err := sbActivateVolumeWithRecoveryKey(name, device, nil, &options); err != nil {
		return fmt.Errorf("cannot unlock encrypted device %q: %v", device, err)
	}

	return nil
}

func isActivatedWithRecoveryKey(err error) bool {
	if err == nil {
		return false
	}
	// with non-nil err, we should check for err being ActivateWithTPMSealedKeyError
	// and RecoveryKeyUsageErr inside that being nil - this indicates that the
	// recovery key was used to unlock it
	activateErr, ok := err.(*sb.ActivateWithTPMSealedKeyError)
	if !ok {
		return false
	}
	return activateErr.RecoveryKeyUsageErr == nil
}

// unlockEncryptedPartitionWithSealedKey unseals the keyfile and opens an encrypted
// device. If activation with the sealed key fails, this function will attempt to
// activate it with the fallback recovery key instead.
func unlockEncryptedPartitionWithSealedKey(tpm *sb.TPMConnection, name, device, keyfile, pinfile string, allowRecovery bool) (UnlockMethod, error) {
	options := sb.ActivateVolumeOptions{
		PassphraseTries: 1,
		// disable recovery key by default
		RecoveryKeyTries: 0,
		KeyringPrefix:    keyringPrefix,
	}
	if allowRecovery {
		// enable recovery key only when explicitly allowed
		options.RecoveryKeyTries = 3
	}

	// XXX: pinfile is currently not used
	activated, err := sbActivateVolumeWithTPMSealedKey(tpm, name, device, keyfile, nil, &options)

	if activated {
		// non nil error may indicate the volume was unlocked using the
		// recovery key
		if err == nil {
			logger.Noticef("successfully activated encrypted device %q with TPM", device)
			return UnlockedWithSealedKey, nil
		} else if isActivatedWithRecoveryKey(err) {
			logger.Noticef("successfully activated encrypted device %q using a fallback activation method", device)
			return UnlockedWithRecoveryKey, nil
		}
		// no other error is possible when activation succeeded
		return UnlockStatusUnknown, fmt.Errorf("internal error: volume activated with unexpected error: %v", err)
	}
	// ActivateVolumeWithTPMSealedKey should always return an error if activated == false
	return NotUnlocked, fmt.Errorf("cannot activate encrypted device %q: %v", device, err)
}

// unlockEncryptedPartitionWithKey unlocks encrypted partition with the provided
// key.
func unlockEncryptedPartitionWithKey(name, device string, key []byte) error {
	// no special options set
	options := sb.ActivateVolumeOptions{}
	err := sbActivateVolumeWithKey(name, device, key, &options)
	if err == nil {
		logger.Noticef("successfully activated encrypted device %v using a key", device)
	}
	return err
}

// SealKeys provisions the TPM and seals the encryption keys according to the
// specified parameters. If the TPM is already provisioned, or a sealed key already
// exists, SealKeys will fail and return an error.
func SealKeys(keys []SealKeyRequest, params *SealKeysParams) error {
	numModels := len(params.ModelParams)
	if numModels < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}

	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}
	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	pcrProfile, err := buildPCRProtectionProfile(params.ModelParams)
	if err != nil {
		return err
	}

	if params.TPMProvision {
		// Provision the TPM as late as possible
		if err := tpmProvision(tpm, params.TPMLockoutAuthFile); err != nil {
			return err
		}
	}

	// Seal the provided keys to the TPM
	creationParams := sb.KeyCreationParams{
		PCRProfile:             pcrProfile,
		PCRPolicyCounterHandle: tpm2.Handle(params.PCRPolicyCounterHandle),
		AuthKey:                params.TPMPolicyAuthKey,
	}

	sbKeys := make([]*sb.SealKeyRequest, 0, len(keys))
	for i := range keys {
		sbKeys = append(sbKeys, &sb.SealKeyRequest{
			Key:  keys[i].Key[:],
			Path: keys[i].KeyFile,
		})
	}

	authKey, err := sbSealKeyToTPMMultiple(tpm, sbKeys, &creationParams)
	if err != nil {
		return err
	}
	if params.TPMPolicyAuthKeyFile != "" {
		if err := osutil.AtomicWriteFile(params.TPMPolicyAuthKeyFile, authKey, 0600, 0); err != nil {
			return fmt.Errorf("cannot write the policy auth key file: %v", err)
		}
	}

	return nil
}

// ResealKeys updates the PCR protection policy for the sealed encryption keys
// according to the specified parameters.
func ResealKeys(params *ResealKeysParams) error {
	numModels := len(params.ModelParams)
	if numModels < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}

	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}
	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	pcrProfile, err := buildPCRProtectionProfile(params.ModelParams)
	if err != nil {
		return err
	}

	authKey, err := ioutil.ReadFile(params.TPMPolicyAuthKeyFile)
	if err != nil {
		return fmt.Errorf("cannot read the policy auth key file: %v", err)
	}

	return sbUpdateKeyPCRProtectionPolicyMultiple(tpm, params.KeyFiles, authKey, pcrProfile)
}

func buildPCRProtectionProfile(modelParams []*SealKeyModelParams) (*sb.PCRProtectionProfile, error) {
	numModels := len(modelParams)
	modelPCRProfiles := make([]*sb.PCRProtectionProfile, 0, numModels)

	for _, mp := range modelParams {
		modelProfile := sb.NewPCRProtectionProfile()

		loadSequences, err := buildLoadSequences(mp.EFILoadChains)
		if err != nil {
			return nil, fmt.Errorf("cannot build EFI image load sequences: %v", err)
		}

		// Add EFI secure boot policy profile
		policyParams := sb.EFISecureBootPolicyProfileParams{
			PCRAlgorithm:  tpm2.HashAlgorithmSHA256,
			LoadSequences: loadSequences,
			// TODO:UC20: set SignatureDbUpdateKeystore to support applying forbidden
			//            signature updates to blacklist signing keys (after rotating them).
			//            This also requires integration of sbkeysync, and some work to
			//            ensure that the PCR profile is updated before/after sbkeysync executes.
		}

		if err := sbAddEFISecureBootPolicyProfile(modelProfile, &policyParams); err != nil {
			return nil, fmt.Errorf("cannot add EFI secure boot policy profile: %v", err)
		}

		// Add EFI boot manager profile
		bootManagerParams := sb.EFIBootManagerProfileParams{
			PCRAlgorithm:  tpm2.HashAlgorithmSHA256,
			LoadSequences: loadSequences,
		}
		if err := sbAddEFIBootManagerProfile(modelProfile, &bootManagerParams); err != nil {
			return nil, fmt.Errorf("cannot add EFI boot manager profile: %v", err)
		}

		// Add systemd EFI stub profile
		if len(mp.KernelCmdlines) != 0 {
			systemdStubParams := sb.SystemdEFIStubProfileParams{
				PCRAlgorithm:   tpm2.HashAlgorithmSHA256,
				PCRIndex:       initramfsPCR,
				KernelCmdlines: mp.KernelCmdlines,
			}
			if err := sbAddSystemdEFIStubProfile(modelProfile, &systemdStubParams); err != nil {
				return nil, fmt.Errorf("cannot add systemd EFI stub profile: %v", err)
			}
		}

		// Add snap model profile
		if mp.Model != nil {
			snapModelParams := sb.SnapModelProfileParams{
				PCRAlgorithm: tpm2.HashAlgorithmSHA256,
				PCRIndex:     initramfsPCR,
				Models:       []sb.SnapModel{mp.Model},
			}
			if err := sbAddSnapModelProfile(modelProfile, &snapModelParams); err != nil {
				return nil, fmt.Errorf("cannot add snap model profile: %v", err)
			}
		}

		modelPCRProfiles = append(modelPCRProfiles, modelProfile)
	}

	var pcrProfile *sb.PCRProtectionProfile
	if numModels > 1 {
		pcrProfile = sb.NewPCRProtectionProfile().AddProfileOR(modelPCRProfiles...)
	} else {
		pcrProfile = modelPCRProfiles[0]
	}

	logger.Debugf("PCR protection profile:\n%s", pcrProfile.String())

	return pcrProfile, nil
}

func tpmProvision(tpm *sb.TPMConnection, lockoutAuthFile string) error {
	// Create and save the lockout authorization file
	lockoutAuth := make([]byte, 16)
	// crypto rand is protected against short reads
	_, err := rand.Read(lockoutAuth)
	if err != nil {
		return fmt.Errorf("cannot create lockout authorization: %v", err)
	}
	if err := osutil.AtomicWriteFile(lockoutAuthFile, lockoutAuth, 0600, 0); err != nil {
		return fmt.Errorf("cannot write the lockout authorization file: %v", err)
	}

	// TODO:UC20: ideally we should ask the firmware to clear the TPM and then reboot
	//            if the device has previously been provisioned, see
	//            https://godoc.org/github.com/snapcore/secboot#RequestTPMClearUsingPPI
	if err := provisionTPM(tpm, sb.ProvisionModeFull, lockoutAuth); err != nil {
		logger.Noticef("TPM provisioning error: %v", err)
		return fmt.Errorf("cannot provision TPM: %v", err)
	}
	return nil
}

func provisionTPMImpl(tpm *sb.TPMConnection, mode sb.ProvisionMode, lockoutAuth []byte) error {
	return tpm.EnsureProvisioned(mode, lockoutAuth)
}

// buildLoadSequences builds EFI load image event trees from this package LoadChains
func buildLoadSequences(chains []*LoadChain) (loadseqs []*sb.EFIImageLoadEvent, err error) {
	// this will build load event trees for the current
	// device configuration, e.g. something like:
	//
	// shim -> recovery grub -> recovery kernel 1
	//                      |-> recovery kernel 2
	//                      |-> recovery kernel ...
	//                      |-> normal grub -> run kernel good
	//                                     |-> run kernel try

	for _, chain := range chains {
		// root of load events has source Firmware
		loadseq, err := chain.loadEvent(sb.Firmware)
		if err != nil {
			return nil, err
		}
		loadseqs = append(loadseqs, loadseq)
	}
	return loadseqs, nil
}

// loadEvent builds the corresponding load event and its tree
func (lc *LoadChain) loadEvent(source sb.EFIImageLoadEventSource) (*sb.EFIImageLoadEvent, error) {
	var next []*sb.EFIImageLoadEvent
	for _, nextChain := range lc.Next {
		// everything that is not the root has source shim
		ev, err := nextChain.loadEvent(sb.Shim)
		if err != nil {
			return nil, err
		}
		next = append(next, ev)
	}
	image, err := efiImageFromBootFile(lc.BootFile)
	if err != nil {
		return nil, err
	}
	return &sb.EFIImageLoadEvent{
		Source: source,
		Image:  image,
		Next:   next,
	}, nil
}

func efiImageFromBootFile(b *bootloader.BootFile) (sb.EFIImage, error) {
	if b.Snap == "" {
		if !osutil.FileExists(b.Path) {
			return nil, fmt.Errorf("file %s does not exist", b.Path)
		}
		return sb.FileEFIImage(b.Path), nil
	}

	snapf, err := snapfile.Open(b.Snap)
	if err != nil {
		return nil, err
	}
	return sb.SnapFileEFIImage{
		Container: snapf,
		FileName:  b.Path,
	}, nil
}
