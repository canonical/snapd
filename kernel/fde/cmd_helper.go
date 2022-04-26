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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

// fdeInitramfsHelperRuntimeMax is the maximum runtime a helper can execute
// XXX: what is a reasonable default here?
var fdeInitramfsHelperRuntimeMax = 2 * time.Minute

// 50 ms means we check at a frequency 20 Hz, fast enough to not hold
// up boot, but not too fast that we are hogging the CPU from the
// thing we are waiting to finish running
var fdeInitramfsHelperPollWait = 50 * time.Millisecond

// fdeInitramfsHelperPollWaitParanoiaFactor controls much longer we wait
// then fdeInitramfsHelperRuntimeMax before stopping to poll for results
var fdeInitramfsHelperPollWaitParanoiaFactor = 2

// overridden in tests
var fdeInitramfsHelperCommandExtra []string

func runFDEinitramfsHelper(name string, stdin []byte) (output []byte, err error) {
	runDir := filepath.Join(dirs.GlobalRootDir, "/run", name)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create tmp dir for %s: %v", name, err)
	}

	// delete and re-create the std{in,out,err} stream files that we use for the
	// hook to be robust against bugs where the files are created with too
	// permissive permissions or not properly deleted afterwards since the hook
	// will be invoked multiple times during the initrd and we want to be really
	// careful since the stdout file will contain the unsealed encryption key
	for _, stream := range []string{"stdin", "stdout", "stderr"} {
		streamFile := filepath.Join(runDir, name+"."+stream)
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
			return nil, fmt.Errorf("cannot create %s for %s: %v", name, stream, err)
		}
	}

	// TODO: use the new systemd.Run() interface once it supports
	//       running without dbus (i.e. supports running without --pipe)
	cmd := exec.Command(
		"systemd-run",
		"--collect",
		"--service-type=exec",
		"--quiet",
		// ensure we get some result from the hook within a
		// reasonable timeout and output from systemd if
		// things go wrong
		fmt.Sprintf("--property=RuntimeMaxSec=%s", fdeInitramfsHelperRuntimeMax),
		// Do not allow mounting, this ensures hooks in initrd
		// can not mess around with ubuntu-data.
		//
		// Note that this is not about perfect confinement, more about
		// making sure that people using the hook know that we do not
		// want them to mess around outside of just providing unseal.
		"--property=SystemCallFilter=~@mount",
		// WORKAROUNDS
		// workaround the lack of "--pipe"
		fmt.Sprintf("--property=StandardInput=file:%s/%s.stdin", runDir, name),
		// NOTE: these files are manually created above with 0600 because by
		// default systemd will create them 0644 and we want to be paranoid here
		fmt.Sprintf("--property=StandardOutput=file:%s/%s.stdout", runDir, name),
		fmt.Sprintf("--property=StandardError=file:%s/%s.stderr", runDir, name),
		// this ensures we get useful output for e.g. segfaults
		fmt.Sprintf(`--property=ExecStopPost=/bin/sh -c 'if [ "$EXIT_STATUS" = 0 ]; then touch %[1]s/%[2]s.success; else echo "service result: $SERVICE_RESULT" >%[1]s/%[2]s.failed; fi'`, runDir, name),
	)
	if fdeInitramfsHelperCommandExtra != nil {
		cmd.Args = append(cmd.Args, fdeInitramfsHelperCommandExtra...)
	}
	// "name" is what we actually need to run
	cmd.Args = append(cmd.Args, name)

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
	// "name" exists or it gets killed when it reaches the
	// fdeInitramfsHelperRuntimeMax defined above.
	//
	// However we are paranoid and exit this loop if systemd
	// did not terminate the process after twice the allocated
	// runtime
	maxLoops := int(fdeInitramfsHelperRuntimeMax/fdeInitramfsHelperPollWait) * fdeInitramfsHelperPollWaitParanoiaFactor
	for i := 0; i < maxLoops; i++ {
		switch {
		case osutil.FileExists(filepath.Join(runDir, name+".failed")):
			stderr, _ := ioutil.ReadFile(filepath.Join(runDir, name+".stderr"))
			systemdErr, _ := ioutil.ReadFile(filepath.Join(runDir, name+".failed"))
			buf := bytes.NewBuffer(stderr)
			buf.Write(systemdErr)
			return buf.Bytes(), fmt.Errorf("%s failed", name)
		case osutil.FileExists(filepath.Join(runDir, name+".success")):
			return ioutil.ReadFile(filepath.Join(runDir, name+".stdout"))
		default:
			time.Sleep(fdeInitramfsHelperPollWait)
		}
	}

	// this should never happen, the loop above should be terminated
	// via systemd
	return nil, fmt.Errorf("internal error: systemd-run did not honor RuntimeMax=%s setting", fdeInitramfsHelperRuntimeMax)
}
