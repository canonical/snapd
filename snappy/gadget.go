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
// package.yaml's

package snappy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/pkg"
)

// Gadget represents the structure inside the package.yaml for the gadget component
// of a gadget package type.
type Gadget struct {
	Store                Store    `yaml:"store,omitempty"`
	Hardware             Hardware `yaml:"hardware,omitempty"`
	Software             Software `yaml:"software,omitempty"`
	SkipIfupProvisioning bool     `yaml:"skip-ifup-provisioning"`
}

// Hardware describes the hardware provided by the gadget snap
type Hardware struct {
	Assign     []HardwareAssign `yaml:"assign,omitempty"`
	BootAssets *BootAssets      `yaml:"boot-assets,omitempty"`
	Bootloader string           `yaml:"bootloader,omitempty"`
}

// Store holds information relevant to the store provided by a Gadget snap
type Store struct {
	ID string `yaml:"id,omitempty"`
}

// Software describes the installed software provided by a Gadget snap
type Software struct {
	BuiltIn []string `yaml:"built-in,omitempty"`
}

// BootAssets represent all the artifacts required for booting a system
// that are particular to the board.
type BootAssets struct {
	Files    []BootAssetFiles    `yaml:"files,omitempty"`
	RawFiles []BootAssetRawFiles `yaml:"raw-files,omitempty"`
}

// BootAssetRawFiles represent all the artifacts required for booting a system
// that are particular to the board and require copying to specific sectors of
// the disk
type BootAssetRawFiles struct {
	Path   string `yaml:"path"`
	Offset string `yaml:"offset"`
}

// BootAssetFiles represent all the files required for booting a system
// that are particular to the board
type BootAssetFiles struct {
	Path   string `yaml:"path"`
	Target string `yaml:"target,omitempty"`
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

// getGadget is a convenience function to not go into the details for the business
// logic for a gadget package in every other function
var getGadget = getGadgetImpl

func getGadgetImpl() (*packageYaml, error) {
	gadgets, _ := ActiveSnapsByType(pkg.TypeGadget)
	if len(gadgets) == 1 {
		return gadgets[0].(*SnapPart).m, nil
	}

	return nil, errors.New("no gadget snap")
}

func bootAssetFilePaths() map[string]string {
	gadget, err := getGadget()
	if err != nil {
		return nil
	}

	fileList := make(map[string]string)
	gadgetPath := filepath.Join(dirs.SnapGadgetDir, gadget.Name, gadget.Version)

	for _, asset := range gadget.Gadget.Hardware.BootAssets.Files {
		orig := filepath.Join(gadgetPath, asset.Path)

		if asset.Target == "" {
			fileList[orig] = filepath.Base(orig)
		} else {
			fileList[orig] = asset.Target
		}
	}

	return fileList
}

// StoreID returns the store id setup by the gadget package or an empty string
func StoreID() string {
	gadget, err := getGadget()
	if err != nil {
		return ""
	}

	return gadget.Gadget.Store.ID
}

// IsBuiltInSoftware returns true if the package is part of the built-in software
// defined by the gadget.
func IsBuiltInSoftware(name string) bool {
	gadget, err := getGadget()
	if err != nil {
		return false
	}

	for _, builtin := range gadget.Gadget.Software.BuiltIn {
		if builtin == name {
			return true
		}
	}

	return false
}

func cleanupGadgetHardwareUdevRules(m *packageYaml) error {
	oldFiles, err := filepath.Glob(filepath.Join(dirs.SnapUdevRulesDir, fmt.Sprintf("80-snappy_%s_*.rules", m.Name)))
	if err != nil {
		return err
	}

	for _, f := range oldFiles {
		os.Remove(f)
	}

	// cleanup the additional files
	for _, h := range m.Gadget.Hardware.Assign {
		jsonAdditionalPath := filepath.Join(dirs.SnapAppArmorDir, fmt.Sprintf("%s.json.additional", h.PartID))
		err = os.Remove(jsonAdditionalPath)
		if err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove %q: %v", jsonAdditionalPath, err)
		}
	}

	return nil
}

func writeGadgetHardwareUdevRules(m *packageYaml) error {
	os.MkdirAll(dirs.SnapUdevRulesDir, 0755)

	// cleanup
	if err := cleanupGadgetHardwareUdevRules(m); err != nil {
		return err
	}
	// write new files
	for _, h := range m.Gadget.Hardware.Assign {
		rulesContent, err := h.generateUdevRuleContent()
		if err != nil {
			return err
		}
		outfile := filepath.Join(dirs.SnapUdevRulesDir, fmt.Sprintf("80-snappy_%s_%s.rules", m.Name, h.PartID))
		if err := helpers.AtomicWriteFile(outfile, []byte(rulesContent), 0644, 0); err != nil {
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
func writeApparmorAdditionalFile(m *packageYaml) error {
	if err := os.MkdirAll(dirs.SnapAppArmorDir, 0755); err != nil {
		return err
	}

	for _, h := range m.Gadget.Hardware.Assign {
		jsonAdditionalPath := filepath.Join(dirs.SnapAppArmorDir, fmt.Sprintf("%s.json.additional", h.PartID))
		if err := helpers.AtomicWriteFile(jsonAdditionalPath, []byte(apparmorAdditionalContent), 0644, 0); err != nil {
			return err
		}
	}

	return nil
}

func installGadgetHardwareUdevRules(m *packageYaml) error {
	if err := writeGadgetHardwareUdevRules(m); err != nil {
		return err
	}

	if err := writeApparmorAdditionalFile(m); err != nil {
		return err
	}

	if err := activateGadgetHardwareUdevRules(); err != nil {
		return err
	}

	return nil
}
