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

package snappy

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/systemd"
)

// SnapFile is a local snap file that can get installed
type SnapFile struct {
	m   *packageYaml
	deb snap.File

	origin  string
	instdir string
}

// NewSnapFile loads a snap from the given snapFile
func NewSnapFile(snapFile string, origin string, unsignedOk bool) (*SnapFile, error) {
	d, err := snap.Open(snapFile)
	if err != nil {
		return nil, err
	}

	yamlData, err := d.MetaMember("package.yaml")
	if err != nil {
		return nil, err
	}

	_, err = d.MetaMember("hooks/config")
	hasConfig := err == nil

	m, err := parsePackageYamlData(yamlData, hasConfig)
	if err != nil {
		return nil, err
	}

	targetDir := dirs.SnapSnapsDir
	if origin == SideloadedOrigin {
		m.Version = helpers.NewSideloadVersion()
	}

	fullName := m.qualifiedName(origin)
	instDir := filepath.Join(targetDir, fullName, m.Version)

	return &SnapFile{
		instdir: instDir,
		origin:  origin,
		m:       m,
		deb:     d,
	}, nil
}

// Type returns the type of the SnapPart (app, gadget, ...)
func (s *SnapFile) Type() snap.Type {
	if s.m.Type != "" {
		return s.m.Type
	}

	// if not declared its a app
	return "app"
}

// Name returns the name
func (s *SnapFile) Name() string {
	return s.m.Name
}

// Version returns the version
func (s *SnapFile) Version() string {
	return s.m.Version
}

// Channel returns the channel used
func (s *SnapFile) Channel() string {
	return ""
}

// Config is used to to configure the snap
func (s *SnapFile) Config(configuration []byte) (new string, err error) {
	return "", err
}

// Date returns the last update date
func (s *SnapFile) Date() time.Time {
	return time.Time{}
}

// Description returns the description of the snap
func (s *SnapFile) Description() string {
	return ""
}

// DownloadSize returns the download size
func (s *SnapFile) DownloadSize() int64 {
	return 0
}

// InstalledSize returns the installed size
func (s *SnapFile) InstalledSize() int64 {
	return 0
}

// Hash returns the hash
func (s *SnapFile) Hash() string {
	return ""
}

// Icon returns the icon
func (s *SnapFile) Icon() string {
	return ""
}

// IsActive returns whether it is active.
func (s *SnapFile) IsActive() bool {
	return false
}

// Uninstall uninstalls the snap
func (s *SnapFile) Uninstall(pb progress.Meter) (err error) {
	return fmt.Errorf("not possible for a SnapFile")
}

// SetActive sets the snap to the new active state
func (s *SnapFile) SetActive(bool, progress.Meter) error {
	return fmt.Errorf("not possible for a SnapFile")
}

// IsInstalled returns if its installed
func (s *SnapFile) IsInstalled() bool {
	return false
}

// NeedsReboot tells if the snap needs rebooting
func (s *SnapFile) NeedsReboot() bool {
	return false
}

// Origin returns the origin
func (s *SnapFile) Origin() string {
	return s.origin
}

// Frameworks returns the list of frameworks needed by the snap
func (s *SnapFile) Frameworks() ([]string, error) {
	return s.m.Frameworks, nil
}

type transaction interface {
	do() error
	undo() error
}

type transactionManager struct {
	all  []transaction
	done []transaction
}

func (tm *transactionManager) Add(op transaction) {
	tm.all = append(tm.all, op)
}

func (tm *transactionManager) rollback() error {
	for _, op := range tm.done {
		op.undo()
	}
	return nil
}

func (tm *transactionManager) Run() error {
	for _, op := range tm.all {
		tm.done = append(tm.done, op)
		if err := op.do(); err != nil {
			tm.rollback()
			return fmt.Errorf("transaction failed: %s", err)
		}
	}

	return nil
}

// Op is a single operation that can be done and undone
type Op struct {
	doer   func() error
	undoer func() error
}

func (o *Op) do() error {
	return o.doer()
}
func (o *Op) undo() error {
	return o.undoer()
}

