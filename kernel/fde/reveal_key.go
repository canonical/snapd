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

package fde

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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/randutil"
)

// RevealKeyRequest carries the operation parameters to the fde-reavel-key
// helper that receives them serialized over stdin.
type RevealKeyRequest struct {
	Op string `json:"op"`

	SealedKey []byte           `json:"sealed-key,omitempty"`
	Handle    *json.RawMessage `json:"handle,omitempty"`
	// depracated for v1
	KeyName string `json:"key-name,omitempty"`

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
func runFDERevealKeyCommand(req *RevealKeyRequest) (output []byte, err error) {
	stdin, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf(`cannot build request for fde-reveal-key %q: %v`, req.Op, err)
	}

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

var runFDERevealKey = runFDERevealKeyCommand

func MockRunFDERevealKey(mock func(*RevealKeyRequest) ([]byte, error)) (restore func()) {
	oldRunFDERevealKey := runFDERevealKey
	runFDERevealKey = mock
	return func() {
		runFDERevealKey = oldRunFDERevealKey
	}
}

func LockSealedKeys() error {
	req := &RevealKeyRequest{
		Op: "lock",
	}
	if output, err := runFDERevealKey(req); err != nil {
		return fmt.Errorf(`cannot run fde-reveal-key "lock": %v`, osutil.OutputErr(output, err))
	}

	return nil
}

// RevealParams contains the parameters for fde-reveal-key reveal operation.
type RevealParams struct {
	SealedKey []byte
	Handle    *json.RawMessage `json:"handle,omitempty"`
	// V2Payload is set true if SealedKey is expected to contain a v2 payload
	// (disk key + aux key)
	V2Payload bool
}

type revealKeyResult struct {
	Key []byte `json:"key"`
}

const (
	v1keySize  = 64
	v1NoHandle = `{"v1-no-handle":true}`
)

// Reveal invokes the fde-reveal-key reveal operation.
func Reveal(params *RevealParams) (payload []byte, err error) {
	handle := params.Handle
	if params.V2Payload && handle != nil && bytes.Equal([]byte(*handle), []byte(v1NoHandle)) {
		handle = nil
	}
	req := &RevealKeyRequest{
		Op:        "reveal",
		SealedKey: params.SealedKey,
		Handle:    handle,
		// deprecated but needed for v1 hooks
		KeyName: "deprecated-" + randutil.RandomString(12),
	}
	output, err := runFDERevealKey(req)
	if err != nil {
		return nil, fmt.Errorf(`cannot run fde-reveal-key "reveal": %v`, osutil.OutputErr(output, err))
	}
	// We expect json output that fits the revealKeyResult json at
	// this point. However the "denver" project uses the old and
	// deprecated v1 API that returns raw bytes and we still need
	// to support this.
	var res revealKeyResult
	if err := json.Unmarshal(output, &res); err != nil {
		if params.V2Payload {
			// We expect a v2 payload but not having json
			// output from the hook means that either the
			// hook is buggy or we have a v1 based hook
			// (e.g. "denver" project) with v2 based json
			// data on disk. This is supported but we let
			// the higher levels unmarshaling of the
			// payload deal with the buggy case.
			return output, nil
		}
		// If the payload is not expected to be v2 and, the
		// output is not json but matches the size of the
		// "denver" project encrypton key (64 bytes) we assume
		// we deal with a v1 API.
		if len(output) != v1keySize {
			return nil, fmt.Errorf(`cannot decode fde-reveal-key "reveal" result: %v`, err)
		}
		return output, nil
	}
	return res.Key, nil
}
