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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

var (
	osReadlink     = os.Readlink
	kernelFeatures = release.SecCompActions
)

func seccompToBpfPath() string {
	// FIXME: use cmd.InternalToolPath here once:
	//   https://github.com/snapcore/snapd/pull/3512
	// is merged
	snapSeccomp := filepath.Join(dirs.DistroLibExecDir, "snap-seccomp")

	exe, err := osReadlink("/proc/self/exe")
	if err != nil {
		logger.Noticef("cannot read /proc/self/exe: %v, using default snap-seccomp command", err)
		return snapSeccomp
	}
	if !strings.HasPrefix(exe, dirs.SnapMountDir) {
		return snapSeccomp
	}

	// if we are re-execed, then snap-seccomp is at the same location
	// as snapd
	return filepath.Join(filepath.Dir(exe), "snap-seccomp")
}

// Backend is responsible for maintaining seccomp profiles for snap-confine.
type Backend struct{}

// Initialize does nothing.
func (b *Backend) Initialize() error {
	return nil
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return interfaces.SecuritySecComp
}

// Setup creates seccomp profiles specific to a given snap.
// The snap can be in developer mode to make security violations non-fatal to
// the offending application process.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func (b *Backend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	snapName := snapInfo.InstanceName()
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), snapName)
	if err != nil {
		return fmt.Errorf("cannot obtain seccomp specification for snap %q: %s", snapName, err)
	}

	// Get the snippets that apply to this snap
	content, err := b.deriveContent(spec.(*Specification), opts, snapInfo)
	if err != nil {
		return fmt.Errorf("cannot obtain expected security files for snap %q: %s", snapName, err)
	}

	glob := interfaces.SecurityTagGlob(snapName)
	dir := dirs.SnapSeccompDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for seccomp profiles %q: %s", dir, err)
	}
	_, _, err = osutil.EnsureDirState(dir, glob, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, err)
	}

	for baseName := range content {
		in := filepath.Join(dirs.SnapSeccompDir, baseName)
		out := filepath.Join(dirs.SnapSeccompDir, strings.TrimSuffix(baseName, ".src")+".bin")

		seccompToBpf := seccompToBpfPath()
		cmd := exec.Command(seccompToBpf, "compile", in, out)
		if output, err := cmd.CombinedOutput(); err != nil {
			return osutil.OutputErr(output, err)
		}
	}

	return nil
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

// deriveContent combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState.
func (b *Backend) deriveContent(spec *Specification, opts interfaces.ConfinementOptions, snapInfo *snap.Info) (content map[string]*osutil.FileState, err error) {
	for _, hookInfo := range snapInfo.Hooks {
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		securityTag := hookInfo.SecurityTag()
		addContent(securityTag, opts, spec.SnippetForTag(securityTag), content)
	}
	for _, appInfo := range snapInfo.Apps {
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		securityTag := appInfo.SecurityTag()
		addContent(securityTag, opts, spec.SnippetForTag(securityTag), content)
	}

	return content, nil
}

func addContent(securityTag string, opts interfaces.ConfinementOptions, snippetForTag string, content map[string]*osutil.FileState) {
	var buffer bytes.Buffer
	if opts.Classic && !opts.JailMode {
		// NOTE: This is understood by snap-confine
		buffer.WriteString("@unrestricted\n")
	}
	if opts.DevMode && !opts.JailMode {
		// NOTE: This is understood by snap-confine
		buffer.WriteString("@complain\n")
		if !release.SecCompSupportsAction("log") {
			buffer.WriteString("# complain mode logging unavailable\n")
		}
	}

	buffer.Write(defaultTemplate)
	buffer.WriteString(snippetForTag)

	// For systems with force-devmode we need to apply a workaround
	// to avoid failing hooks. See description in template.go for
	// more details.
	if release.ReleaseInfo.ForceDevMode() {
		buffer.WriteString(bindSyscallWorkaround)
	}

	path := fmt.Sprintf("%s.src", securityTag)
	content[path] = &osutil.FileState{
		Content: buffer.Bytes(),
		Mode:    0644,
	}
}

// NewSpecification returns an empty seccomp specification.
func (b *Backend) NewSpecification() interfaces.Specification {
	return &Specification{}
}

// SandboxFeatures returns the list of seccomp features supported by the kernel.
func (b *Backend) SandboxFeatures() []string {
	features := kernelFeatures()
	tags := make([]string, 0, len(features)+1)
	for _, feature := range features {
		// Prepend "kernel:" to apparmor kernel features to namespace them and
		// allow us to introduce our own tags later.
		tags = append(tags, "kernel:"+feature)
	}
	tags = append(tags, "bpf-argument-filtering")
	return tags
}
