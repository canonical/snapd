// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2019 Canonical Ltd
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

package builtin

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/strutil"
)

const kvmSummary = `allows access to the kvm device`

const kvmBaseDeclarationSlots = `
  kvm:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const kvmConnectedPlugAppArmor = `
# Description: Allow write access to kvm.
# See 'man kvm' for details.

/dev/kvm rw,

# Allow nested virtualization checks for different CPU models and architectures (where it is supported).
/sys/module/kvm_intel/parameters/nested r,
/sys/module/kvm_amd/parameters/nested r,
/sys/module/kvm_hv/parameters/nested r, # PPC64.
/sys/module/kvm/parameters/nested r, # S390.

# Allow AMD SEV checks for AMD CPU's.
/sys/module/kvm_amd/parameters/sev r,
`

var kvmConnectedPlugUDev = []string{`KERNEL=="kvm"`}

type kvmInterface struct {
	commonInterface
}

var procCpuinfo = "/proc/cpuinfo"
var flagsMatcher = regexp.MustCompile(`(?m)^flags\s+:\s+(.*)$`).FindSubmatch

func getCpuFlags() (flags []string, err error) {
	buf, err := os.ReadFile(procCpuinfo)
	if err != nil {
		// if we can't read cpuinfo, we want to know _why_
		return nil, fmt.Errorf("unable to read %v: %v", procCpuinfo, err)
	}

	// want to capture the text after 'flags:' entry
	match := flagsMatcher(buf)
	if len(match) == 0 {
		return nil, fmt.Errorf("%v does not contain a 'flags:' entry", procCpuinfo)
	}

	// match[0] has whole matching line, match[1] must exist as it has the captured text after 'flags:'
	cpu_flags := strings.Fields(string(match[1]))
	return cpu_flags, nil
}

func (iface *kvmInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Check CPU capabilities to load suitable module
	// NOTE: this only considers i386, x86_64 and amd64 CPUs, but some ARM, PPC and S390 CPUs also support KVM
	m := "kvm"
	cpu_flags, err := getCpuFlags()
	if err != nil {
		logger.Debugf("kvm: fetching cpu info failed: %v", err)
	}

	if strutil.ListContains(cpu_flags, "vmx") {
		m = "kvm_intel"
	} else if strutil.ListContains(cpu_flags, "svm") {
		m = "kvm_amd"
	} else {
		// CPU appears not to support KVM extensions, fall back to bare kvm module as it appears
		// sufficient for some architectures
		logger.Noticef("kvm: failed to detect CPU specific KVM support, will attempt to modprobe generic KVM support")
	}

	if err := spec.AddModule(m); err != nil {
		return err
	}
	return nil
}

func init() {
	registerIface(&kvmInterface{commonInterface{
		name:                  "kvm",
		summary:               kvmSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  kvmBaseDeclarationSlots,
		connectedPlugAppArmor: kvmConnectedPlugAppArmor,
		connectedPlugUDev:     kvmConnectedPlugUDev,
	}})
}
