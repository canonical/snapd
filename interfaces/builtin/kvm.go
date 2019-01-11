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
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/kmod"
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
`

var kvmConnectedPlugUDev = []string{`KERNEL=="kvm"`}

type kvmInterface struct {
	commonInterface
}

var procCpuinfo = "/proc/cpuinfo"

func getCpuFlags() (flags string, err error) {
	buf, err := ioutil.ReadFile(procCpuinfo)
	if err != nil {
		// if we can't read cpuinfo, we want to know _why_
		return "", fmt.Errorf("error: %v", err)
	}

	pattern := []byte("\nflags\t\t:")
	idx := bytes.LastIndex(buf, pattern)
	if idx >= 0 {
		offset := idx + len(pattern)
		endidx := bytes.Index(buf[offset:], []byte("\n"))
		return string(buf[offset : endidx+offset]), nil
	}

	// if not found (which will happen on non-x86 architectures, which is ok
	// because they'd typically not have the same info over and over again),
	// return whole buffer; otherwise, return from just after the \n
	return string(buf[idx+1:]), nil
}

func (iface *kvmInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Check CPU capabilities to load suitable module
	// TODO: this only considering x86 CPUs, but some ARM, PPC and S390 CPUs also support KVM
	m := "kvm"
	cpu_flags, err := getCpuFlags()
	if err != nil {
		fmt.Println("kvm: fetching cpu info failed:", err)
	}

	if strings.Contains(cpu_flags, "vmx") {
		m = "kvm_intel"
	} else if strings.Contains(cpu_flags, "svm") {
		m = "kvm_amd"
	} else {
		// CPU appears not to support KVM extensions, fall back to bare kvm module as it appears
		// sufficient for some architectures
		fmt.Println("kvm: failed to detect CPU specific KVM support, will attempt to modprobe generic KVM support")
	}

	if err := spec.AddModule(m); err != nil {
		return nil
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
		reservedForOS:         true,
	}})
}
