// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package apparmor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

// This file implements runtime detection of the AppArmor "file_perm
// class=net" kernel bug (LP: #2169038) that affects profiles compiled by an
// AppArmor 5 parser against abi/5.0: on affected kernels, socket
// file-descriptor I/O (write/read, as opposed to send/recv) is wrongly
// mediated as a file operation and denied with EACCES. When the bug is
// detected, snapd downgrades the ABI it compiles profiles with from 5.0 to
// 4.0.
//
// The probe has three states, recorded in the marker file
// /run/snapd/apparmor-5-abi-downgrade (tmpfs, so per-boot and in-memory
// only):
//
//	absent     probe needed: nobody has run the probe this boot
//	"0"        probed, bug absent: use abi/5.0
//	"1"        probed, bug present: downgrade to abi/4.0
//
// Both snapd.apparmor.service and snapd.service call
// EnsureAppArmor5NetworkBugProbe; importing the package has no side effects
// and AppArmorParser only consults cheap cached state.

// networkBugMarkerName is the name of the marker file under dirs.SnapRunDir.
const networkBugMarkerName = "apparmor-5-abi-downgrade"

// networkBugProfileTemplate is the AppArmor profile the probe runs under;
// "###NAME###" is replaced with a unique per-probe profile name. See the
// comment inside the template for why it grants file and inet stream
// networking and why it carries no abi line.
//
// This is a const rather than a //go:embed'ed file because some package
// builds (old dh-golang) copy only *.go files into the build tree, which
// breaks the embed.
const networkBugProfileTemplate = `# Probe profile for detecting the AppArmor "file_perm class=net" bug
# (LP: #2169038): on affected kernels (e.g. 6.14.0-37, 6.17.0-19/-23),
# socket file-descriptor I/O (write/read, as opposed to send/recv) is
# wrongly mediated as a file operation when the profile is compiled by
# an AppArmor 5 parser against abi/5.0. Fixed kernels (e.g. 6.17.0-42)
# treat write() on a connected socket as the network "send" permission
# and allow it.
#
# The profile grants all file rules (so the Go stub and the freestanding
# helper can exec and run) and inet stream networking. The helper itself
# is statically linked and freestanding, so it needs no file rules of
# its own; the "file," rule exists for the Go stub that execs it, and a
# follow-up could test whether it can be dropped or narrowed.
# On a correct kernel the probe's loopback exchange completes; on an
# affected kernel the client's write() is denied with EACCES and the
# audit log shows
#   operation="file_perm" class="net" ... requested="send" denied="send"
#
# The profile intentionally carries no abi <...> line: the ABI is
# selected by the parser's --policy-features flag so that the probe
# tests exactly the ABI snapd would compile real profiles with.
#
# ###NAME### is replaced with the unique, per-probe profile name at
# runtime. Profile names are global kernel state, so a unique name per
# probe makes concurrent loads/removes from different snapd processes
# race-free: they operate on different profiles, and the kernel
# serializes the loads/removes themselves.
profile ###NAME### {
	file,
	network inet stream,
}
`

const networkBugProfileNamePrefix = "snapd-kernel-probe-network-bug"

// For mocking in tests.
var (
	osExecutable          = os.Executable
	osReadFile            = os.ReadFile
	osutilAtomicWriteFile = osutil.AtomicWriteFile
)

// networkBugProbeRunner runs the probe under the given AppArmor profile and
// reports the probe's outcome. It is a variable to allow mocking in tests.
// The parser is the one selected during the eligibility check, so a
// concurrent probe verdict from another process cannot change the ABI the
// probe profile is compiled against mid-flight.
var networkBugProbeRunner = runNetworkBugProbe

var (
	networkBugProbeMu   sync.Mutex
	networkBugProbeDone bool
	// networkBugProbeDowngrade is an atomic rather than a plain bool
	// guarded by networkBugProbeMu because AppArmorParser reads it via
	// networkBugForcedAbi40 without holding the mutex (it may already be
	// held by ensureAppArmor5NetworkBugProbeLocked, so locking would
	// self-deadlock). All writes go through atomic.StoreUint32, all
	// reads through atomic.LoadUint32, so concurrent probing and
	// profile compilation cannot race. It is an integer atomic rather
	// than atomic.Bool to keep building with go 1.18.
	networkBugProbeDowngrade uint32
)

// networkBugMarkerPath returns the path of the marker file recording the
// probe verdict.
func networkBugMarkerPath() string {
	return filepath.Join(dirs.SnapRunDir, networkBugMarkerName)
}

