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

// fdehelper implements the early boot hook that reveals the FDE key
//
// This package implements running a hook from snap-bootstrap in
// initramfs that reveals the key to continue booting. The hook is
// confined by a small amount of systemd-run sandboxing to prevent
// flagrant abuse of the hook. It is not designed as a real security
// measure and that would be pointless anyway as the hook comes from
// the kernel snap which has unlimited powers.
//
// For the initial-provision/updating of the key a "real" snap hook
// from "meta/hooks/fde-setup" is used.
package fdehelper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
)

func fdehelper(rootDir string) string {
	return filepath.Join(rootDir, "bin/fde-reveal-key")
}

// Enabled returns whether the external FDE helper should be called
func Enabled(rootDir string) bool {
	return osutil.FileExists(fdehelper(rootDir))
}

// fdehelperRuntimeMax is the maximum runtime a fdehelper can execute
func fdehelperRuntimeMax() string {
	// only used in tests (tests/main/fdehelper)
	if s := os.Getenv("DEBUG_FDEHELPER_RUNTIME_MAX"); s != "" {
		return s
	}
	// XXX: reasonable default?
	return "1m"
}

// fdehelperCmd returns a *exec.Cmd that runs the fdehelper code with
// (some) sandboxing around it. The sandbox will ensure that it's hard
// for the hook to abuse that they are called in e.g. initrd.  But it
// does not aim for perfect protection - the fdehelpers are part of the
// kernel snap/initrd so if someone wants to do mischief there are
// easier ways by hacking initrd directly.
func fdehelperCmd(rootDir string, args ...string) *exec.Cmd {
	// XXX: we could also call the systemd dbus api for this but it
	//      seems much simpler this way (but it means we need
	//      systemd-run in initrd)
	cmd := exec.Command(
		"systemd-run",
		append([]string{
			"--pipe", "--same-dir", "--wait", "--collect",
			"--service-type=exec",
			"--quiet",
			// ensure we get some result from the hook
			// within a reasonable timeout and output
			// from systemd if things go wrong
			fmt.Sprintf("--property=RuntimeMaxSec=%s", fdehelperRuntimeMax()),
			fmt.Sprintf(`--property=ExecStopPost=/bin/sh -c 'if [ "$EXIT_STATUS" != 0 ]; then echo "service result: $SERVICE_RESULT" 1>&2; fi'`),
			// do not allow mounting, this ensures hooks
			// in initrd can not mess around with
			// ubuntu-data
			"--property=SystemCallFilter=~@mount",
			// add basic sandboxing to prevent messing
			// around
			// XXX: this maybe too strict, i.e. to unseal
			//      the fdehelper may need to write to some
			//      crypto device in /dev which will not be
			//      possible with "ProtectSystem=strict"
			"--property=ProtectSystem=strict",
			fdehelper(rootDir),
		}, args...)...)
	return cmd
}

// XXX: this will also need to provide the full boot chains for the
// more complex hooks, c.f.
// https://github.com/snapcore/snapd/compare/master...cmatsuoka:spike/fde-helper-tpm#diff-9d89d75d6aa1bcb1db124c226af0d8e0R43
type RevealParams struct {
	// XXX: what about cases when only the hook can get the key from
	//      some hardware security module (HSM)?
	SealedKey        []byte `json:"sealed-key"`
	VolumeName       string `json:"volume-name"`
	SourceDevicePath string `json:"source-device-path"`
}

// Reveal reveals the key to unlocks the encrypted volume key specified
// in params.
//
// This is usually called in the inird.
func Reveal(rootDir string, params *RevealParams) (unsealedKey []byte, err error) {
	jbuf, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	cmd := fdehelperCmd(rootDir, "--unlock")
	cmd.Stdin = bytes.NewReader(jbuf)
	// provide basic things via environment to make it easier for
	// C based hooks
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("FDE_SEALED_KEY=%s", params.SealedKey),
		fmt.Sprintf("FDE_VOLUME_NAME=%s", params.VolumeName),
		fmt.Sprintf("FDE_SOURCE_DEVICE_PATH=%s", params.SourceDevicePath),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, osutil.OutputErr(out, err)
	}
	return out, nil
}
