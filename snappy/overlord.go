// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/squashfs"
	"github.com/ubuntu-core/snappy/systemd"
)

// Overlord is responsible for the overall system state.
type Overlord struct {
}

// Install installs the given snap file to the system.
//
// It returns the local snap file or an error
func (o *Overlord) Install(snapFilePath string, origin string, flags InstallFlags, meter progress.Meter) (sp *SnapPart, err error) {
	allowGadget := (flags & AllowGadget) != 0
	inhibitHooks := (flags & InhibitHooks) != 0
	allowUnauth := (flags & AllowUnauthenticated) != 0

	s, err := NewSnapFile(snapFilePath, origin, allowUnauth)
	if err != nil {
		return nil, fmt.Errorf("can not open %s: %s", snapFilePath, err)
	}

	// we do not Verify() the package here. This is done earlier in
	// NewSnapFile() to ensure that we do not mount/inspect
	// potentially dangerous snaps
	if err := canInstall(s, allowGadget, meter); err != nil {
		return nil, err
	}

	// the "gadget" parts are special
	if s.Type() == snap.TypeGadget {
		if err := installGadgetHardwareUdevRules(s.m); err != nil {
			return nil, err
		}
	}

	fullName := QualifiedName(s)
	dataDir := filepath.Join(dirs.SnapDataDir, fullName, s.Version())

	var oldPart *SnapPart
	if currentActiveDir, _ := filepath.EvalSymlinks(filepath.Join(s.instdir, "..", "current")); currentActiveDir != "" {
		oldPart, err = NewInstalledSnapPart(filepath.Join(currentActiveDir, "meta", "snap.yaml"), s.origin)
		if err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(s.instdir, 0755); err != nil {
		logger.Noticef("Can not create %q: %v", s.instdir, err)
		return nil, err
	}

	// if anything goes wrong here we cleanup
	defer func() {
		if err != nil {
			if e := os.RemoveAll(s.instdir); e != nil && !os.IsNotExist(e) {
				logger.Noticef("Failed to remove %q: %v", s.instdir, e)
			}
		}
	}()

	// we need to call the external helper so that we can reliable drop
	// privs
	if err := s.deb.Install(s.instdir); err != nil {
		return nil, err
	}

	// generate the mount unit for the squashfs
	if err := addSquashfsMount(s.m, s.instdir, inhibitHooks, meter); err != nil {
		return nil, err
	}
	// if anything goes wrong we ensure we stop
	defer func() {
		if err != nil {
			if e := removeSquashfsMount(s.m, s.instdir, meter); e != nil {
				logger.Noticef("Failed to remove mount unit for  %s: %s", fullName, e)
			}
		}
	}()

	// FIXME: special handling is bad 'mkay
	if s.m.Type == snap.TypeKernel {
		if err := extractKernelAssets(s, meter, flags); err != nil {
			return nil, fmt.Errorf("failed to install kernel %s", err)
		}
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
		err = oldPart.deactivate(inhibitHooks, meter)
		defer func() {
			if err != nil {
				if cerr := oldPart.activate(inhibitHooks, meter); cerr != nil {
					logger.Noticef("Setting old version back to active failed: %v", cerr)
				}
			}
		}()
		if err != nil {
			return nil, err
		}

		err = copySnapData(fullName, oldPart.Version(), s.Version())
	} else {
		err = os.MkdirAll(dataDir, 0755)
	}

	defer func() {
		if err != nil {
			if cerr := removeSnapData(fullName, s.Version()); cerr != nil {
				logger.Noticef("When cleaning up data for %s %s: %v", s.Name(), s.Version(), cerr)
			}
		}
	}()

	if err != nil {
		return nil, err
	}

	if !inhibitHooks {
		newPart, err := newSnapPartFromYaml(filepath.Join(s.instdir, "meta", "snap.yaml"), s.origin, s.m)
		if err != nil {
			return nil, err
		}

		// and finally make active
		err = newPart.activate(inhibitHooks, meter)
		defer func() {
			if err != nil && oldPart != nil {
				if cerr := oldPart.activate(inhibitHooks, meter); cerr != nil {
					logger.Noticef("When setting old %s version back to active: %v", s.Name(), cerr)
				}
			}
		}()
		if err != nil {
			return nil, err
		}

		// oh, one more thing: refresh the security bits
		deps, err := newPart.Dependents()
		if err != nil {
			return nil, err
		}

		sysd := systemd.New(dirs.GlobalRootDir, meter)
		stopped := make(map[string]time.Duration)
		defer func() {
			if err != nil {
				for serviceName := range stopped {
					if e := sysd.Start(serviceName); e != nil {
						meter.Notify(fmt.Sprintf("unable to restart %s with the old %s: %s", serviceName, s.Name(), e))
					}
				}
			}
		}()

		for _, dep := range deps {
			if !dep.IsActive() {
				continue
			}
			for _, svc := range dep.Apps() {
				if svc.Daemon == "" {
					continue
				}
				serviceName := filepath.Base(generateServiceFileName(dep.m, svc))
				timeout := time.Duration(svc.StopTimeout)
				if err = sysd.Stop(serviceName, timeout); err != nil {
					meter.Notify(fmt.Sprintf("unable to stop %s; aborting install: %s", serviceName, err))
					return nil, err
				}
				stopped[serviceName] = timeout
			}
		}

		if err := newPart.RefreshDependentsSecurity(oldPart, meter); err != nil {
			return nil, err
		}

		started := make(map[string]time.Duration)
		defer func() {
			if err != nil {
				for serviceName, timeout := range started {
					if e := sysd.Stop(serviceName, timeout); e != nil {
						meter.Notify(fmt.Sprintf("unable to stop %s with the old %s: %s", serviceName, s.Name(), e))
					}
				}
			}
		}()
		for serviceName, timeout := range stopped {
			if err = sysd.Start(serviceName); err != nil {
				meter.Notify(fmt.Sprintf("unable to restart %s; aborting install: %s", serviceName, err))
				return nil, err
			}
			started[serviceName] = timeout
		}
	}

	return newSnapPartFromYaml(filepath.Join(s.instdir, "meta", "snap.yaml"), s.origin, s.m)
}

// CanInstall checks whether the SnapPart passes a series of tests required for installation
func canInstall(s *SnapFile, allowGadget bool, inter interacter) error {
	if err := checkForPackageInstalled(s.m, s.Origin()); err != nil {
		return err
	}

	// verify we have a valid architecture
	if !arch.IsSupportedArchitecture(s.m.Architectures) {
		return &ErrArchitectureNotSupported{s.m.Architectures}
	}

	if err := checkForFrameworks(s.m); err != nil {
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
	if err := checkLicenseAgreement(s.m, inter, s.deb, curr); err != nil {
		return err
	}

	return nil
}

// Uninstall removes the given local snap from the system.
//
// It returns an error on failure
func (o *Overlord) Uninstall(s *SnapPart, meter progress.Meter) error {
	// Gadget snaps should not be removed as they are a key
	// building block for Gadgets. Prunning non active ones
	// is acceptible.
	if s.m.Type == snap.TypeGadget && s.IsActive() {
		return ErrPackageNotRemovable
	}

	// You never want to remove an active kernel or OS
	if (s.m.Type == snap.TypeKernel || s.m.Type == snap.TypeOS) && s.IsActive() {
		return ErrPackageNotRemovable
	}

	if IsBuiltInSoftware(s.Name()) && s.IsActive() {
		return ErrPackageNotRemovable
	}

	deps, err := s.DependentNames()
	if err != nil {
		return err
	}
	if len(deps) != 0 {
		return ErrFrameworkInUse(deps)
	}

	if err := s.deactivate(false, meter); err != nil && err != ErrSnapNotActive {
		return err
	}

	// ensure mount unit stops
	if err := removeSquashfsMount(s.m, s.basedir, meter); err != nil {
		return err
	}

	err = os.RemoveAll(s.basedir)
	if err != nil {
		return err
	}

	// best effort(?)
	os.Remove(filepath.Dir(s.basedir))

	// remove the snap
	if err := os.RemoveAll(squashfs.BlobPath(s.basedir)); err != nil {
		return err
	}

	// remove the kernel assets (if any)
	if s.m.Type == snap.TypeKernel {
		if err := removeKernelAssets(s, meter); err != nil {
			logger.Noticef("removing kernel assets failed with %s", err)
		}
	}

	return RemoveAllHWAccess(QualifiedName(s))
}

// SetActive sets the active state of the given snap
//
// It returns an error on failure
func (o *Overlord) SetActive(s *SnapPart, active bool, meter progress.Meter) error {
	if active {
		return s.activate(false, meter)
	}

	return s.deactivate(false, meter)
}

// Configure configures the given snap
//
// It returns an error on failure
func (o *Overlord) Configure(s *SnapPart, configuration []byte) ([]byte, error) {
	if s.m.Type == snap.TypeOS {
		return coreConfig(configuration)
	}

	return snapConfig(s.basedir, s.origin, configuration)
}

// Installed returns the installed snaps from this repository
func (o *Overlord) Installed() ([]*SnapPart, error) {
	globExpr := filepath.Join(dirs.SnapSnapsDir, "*", "*", "meta", "snap.yaml")
	parts, err := o.partsForGlobExpr(globExpr)
	if err != nil {
		return nil, fmt.Errorf("Can not get the installed snaps: %s", err)

	}

	return parts, nil
}

func (o *Overlord) partsForGlobExpr(globExpr string) (parts []*SnapPart, err error) {
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return nil, err
	}

	for _, yamlfile := range matches {
		// skip "current" and similar symlinks
		realpath, err := filepath.EvalSymlinks(yamlfile)
		if err != nil {
			return nil, err
		}
		if realpath != yamlfile {
			continue
		}

		origin, _ := originFromYamlPath(realpath)
		snap, err := NewInstalledSnapPart(realpath, origin)
		if err != nil {
			return nil, err
		}
		parts = append(parts, snap)
	}

	return parts, nil
}