// networkBugForcedAbi40 reports whether snapd should compile profiles with
// abi/4.0 instead of abi/5.0 because the running kernel is affected by the
// "file_perm class=net" bug. It consults the in-process probe verdict first
// and falls back to the marker file written by whichever snapd process
// probed first this boot. The in-process verdict is sticky for the process
// lifetime by design: the bug is a per-boot kernel property, so the verdict
// cannot legitimately change while the process runs. It performs no probing
// itself and is safe to call from AppArmorParser on every profile
// compilation.
func networkBugForcedAbi40() bool {
	if atomic.LoadUint32(&networkBugProbeDowngrade) == 1 {
		return true
	}
	content, err := osReadFile(networkBugMarkerPath())
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(content)) == "1"
}

// writeNetworkBugVerdict records the probe verdict in the marker file,
// creating /run/snapd if needed. The write is atomic; concurrent writers
// always write the same verdict so no locking is required.
func writeNetworkBugVerdict(buggy bool) error {
	if err := osMkdirAll(dirs.SnapRunDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %v", dirs.SnapRunDir, err)
	}
	verdict := "0\n"
	if buggy {
		verdict = "1\n"
	}
	return osutilAtomicWriteFile(networkBugMarkerPath(), []byte(verdict), 0644, 0)
}

// networkBugProbeProfile returns the probe profile source with a unique
// per-process profile name substituted in, along with the profile name.
func networkBugProbeProfile() (source, name string) {
	name = fmt.Sprintf("%s.%d", networkBugProfileNamePrefix, os.Getpid())
	// Replace every occurrence: the template mentions ###NAME### in a
	// comment before the actual profile line, and substituting only the
	// first would leave the profile named literally "###NAME###".
	return strings.ReplaceAll(networkBugProfileTemplate, "###NAME###", name), name
}

