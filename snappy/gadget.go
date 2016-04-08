// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

// TODO this should be it's own package, but depends on splitting out
// snap.yaml's

package snappy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/legacygadget"
)

func generateUdevRuleContent(hw *legacygadget.HardwareAssign) (string, error) {
	s := ""
	for _, r := range hw.Rules {
		if r.Kernel != "" {
			s += fmt.Sprintf(`KERNEL=="%v", `, r.Kernel)
		}
		if r.Subsystem != "" {
			s += fmt.Sprintf(`SUBSYSTEM=="%v", `, r.Subsystem)
		}
		if r.WithSubsystems != "" {
			s += fmt.Sprintf(`SUBSYSTEMS=="%v", `, r.WithSubsystems)
		}
		if r.WithDriver != "" {
			s += fmt.Sprintf(`DRIVER=="%v", `, r.WithDriver)
		}
		for _, a := range r.WithAttrs {
			l := strings.Split(a, "=")
			s += fmt.Sprintf(`ATTRS{%v}=="%v", `, l[0], l[1])
		}
		for _, a := range r.WithProps {
			l := strings.SplitN(a, "=", 2)
			s += fmt.Sprintf(`ENV{%v}=="%v", `, l[0], l[1])
		}
		s += fmt.Sprintf(`TAG:="snappy-assign", ENV{SNAPPY_APP}:="%s"`, hw.PartID)
		s += "\n\n"
	}

	return s, nil
}

// getGadget is a convenience function to not go into the details for the business
// logic for a gadget package in every other function
var getGadget = getGadgetImpl

func getGadgetImpl() (*snap.Info, error) {
	gadgets, _ := ActiveSnapsByType(snap.TypeGadget)
	if len(gadgets) == 1 {
		return gadgets[0].Info(), nil
	}

	return nil, errors.New("no gadget snap")
}

// StoreID returns the store id setup by the gadget package or an empty string
func StoreID() string {
	gadget, err := getGadget()
	if err != nil {
		return ""
	}

	return gadget.Legacy.Gadget.Store.ID
}

// IsBuiltInSoftware returns true if the package is part of the built-in software
// defined by the gadget.
func IsBuiltInSoftware(name string) bool {
	gadget, err := getGadget()
	if err != nil {
		return false
	}

	for _, builtin := range gadget.Legacy.Gadget.Software.BuiltIn {
		if builtin == name {
			return true
		}
	}

	return false
}

func cleanupGadgetHardwareUdevRules(s *snap.Info) error {
	oldFiles, err := filepath.Glob(filepath.Join(dirs.SnapUdevRulesDir, fmt.Sprintf("80-snappy_%s_*.rules", s.Name())))
	if err != nil {
		return err
	}

	for _, f := range oldFiles {
		os.Remove(f)
	}

	// cleanup the additional files
	for _, h := range s.Legacy.Gadget.Hardware.Assign {
		jsonAdditionalPath := filepath.Join(dirs.SnapAppArmorDir, fmt.Sprintf("%s.json.additional", h.PartID))
		err = os.Remove(jsonAdditionalPath)
		if err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove %q: %v", jsonAdditionalPath, err)
		}
	}

	return nil
}

func writeGadgetHardwareUdevRules(s *snap.Info) error {
	os.MkdirAll(dirs.SnapUdevRulesDir, 0755)

	// cleanup
	if err := cleanupGadgetHardwareUdevRules(s); err != nil {
		return err
	}
	// write new files
	for _, h := range s.Legacy.Gadget.Hardware.Assign {
		rulesContent, err := generateUdevRuleContent(&h)
		if err != nil {
			return err
		}
		outfile := filepath.Join(dirs.SnapUdevRulesDir, fmt.Sprintf("80-snappy_%s_%s.rules", s.Name(), h.PartID))
		if err := osutil.AtomicWriteFile(outfile, []byte(rulesContent), 0644, 0); err != nil {
			return err
		}
	}

	return nil
}

// var to make testing easier
var runUdevAdm = runUdevAdmImpl

func runUdevAdmImpl(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func activateGadgetHardwareUdevRules() error {
	if err := runUdevAdm("udevadm", "control", "--reload-rules"); err != nil {
		return err
	}

	return runUdevAdm("udevadm", "trigger")
}

const apparmorAdditionalContent = `{
 "write_path": [
   "/dev/**"
 ],
 "read_path": [
   "/run/udev/data/*"
 ]
}`

// writeApparmorAdditionalFile generate a $partID.json.additional file.
//
// This file grants additional access on top of the existing apparmor json
// rules. This is required for the Gadget hardware assign code because by
// default apparmor will not allow access to /dev. We grant access here
// and the ubuntu-core-launcher is then used to generate a confinement
// based on the devices cgroup.
func writeApparmorAdditionalFile(s *snap.Info) error {
	if err := os.MkdirAll(dirs.SnapAppArmorDir, 0755); err != nil {
		return err
	}

	for _, h := range s.Legacy.Gadget.Hardware.Assign {
		jsonAdditionalPath := filepath.Join(dirs.SnapAppArmorDir, fmt.Sprintf("%s.json.additional", h.PartID))
		if err := osutil.AtomicWriteFile(jsonAdditionalPath, []byte(apparmorAdditionalContent), 0644, 0); err != nil {
			return err
		}
	}

	return nil
}

func installGadgetHardwareUdevRules(s *snap.Info) error {
	if err := writeGadgetHardwareUdevRules(s); err != nil {
		return err
	}

	if err := writeApparmorAdditionalFile(s); err != nil {
		return err
	}

	if err := activateGadgetHardwareUdevRules(); err != nil {
		return err
	}

	return nil
}
