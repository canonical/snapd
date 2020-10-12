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

package fdehook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
)

func fdeHook(kernelDir string) string {
	return filepath.Join(kernelDir, "meta/hooks/fde")
}

// Enabled returns whether the external FDE helper should be called
func Enabled(kernelDir string) bool {
	return osutil.FileExists(fdeHook(kernelDir))
}

// fdehookRuntimeMax is the maximum runtime a fdehook can execute
func fdehookRuntimeMax() string {
	// only used in tests (tests/main/fdehook)
	if s := os.Getenv("DEBUG_FDEHOOK_RUNTIME_MAX"); s != "" {
		return s
	}
	// XXX: reasonable default?
	return "5m"
}

// fdeHookCmd returns a *exec.Cmd that runs the fdehook code with
// (some) sandboxing around it. The sandbox will ensure that it's hard
// for the hook to abuse that they are called in e.g. initrd.  But it
// does not aim for perfect protection - the fdehooks are part of the
// kernel snap/initrd so if someone wants to do mischief there are
// easier ways by hacking initrd directly.
func fdeHookCmd(kernelDir string, args ...string) *exec.Cmd {
	cmd := exec.Command(
		"systemd-run",
		append([]string{
			"--pipe", "--same-dir", "--wait", "--collect",
			"--service-type=exec",
			"--quiet",
			// ensure we get some result from the hook
			// within a reasonable timeout and output
			// from systemd if things go wrong
			fmt.Sprintf("--property=RuntimeMaxSec=%s", fdehookRuntimeMax()),
			fmt.Sprintf(`--property=ExecStopPost=/bin/sh -c 'if [ "$EXIT_STATUS" != 0 ]; then echo "service result: $SERVICE_RESULT" 1>&2; fi'`),
			// do not allow mounting, this ensures hooks
			// in initrd can not mess around with
			// ubuntu-data
			"--property=SystemCallFilter=~@mount",
			// add basic sandboxing to prevent messing
			// around
			// XXX: this maybe too strict, i.e. to unseal
			//      the fdehook may need to write to some
			//      crypto device in /dev which will not be
			//      possible with "ProtectSystem=strict"
			"--property=ProtectSystem=strict",
			fdeHook(kernelDir),
		}, args...)...)
	return cmd
}

type UnlockParams struct {
	SealedKey        []byte `json:"unsealed-key"`
	VolumeName       string `json:"volume-name"`
	SourceDevicePath string `json:"source-device-path"`
	LockKeysOnFinish bool   `json:"lock-keys-on-finish"`
}

// Unlock unseals the key and unlocks the encrypted volume key specified
// in params.
//
// This is usually called in the inird.
func Unlock(kernelOrRootDir string, params *UnlockParams) (unsealedKey []byte, err error) {
	jbuf, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	cmd := fdeHookCmd(kernelOrRootDir, "--unlock")
	cmd.Stdin = bytes.NewReader(jbuf)
	// provide this via environment to make it easier for C based hooks
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("FDE_SEALED_KEY=%s", params.SealedKey),
		fmt.Sprintf("FDE_VOLUME_NAME=%s", params.VolumeName),
		fmt.Sprintf("FDE_SOURCE_DEVICE_PATH=%s", params.SourceDevicePath),
		fmt.Sprintf("FDE_LOCK_KEYS_ON_FINISH=%v", params.LockKeysOnFinish),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, osutil.OutputErr(out, err)
	}
	return out, nil
}

type InitialProvisionParams struct {
	Key string `json:"key"`
}

// InitialProvision is called on system install to initialize the
// external fde and seal the key.
func InitialProvision(kernelDir string, params *InitialProvisionParams) (sealedKey []byte, err error) {
	jbuf, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	cmd := fdeHookCmd(kernelDir, "--initial-provision")
	cmd.Stdin = bytes.NewReader(jbuf)
	// provide this via environment to make it easier for C based hooks
	cmd.Env = append(os.Environ(),
		// XXX: use string encoding?
		fmt.Sprintf("FDE_Key=%s", params.Key),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, osutil.OutputErr(out, err)
	}
	return out, nil
}

// see https://github.com/snapcore/snapd/compare/master...cmatsuoka:spike/fde-helper-tpm#diff-9d89d75d6aa1bcb1db124c226af0d8e0R43
// XXX: we will need to add Supported()
// XXX: we will need to add "Update()"