// runNetworkBugProbe compiles the probe profile with the given parser, runs
// the probe under it and removes the profile. It returns whether the bug was
// detected. The parser must be the one selected during the eligibility check
// (with abi/5.0): capturing it here keeps the probe testing the ABI that was
// in effect when the probe started, even if another process records a
// downgrade verdict while this probe is in flight.
func runNetworkBugProbe(parser *exec.Cmd) (bool, error) {
	source, name := networkBugProbeProfile()

	// The probe profile must not outlive the probe. Use a standard
	// temporary file and always remove the profile from the kernel when
	// done.
	tmp, err := os.CreateTemp("", "snapd-kernel-probe-*.aap")
	if err != nil {
		return false, fmt.Errorf("cannot create probe profile file: %v", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(source); err != nil {
		tmp.Close()
		return false, fmt.Errorf("cannot write probe profile: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("cannot write probe profile: %v", err)
	}

	// The parser args deliberately reflect the ABI selection that would
	// be used for real profiles right now: abi/5.0, when the probe runs.
	// That is exactly the configuration being tested; a downgrade verdict
	// recorded earlier this boot short-circuits ensureAppArmor5NetworkBugProbe
	// before the probe is reached. Copy the args before appending to
	// avoid sharing the backing array of parser.Args.
	parserArgs := append([]string{}, parser.Args[1:]...)

	// Load the profile, replacing any stale copy of our unique name (there
	// should be none). --skip-cache keeps this out of the parser cache so
	// probing never interferes with real profile compilation.
	load := exec.Command(parser.Args[0], append(parserArgs, "--skip-cache", "--replace", tmpPath)...)
	if out, err := load.CombinedOutput(); err != nil {
		return false, fmt.Errorf("cannot load probe profile: %v: %s", err, out)
	}

	// Unload the profile when done. A unique name makes load/remove of
	// concurrent probes race-free.
	defer func() {
		remove := exec.Command(parser.Args[0], append(parserArgs, "--skip-cache", "--remove", tmpPath)...)
		if out, err := remove.CombinedOutput(); err != nil {
			logger.Debugf("cannot remove probe profile %q: %v: %s", name, err, out)
		}
	}()

	// Run the probe confined. The "snapd-kernel-probe" subcommand writes
	// "exec <profile>" to /proc/self/attr/exec and execs the static helper,
	// so the helper performs the socket exercise under the probe profile.
	// Set argv[0] to "snapd" explicitly: internal-tool dispatch in
	// cmd/snapd keys off the basename of argv[0], but os.Executable may
	// resolve to a renamed binary (e.g. "snapd-fips" in the FIPS snap)
	// which would fall through to cliMain and leave the probe
	// inconclusive.
	self, err := osExecutable()
	if err != nil {
		return false, fmt.Errorf("cannot locate own executable: %v", err)
	}
	cmd := exec.Command(self, "snapd-kernel-probe", "apparmor-5-network-bug", name)
	cmd.Args[0] = "snapd"
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr == nil {
		// Exit 0: the loopback exchange completed, kernel is not affected.
		return false, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		// The probe exits with the errno of the first failed operation
		// and reports it as "FAIL <op> <errno>" on stderr. Only EACCES
		// on the socket write/read is the bug: any earlier operation
		// (socket, bind, connect, ...) can fail with EACCES for
		// unrelated policy reasons, so those must remain inconclusive
		// rather than downgrade a healthy kernel.
		msg := strings.TrimSpace(stderr.String())
		if exitErr.ExitCode() == int(syscall.EACCES) && (msg == "FAIL write 13" || msg == "FAIL read 13") {
			logger.Noticef("detected AppArmor 5.0 network mediation bug (probe under profile %q denied socket I/O with EACCES: %s)", name, msg)
			return true, nil
		}
		return false, fmt.Errorf("probe failed with exit status %d: %s", exitErr.ExitCode(), msg)
	}
	return false, fmt.Errorf("cannot run probe: %v", runErr)
}

// EnsureAppArmor5NetworkBugProbe decides whether the running kernel
// mis-mediates socket read/write under abi/5.0-compiled profiles, and
// downgrades the ABI snapd compiles profiles with from 5.0 to 4.0
// accordingly. The verdict is shared between snapd processes via the
// marker file under /run/snapd so only the first caller each boot pays the
// probe cost.
//
// The probe only runs when snapd would actually compile profiles with an
// AppArmor 5 parser against abi/5.0; on any other system there is nothing to
// work around and no verdict is recorded, so a later snapd refresh that
// introduces abi/5.0 re-probes.
//
// A probe that fails for reasons other than the bug (parser errors, missing
// helper, unexpected exit status) is inconclusive: no verdict is recorded
// and no downgrade happens, so a broken probe never changes behavior on
// healthy systems. Only conclusive outcomes are remembered for the process
// lifetime; an inconclusive probe is retried by the next
// EnsureAppArmor5NetworkBugProbe call, which in practice means the next
// process start (both snapd.apparmor.service and snapd.service probe at
// startup), so a transient failure (e.g. the parser being briefly
// unavailable during a package upgrade) cannot leave an affected kernel
// undetected across a restart.
func EnsureAppArmor5NetworkBugProbe() error {
	networkBugProbeMu.Lock()
	defer networkBugProbeMu.Unlock()
	if networkBugProbeDone {
		// A conclusive outcome was reached earlier in this process.
		return nil
	}
	return ensureAppArmor5NetworkBugProbeLocked()
}

func ensureAppArmor5NetworkBugProbeLocked() error {
	// Consult the marker file first: if another snapd process already
	// probed this boot, adopt its verdict without re-probing.
	content, err := osReadFile(networkBugMarkerPath())
	if err == nil {
		switch strings.TrimSpace(string(content)) {
		case "1":
			atomic.StoreUint32(&networkBugProbeDowngrade, 1)
		}
		networkBugProbeDone = true
		return nil
	}
	if !os.IsNotExist(err) {
		logger.Debugf("cannot read %s: %v", networkBugMarkerPath(), err)
	}

	// Only probe when an AppArmor 5 parser is in use with abi/5.0
	// selected; anything else is unaffected by the bug. No verdict is
	// recorded in this case so that a later snapd refresh that introduces
	// abi/5.0 re-probes. This is conclusive for the process lifetime: the
	// parser and ABI selection cannot change while the process runs.
	parser, _, err := AppArmorParser()
	if err != nil {
		// No parser at all: nothing to do.
		networkBugProbeDone = true
		return nil
	}
	usesAbi50 := false
	for _, arg := range parser.Args {
		if strings.HasSuffix(arg, "/abi/5.0") {
			usesAbi50 = true
			break
		}
	}
	if !usesAbi50 {
		networkBugProbeDone = true
		return nil
	}

	buggy, err := networkBugProbeRunner(parser)
	if err != nil {
		// Inconclusive: do not set networkBugProbeDone so the next
		// caller (in practice the next process start) re-attempts the
		// probe.
		return err
	}
	if err := writeNetworkBugVerdict(buggy); err != nil {
		// Failing to record the verdict only means the next process
		// re-probes; do not fail the caller over it.
		logger.Debugf("cannot record AppArmor 5.0 network bug probe verdict: %v", err)
	}
	if buggy {
		logger.Noticef("kernel is affected by the AppArmor 5.0 network mediation bug; downgrading snapd AppArmor profiles to abi/4.0")
		atomic.StoreUint32(&networkBugProbeDowngrade, 1)
	}
	networkBugProbeDone = true
	return nil
}
