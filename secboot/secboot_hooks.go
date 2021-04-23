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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

var fdeHasRevealKey = fde.HasRevealKey

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

// TODO: move some of this to kernel/fde?

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
