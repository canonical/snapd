// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

// Package seccomp implements integration between snapd and snap-confine around
// seccomp.
//
// Snappy creates so-called seccomp profiles for each application (for each
// snap) present in the system.  Upon each execution of snap-confine, the
// profile is read and "compiled" to an eBPF program and injected into the
// kernel for the duration of the execution of the process.
//
// There is no binary cache for seccomp, each time the launcher starts an
// application the profile is parsed and re-compiled.
//
// The actual profiles are stored in /var/lib/snappy/seccomp/bpf/*.{src,bin}.
// This directory is hard-coded in snap-confine.
package seccomp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

var (
	kernelFeatures         = seccomp.Actions
	dpkgKernelArchitecture = arch.DpkgKernelArchitecture
	releaseInfoId          = release.ReleaseInfo.ID
	releaseInfoVersionId   = release.ReleaseInfo.VersionID
	requiresSocketcall     = requiresSocketcallImpl

	snapSeccompVersionInfo = snapSeccompVersionInfoImpl
	seccompCompilerLookup  = snapdtool.InternalToolPath
)

func snapSeccompVersionInfoImpl(c Compiler) (seccomp.VersionInfo, error) {
	return c.VersionInfo()
}

type Compiler interface {
	Compile(in, out string) error
	VersionInfo() (seccomp.VersionInfo, error)
}

// Backend is responsible for maintaining seccomp profiles for snap-confine.
type Backend struct {
	snapSeccomp Compiler
	versionInfo seccomp.VersionInfo
}

// TODO: now that snap-seccomp has full support for deny-listing this
// should be replaced with something like:
//
//	~ioctl - 4294967295|TIOCSTI
//	~ioctl - 4294967295|TIOCLINUX
//
// in the default template. This requires that MaskedEq learns
// to deal with two arguments (see also https://github.com/snapcore/snapd/compare/master...mvo5:rework-seccomp-denylist-incoperate-global.bin?expand=1)
//
// globalProfileLE is generated via cmd/snap-seccomp-blacklist
var globalProfileLE = []byte{
	0x20, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x15, 0x00, 0x00, 0x04, 0x3e, 0x00, 0x00, 0xc0,
	0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x35, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x40,
	0x15, 0x00, 0x00, 0x0d, 0xff, 0xff, 0xff, 0xff, 0x15, 0x00, 0x06, 0x0c, 0x10, 0x00, 0x00, 0x00,
	0x15, 0x00, 0x00, 0x02, 0xb7, 0x00, 0x00, 0xc0, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x15, 0x00, 0x03, 0x09, 0x1d, 0x00, 0x00, 0x00, 0x15, 0x00, 0x00, 0x08, 0x15, 0x00, 0x00, 0xc0,
	0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x15, 0x00, 0x00, 0x06, 0x36, 0x00, 0x00, 0x00,
	0x20, 0x00, 0x00, 0x00, 0x1c, 0x00, 0x00, 0x00, 0x54, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x15, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x00, 0x00, 0x18, 0x00, 0x00, 0x00,
	0x15, 0x00, 0x00, 0x01, 0x12, 0x54, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, 0x01, 0x00, 0x05, 0x00,
	0x06, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x7f,
}

// globalProfileBE is generated via cmd/snap-seccomp-blacklist
var globalProfileBE = []byte{
	0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x00, 0x15, 0x00, 0x08, 0x80, 0x00, 0x00, 0x16,
	0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x15, 0x00, 0x06, 0x00, 0x00, 0x00, 0x36,
	0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x18, 0x00, 0x54, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x15, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x1c,
	0x00, 0x15, 0x00, 0x01, 0x00, 0x00, 0x54, 0x12, 0x00, 0x06, 0x00, 0x00, 0x00, 0x05, 0x00, 0x01,
	0x00, 0x06, 0x00, 0x00, 0x7f, 0xff, 0x00, 0x00,
}

