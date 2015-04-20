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
// package.yaml's

package snappy

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OEM represents the structure inside the package.yaml for the oem component
// of an oem package type.
type OEM struct {
	Store struct {
		ID string `yaml:"id,omitempty"`
	} `yaml:"store,omitempty"`
	Hardware struct {
		Assign []HardwareAssign `yaml:"assign,omitempty"`
	} `yaml:"hardware,omitempty"`
	Software struct {
		BuiltIn []string `yaml:"built-in,omitempty"`
	} `yaml:"software,omitempty"`
}

// HardwareAssign describes the hardware a app can use
type HardwareAssign struct {
	PartID string `yaml:"part-id,omitempty"`
	Rules  []struct {
		Kernel         string   `yaml:"kernel,omitempty"`
		Subsystem      string   `yaml:"subsystem,omitempty"`
		WithSubsystems string   `yaml:"with-subsystems,omitempty"`
		WithDriver     string   `yaml:"with-driver,omitempty"`
		WithAttrs      []string `yaml:"with-attrs,omitempty"`
		WithProps      []string `yaml:"with-props,omitempty"`
	} `yaml:"rules,omitempty"`
}

func (hw *HardwareAssign) generateUdevRuleContent() (string, error) {
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

// getOem is a convenience function to not go into the details for the business
// logic for an oem package in every other function
func getOem() (*packageYaml, error) {
	oems, _ := ActiveSnapsByType(SnapTypeOem)
	if len(oems) == 1 {
		return oems[0].(*SnapPart).m, nil
	}

	return nil, errors.New("no oem snap")
}

// StoreID returns the store id setup by the oem package or an empty string
func StoreID() string {
	oem, err := getOem()
	if err != nil {
		return ""
	}

	return oem.OEM.Store.ID
}

// IsBuiltInSoftware returns true if the package is part of the built-in software
// defined by the oem.
func IsBuiltInSoftware(name string) bool {
	oem, err := getOem()
	if err != nil {
		return false
	}

	for _, builtin := range oem.OEM.Software.BuiltIn {
		if builtin == name {
			return true
		}
	}

	return false
}

func cleanupOemHardwareUdevRules(m *packageYaml) error {
	oldFiles, err := filepath.Glob(filepath.Join(snapUdevRulesDir, fmt.Sprintf("80-snappy_%s_*.rules", m.Name)))
	if err != nil {
		return err
	}

	for _, f := range oldFiles {
		os.Remove(f)
	}

	return nil
}

func writeOemHardwareUdevRules(m *packageYaml) error {
	// cleanup
	if err := cleanupOemHardwareUdevRules(m); err != nil {
		return err
	}
	// write new files
	for _, h := range m.OEM.Hardware.Assign {
		rulesContent, err := h.generateUdevRuleContent()
		if err != nil {
			return err
		}
		outfile := filepath.Join(snapUdevRulesDir, fmt.Sprintf("80-snappy_%s_%s.rules", m.Name, h.PartID))
		if err := ioutil.WriteFile(outfile, []byte(rulesContent), 0644); err != nil {
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

func activateOemHardwareUdevRules(m *packageYaml) error {
	if err := runUdevAdm("udevadm", "control", "--reload-rules"); err != nil {
		return err
	}

	return runUdevAdm("udevadm", "trigger")
}

func installOemHardwareUdevRules(m *packageYaml) error {
	if err := writeOemHardwareUdevRules(m); err != nil {
		return err
	}

	if err := activateOemHardwareUdevRules(m); err != nil {
		return err
	}

	return nil
}