// Install installs the snap
func (s *SnapFile) Install(inter progress.Meter, flags InstallFlags) (name string, err error) {
	allowGadget := (flags & AllowGadget) != 0
	inhibitHooks := (flags & InhibitHooks) != 0

	// we do not Verify() the package here. This is done earlier in
	// NewSnapFile() to ensure that we do not mount/inspect
	// potentially dangerous snaps

	if err := s.CanInstall(allowGadget, inter); err != nil {
		return "", err
	}

	// the "gadget" parts are special
	if s.Type() == snap.TypeGadget {
		if err := installGadgetHardwareUdevRules(s.m); err != nil {
			return "", err
		}
	}

	fullName := QualifiedName(s)
	dataDir := filepath.Join(dirs.SnapDataDir, fullName, s.Version())

	var oldPart *SnapPart
	if currentActiveDir, _ := filepath.EvalSymlinks(filepath.Join(s.instdir, "..", "current")); currentActiveDir != "" {
		oldPart, err = NewInstalledSnapPart(filepath.Join(currentActiveDir, "meta", "package.yaml"), s.origin)
		if err != nil {
			return "", err
		}
	}

	// run
	tm := &transactionManager{}
	// create dir
	tm.Add(&Op{
		func() error { return os.MkdirAll(s.instdir, 0755) },
		func() error { return os.RemoveAll(s.instdir) },
	})
	// install the snap XXX: how to undo?
	tm.Add(&Op{
		func() error { return s.deb.Install(s.instdir) },
		func() error { return nil },
	})
	// add the mount unit
	tm.Add(&Op{
		func() error { return s.m.addSquashfsMount(s.instdir, inhibitHooks, inter) },
		func() error { return s.m.removeSquashfsMount(s.instdir, inter) },
	})
	if s.m.Type == snap.TypeKernel {
		assetsDir := ""
		tm.Add(&Op{
			func() error { var err error; assetsDir, err = extractKernelAssets(s, inter, flags); return err },
			func() error { return os.RemoveAll(assetsDir) },
		})
	}

	// deal with the data:
	//
	// if there was a previous version, stop it
	// from being active so that it stops running and can no longer be
	// started then copy the data
	//
	// otherwise just create a empty data dir
	if oldPart != nil {
		// we need to stop making it active
		tm.Add(&Op{
			func() error { return oldPart.deactivate(inhibitHooks, inter) },
			func() error { return oldPart.activate(inhibitHooks, inter) },
		})
		// and copy the data
		tm.Add(&Op{
			func() error { return copySnapData(fullName, oldPart.Version(), s.Version()) },
			func() error { return removeSnapData(fullName, s.Version()) },
		})
	} else {
		err = os.MkdirAll(dataDir, 0755)
	}

	if !inhibitHooks {
		newPart, err := newSnapPartFromYaml(filepath.Join(s.instdir, "meta", "package.yaml"), s.origin, s.m)
		if err != nil {
			return "", err
		}

		// and finally make active
		tm.Add(&Op{
			func() error { return newPart.activate(inhibitHooks, inter) },
			func() error {
				if oldPart != nil {
					return oldPart.activate(inhibitHooks, inter)
				}
				return nil
			},
		})

		// oh, one more thing: refresh the security bits
		deps, err := newPart.Dependents()
		if err != nil {
			return "", err
		}
		sysd := systemd.New(dirs.GlobalRootDir, inter)
		stopped := make(map[string]time.Duration)

		tm.Add(&Op{
			func() error {
				for _, dep := range deps {
					if !dep.IsActive() {
						continue
					}
					for _, svc := range dep.ServiceYamls() {
						serviceName := filepath.Base(generateServiceFileName(dep.m, svc))
						timeout := time.Duration(svc.StopTimeout)
						if err = sysd.Stop(serviceName, timeout); err != nil {
							inter.Notify(fmt.Sprintf("unable to stop %s; aborting install: %s", serviceName, err))
							return err
						}
						stopped[serviceName] = timeout
					}
				}
				if err := newPart.RefreshDependentsSecurity(oldPart, inter); err != nil {
					return err
				}
				return nil
			},
			func() error {
				for serviceName := range stopped {
					if e := sysd.Start(serviceName); e != nil {
						inter.Notify(fmt.Sprintf("unable to restart %s with the old %s: %s", serviceName, s.Name(), e))
					}
				}
				return nil
			},
		})

		started := make(map[string]time.Duration)
		tm.Add(&Op{
			func() error {
				for serviceName, timeout := range stopped {
					if err = sysd.Start(serviceName); err != nil {
						inter.Notify(fmt.Sprintf("unable to restart %s; aborting install: %s", serviceName, err))
						return err
					}
					started[serviceName] = timeout
				}
				return nil
			},
			func() error {
				for serviceName, timeout := range started {
					if e := sysd.Stop(serviceName, timeout); e != nil {
						inter.Notify(fmt.Sprintf("unable to stop %s with the old %s: %s", serviceName, s.Name(), e))
					}
				}
				return nil
			},
		})
	}

	if err := tm.Run(); err != nil {
		return "", err
	}

	return s.Name(), nil
}

// CanInstall checks whether the SnapPart passes a series of tests required for installation
func (s *SnapFile) CanInstall(allowGadget bool, inter interacter) error {
	if err := s.m.checkForPackageInstalled(s.Origin()); err != nil {
		return err
	}

	// verify we have a valid architecture
	if !arch.IsSupportedArchitecture(s.m.Architectures) {
		return &ErrArchitectureNotSupported{s.m.Architectures}
	}

	if err := s.m.checkForNameClashes(); err != nil {
		return err
	}

	if err := s.m.checkForFrameworks(); err != nil {
		return err
	}

	if s.Type() == snap.TypeGadget {
		if !allowGadget {
			if currentGadget, err := getGadget(); err == nil {
				if currentGadget.Name != s.Name() {
					return ErrGadgetPackageInstall
				}
			} else {
				// there should always be a gadget package now
				return ErrGadgetPackageInstall
			}
		}
	}

	curr, _ := filepath.EvalSymlinks(filepath.Join(s.instdir, "..", "current"))
	if err := s.m.checkLicenseAgreement(inter, s.deb, curr); err != nil {
		return err
	}

	return nil
}
