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
	"github.com/ubuntu-core/snappy/systemd"
)

type Overlord struct {
}

func (o *Overlord) Install(snapFileName string, origin string, inter progress.Meter, flags InstallFlags) (name string, err error) {
	allowGadget := (flags & AllowGadget) != 0
	inhibitHooks := (flags & InhibitHooks) != 0
	allowUnauth := (flags & AllowUnauthenticated) != 0

	s, err := NewSnapFile(snapFileName, origin, allowUnauth)
	if err != nil {
		return "", fmt.Errorf("can not open %s: %s", snapFileName, err)
	}
	defer s.deb.Close()

	// we do not Verify() the package in CanInstall. This is done earlier in
	// NewSnapFile() to ensure that we do not mount/inspect
	// potentially dangerous snaps
	if err := o.CanInstall(s, allowGadget, inter); err != nil {
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

	if err := os.MkdirAll(s.instdir, 0755); err != nil {
		logger.Noticef("Can not create %q: %v", s.instdir, err)
		return "", err
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
	if err := s.deb.UnpackWithDropPrivs(s.instdir, dirs.GlobalRootDir); err != nil {
		return "", err
	}

	// generate the mount unit for the squashfs
	if s.deb.NeedsMountUnit() {
		if err := s.m.addSquashfsMount(s.instdir, inhibitHooks, inter); err != nil {
			return "", err
		}
	}

	// write the hashes now
	if err := s.deb.ExtractHashes(filepath.Join(s.instdir, "meta")); err != nil {
		return "", err
	}

	// FIXME: special handling is bad 'mkay
	if s.m.Type == snap.TypeKernel {
		if err := extractKernelAssets(s, inter, flags); err != nil {
			return "", fmt.Errorf("failed to install kernel %s", err)
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
		err = oldPart.deactivate(inhibitHooks, inter)
		defer func() {
			if err != nil {
				if cerr := oldPart.activate(inhibitHooks, inter); cerr != nil {
					logger.Noticef("Setting old version back to active failed: %v", cerr)
				}
			}
		}()
		if err != nil {
			return "", err
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
		return "", err
	}

	if !inhibitHooks {
		newPart, err := newSnapPartFromYaml(filepath.Join(s.instdir, "meta", "package.yaml"), s.origin, s.m)
		if err != nil {
			return "", err
		}

		// and finally make active
		err = newPart.activate(inhibitHooks, inter)
		defer func() {
			if err != nil && oldPart != nil {
				if cerr := oldPart.activate(inhibitHooks, inter); cerr != nil {
					logger.Noticef("When setting old %s version back to active: %v", s.Name(), cerr)
				}
			}
		}()
		if err != nil {
			return "", err
		}

		// oh, one more thing: refresh the security bits
		deps, err := newPart.Dependents()
		if err != nil {
			return "", err
		}

		sysd := systemd.New(dirs.GlobalRootDir, inter)
		stopped := make(map[string]time.Duration)
		defer func() {
			if err != nil {
				for serviceName := range stopped {
					if e := sysd.Start(serviceName); e != nil {
						inter.Notify(fmt.Sprintf("unable to restart %s with the old %s: %s", serviceName, s.Name(), e))
					}
				}
			}
		}()

		for _, dep := range deps {
			if !dep.IsActive() {
				continue
			}
			for _, svc := range dep.ServiceYamls() {
				serviceName := filepath.Base(generateServiceFileName(dep.m, svc))
				timeout := time.Duration(svc.StopTimeout)
				if err = sysd.Stop(serviceName, timeout); err != nil {
					inter.Notify(fmt.Sprintf("unable to stop %s; aborting install: %s", serviceName, err))
					return "", err
				}
				stopped[serviceName] = timeout
			}
		}

		if err := newPart.RefreshDependentsSecurity(oldPart, inter); err != nil {
			return "", err
		}

		started := make(map[string]time.Duration)
		defer func() {
			if err != nil {
				for serviceName, timeout := range started {
					if e := sysd.Stop(serviceName, timeout); e != nil {
						inter.Notify(fmt.Sprintf("unable to stop %s with the old %s: %s", serviceName, s.Name(), e))
					}
				}
			}
		}()
		for serviceName, timeout := range stopped {
			if err = sysd.Start(serviceName); err != nil {
				inter.Notify(fmt.Sprintf("unable to restart %s; aborting install: %s", serviceName, err))
				return "", err
			}
			started[serviceName] = timeout
		}
	}

	return s.Name(), nil
}

// CanInstall checks whether the SnapPart passes a series of tests required for installation
func (o *Overlord) CanInstall(s *SnapFile, allowGadget bool, inter interacter) error {
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