// Initialize ensures that the global profile is on disk and interrogates
// libseccomp wrapper to generate a version string that will be used to
// determine if we need to recompile seccomp policy due to system
// changes outside of snapd.
func (b *Backend) Initialize(*interfaces.SecurityBackendOptions) error {
	dir := dirs.SnapSeccompDir
	fname := "global.bin"
	var globalProfile []byte
	if arch.Endian() == binary.BigEndian {
		globalProfile = globalProfileBE
	} else {
		globalProfile = globalProfileLE
	}
	content := map[string]osutil.FileState{
		fname: &osutil.MemoryFileState{Content: globalProfile, Mode: 0644},
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for seccomp profiles %q: %s", dir, err)
	}
	_, _, err := osutil.EnsureDirState(dir, fname, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize global seccomp profile: %s", err)
	}

	b.snapSeccomp, err = seccomp.NewCompiler(seccompCompilerLookup)
	if err != nil {
		return fmt.Errorf("cannot initialize seccomp profile compiler: %v", err)
	}

	versionInfo, err := snapSeccompVersionInfo(b.snapSeccomp)
	if err != nil {
		return fmt.Errorf("cannot obtain snap-seccomp version information: %v", err)
	}
	b.versionInfo = versionInfo
	return nil
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return interfaces.SecuritySecComp
}

func bpfSrcPath(srcName string) string {
	return filepath.Join(dirs.SnapSeccompDir, srcName)
}

func bpfBinPath(srcName string) string {
	return filepath.Join(dirs.SnapSeccompDir, strings.TrimSuffix(srcName, ".src")+".bin2")
}

func parallelCompile(compiler Compiler, profiles []string) error {
	if len(profiles) == 0 {
		// no profiles, nothing to do
		return nil
	}

	profilesQueue := make(chan string, len(profiles))
	numWorkers := runtime.NumCPU()
	if numWorkers >= 2 {
		numWorkers -= 1
	}
	if numWorkers > len(profiles) {
		numWorkers = len(profiles)
	}
	resultsBufferSize := numWorkers * 2
	if resultsBufferSize > len(profiles) {
		resultsBufferSize = len(profiles)
	}
	res := make(chan error, resultsBufferSize)

	// launch as many workers as we have CPUs
	for i := 0; i < numWorkers; i++ {
		go func() {
			for {
				profile, ok := <-profilesQueue
				if !ok {
					break
				}
				in := bpfSrcPath(profile)
				out := bpfBinPath(profile)
				// remove the old profile first so that we are
				// not loading it accidentally should the
				// compilation fail
				if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
					res <- err
					continue
				}

				// snap-seccomp uses AtomicWriteFile internally, on failure the
				// output file is unlinked
				if err := compiler.Compile(in, out); err != nil {
					res <- fmt.Errorf("cannot compile %s: %v", in, err)
				} else {
					res <- nil
				}
			}
		}()
	}

	for _, p := range profiles {
		profilesQueue <- p
	}
	// signal workers to exit
	close(profilesQueue)

	var firstErr error
	for i := 0; i < len(profiles); i++ {
		maybeErr := <-res
		if maybeErr != nil && firstErr == nil {
			firstErr = maybeErr
		}

	}

	// not expecting any more results
	close(res)

	if firstErr != nil {
		for _, p := range profiles {
			out := bpfBinPath(p)
			// unlink all profiles that could have been successfully
			// compiled
			os.Remove(out)
		}

	}
	return firstErr
}

// Setup creates seccomp profiles specific to a given snap.
// The snap can be in developer mode to make security violations non-fatal to
// the offending application process.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func (b *Backend) Setup(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	snapName := appSet.InstanceName()
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), appSet)
	if err != nil {
		return fmt.Errorf("cannot obtain seccomp specification for snap %q: %s", snapName, err)
	}

	// Get the snippets that apply to this snap
	content, err := b.deriveContent(spec.(*Specification), opts, appSet)
	if err != nil {
		return fmt.Errorf("cannot obtain expected security files for snap %q: %s", snapName, err)
	}

	glob := interfaces.SecurityTagGlob(snapName) + ".src"
	dir := dirs.SnapSeccompDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for seccomp profiles %q: %s", dir, err)
	}
	// There is a delicate interaction between `snap run`, `snap-confine`
	// and compilation of profiles:
	// - whenever profiles need to be rebuilt due to system-key change,
	//   `snap run` detects the system-key mismatch and waits for snapd
	//   (system key is only updated once all security backends have
	//   finished their job)
	// - whenever the binary file does not exist, `snap-confine` will poll
	//   and wait for SNAP_CONFINE_MAX_PROFILE_WAIT, if the profile does not
	//   appear in that time, `snap-confine` will fail and die
	changed, removed, err := osutil.EnsureDirState(dir, glob, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, err)
	}
	for _, c := range removed {
		err := os.Remove(bpfBinPath(c))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return parallelCompile(b.snapSeccomp, changed)
}

// Remove removes seccomp profiles of a given snap.
func (b *Backend) Remove(snapName string) error {
	glob := interfaces.SecurityTagGlob(snapName)
	_, _, err := osutil.EnsureDirState(dirs.SnapSeccompDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, err)
	}
	return nil
}

// Obtain the privilege dropping snippet
func uidGidChownSnippet(name string) (string, error) {
	tmp := strings.Replace(privDropAndChownSyscalls, "###USERNAME###", name, -1)
	return strings.Replace(tmp, "###GROUP###", name, -1), nil
}

// deriveContent combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState.
func (b *Backend) deriveContent(spec *Specification, opts interfaces.ConfinementOptions, appSet *interfaces.SnapAppSet) (content map[string]osutil.FileState, err error) {
	snapInfo := appSet.Info()
	// Some base snaps and systems require the socketcall() in the default
	// template
	addSocketcall := requiresSocketcall(snapInfo.Base)

	var uidGidChownSyscalls bytes.Buffer
	if len(snapInfo.SystemUsernames) == 0 {
		uidGidChownSyscalls.WriteString(barePrivDropSyscalls)
	} else {
		for _, id := range snapInfo.SystemUsernames {
			syscalls, err := uidGidChownSnippet(id.Name)
			if err != nil {
				return nil, fmt.Errorf(`cannot calculate syscalls for "%s": %s`, id, err)
			}
			uidGidChownSyscalls.WriteString(syscalls)
		}
		uidGidChownSyscalls.WriteString(rootSetUidGidSyscalls)
	}

	for _, hookInfo := range snapInfo.Hooks {
		if content == nil {
			content = make(map[string]osutil.FileState)
		}
		securityTag := hookInfo.SecurityTag()

		path := securityTag + ".src"
		content[path] = &osutil.MemoryFileState{
			Content: generateContent(opts, spec.SnippetForTag(securityTag), addSocketcall, b.versionInfo, uidGidChownSyscalls.String()),
			Mode:    0644,
		}
	}
	for _, appInfo := range snapInfo.Apps {
		if content == nil {
			content = make(map[string]osutil.FileState)
		}
		securityTag := appInfo.SecurityTag()
		path := securityTag + ".src"
		content[path] = &osutil.MemoryFileState{
			Content: generateContent(opts, spec.SnippetForTag(securityTag), addSocketcall, b.versionInfo, uidGidChownSyscalls.String()),
			Mode:    0644,
		}
	}

	// TODO: something with component hooks will need to happen here

	return content, nil
}

func generateContent(opts interfaces.ConfinementOptions, snippetForTag string, addSocketcall bool, versionInfo seccomp.VersionInfo, uidGidChownSyscalls string) []byte {
	var buffer bytes.Buffer

	if versionInfo != "" {
		buffer.WriteString("# snap-seccomp version information:\n")
		fmt.Fprintf(&buffer, "# %s\n", versionInfo)
	}

	if opts.Classic && !opts.JailMode {
		// NOTE: This is understood by snap-confine
		buffer.WriteString("@unrestricted\n")
	}
	if opts.DevMode && !opts.JailMode {
		// NOTE: This is understood by snap-confine
		buffer.WriteString("@complain\n")
		if !seccomp.SupportsAction("log") {
			buffer.WriteString("# complain mode logging unavailable\n")
		}
	}

	buffer.Write(defaultTemplate)
	buffer.WriteString(snippetForTag)
	buffer.WriteString(uidGidChownSyscalls)

	// For systems with partial or missing AppArmor support we need to apply
	// a workaround to avoid failing hooks. See description in template.go
	// for more details.
	if apparmor.ProbedLevel() != apparmor.Full {
		buffer.WriteString(bindSyscallWorkaround)
	}

	if addSocketcall {
		buffer.WriteString(socketcallSyscallDeprecated)
	}

	return buffer.Bytes()
}

// NewSpecification returns an empty seccomp specification.
func (b *Backend) NewSpecification(appSet *interfaces.SnapAppSet) interfaces.Specification {
	return &Specification{appSet: appSet}
}

// SandboxFeatures returns the list of seccomp features supported by the kernel
// and userspace.
func (b *Backend) SandboxFeatures() []string {
	features := kernelFeatures()
	tags := make([]string, 0, len(features)+1)
	for _, feature := range features {
		// Prepend "kernel:" to apparmor kernel features to namespace
		// them.
		tags = append(tags, "kernel:"+feature)
	}
	tags = append(tags, "bpf-argument-filtering")

	if res, err := b.versionInfo.HasFeature("bpf-actlog"); err == nil && res {
		tags = append(tags, "bpf-actlog")
	}

	return tags
}

// Determine if the system requires the use of socketcall(). Factors:
//   - if the kernel architecture is amd64, armhf or arm64, do not require
//     socketcall (unused on these architectures)
//   - if the kernel architecture is i386 or s390x
//   - if the kernel is < 4.3, force the use of socketcall()
//   - for backwards compatibility, if the system is Ubuntu 14.04 or lower,
//     force use of socketcall()
//   - for backwards compatibility, if the base snap is unspecified, "core" or
//     "core16", then force use of socketcall()
//   - otherwise (ie, if new enough kernel, not 14.04, and a non-16 base
//     snap), don't force use of socketcall()
//   - if the kernel architecture is not any of the above, force the use of
//     socketcall()
func requiresSocketcallImpl(baseSnap string) bool {
	switch dpkgKernelArchitecture() {
	case "i386", "s390x":
		// glibc sysdeps/unix/sysv/linux/i386/kernel-features.h and
		// sysdeps/unix/sysv/linux/s390/kernel-features.h added the
		// individual socket syscalls in 4.3.
		if cmp, _ := strutil.VersionCompare(osutil.KernelVersion(), "4.3"); cmp < 0 {
			return true
		}

		// For now, on 14.04, always require socketcall()
		if releaseInfoId == "ubuntu" {
			if cmp, _ := strutil.VersionCompare(releaseInfoVersionId, "14.04"); cmp <= 0 {
				return true
			}
		}

		// Detect when the base snap requires the use of socketcall().
		//
		// TODO: eventually try to auto-detect this. For now, err on
		// the side of security and only require it for base snaps
		// where we know we want it added. Technically, core16's glibc
		// is new enough, but it always had socketcall in the template,
		// so ensure backwards compatibility.
		if baseSnap == "" || baseSnap == "core" || baseSnap == "core16" {
			return true
		}

		// If none of the above, we don't need the syscall
		return false
	case "powerpc":
		// glibc's sysdeps/unix/sysv/linux/powerpc/kernel-features.h
		// states that the individual syscalls are all available as of
		// 2.6.37. snapd isn't expected to run on these kernels so just
		// default to unneeded.
		return false
	case "sparc", "sparc64":
		// glibc's sysdeps/unix/sysv/linux/sparc/kernel-features.h
		// indicates that socketcall() is used and the individual
		// syscalls are undefined.
		return true
	default:
		// amd64, arm64, armhf, ppc64el, etc
		// glibc's sysdeps/unix/sysv/linux/kernel-features.h says that
		// __ASSUME_SOCKETCALL will be defined on archs that need it.
		// Modern architectures do not implement the socketcall()
		// syscall and all the older architectures except sparc (see
		// above) have introduced the individual syscalls, so default
		// to unneeded.
		return false
	}

	// If we got here, something went wrong, but if the code above changes
	// the compiler will complain about the lack of 'return'.
}

// MockSnapSeccompVersionInfo is for use in tests only.
func MockSnapSeccompVersionInfo(versionInfo string) (restore func()) {
	old := snapSeccompVersionInfo
	snapSeccompVersionInfo = func(c Compiler) (seccomp.VersionInfo, error) {
		return seccomp.VersionInfo(versionInfo), nil
	}
	return func() {
		snapSeccompVersionInfo = old
	}
}
