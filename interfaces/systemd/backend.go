// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

// Package systemd implements integration between snappy interfaces and
// arbitrary systemd units that may be required for "oneshot" style tasks.
package systemd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	sysd "github.com/snapcore/snapd/systemd"
)

// Backend is responsible for maintaining apparmor profiles for ubuntu-core-launcher.
type Backend struct{}

// Initialize does nothing.
func (b *Backend) Initialize() error {
	return nil
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return interfaces.SecuritySystemd
}

// Setup creates and starts systemd services specific to a given snap.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func (b *Backend) Setup(snapInfo *snap.Info, confinement interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	// Record all the extra systemd services for this snap.
	snapName := snapInfo.InstanceName()
	// Get the services that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), snapName)
	if err != nil {
		return fmt.Errorf("cannot obtain systemd services for snap %q: %s", snapName, err)
	}
	content := deriveContent(spec.(*Specification), snapInfo)
	// synchronize the content with the filesystem
	dir := dirs.SnapServicesDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for systemd services %q: %s", dir, err)
	}
	glob := interfaces.InterfaceServiceName(snapName, "*")

	systemd := sysd.New(dirs.GlobalRootDir, &dummyReporter{})
	// We need to be carefully here and stop all removed service units before
	// we remove their files as otherwise systemd is not able to disable/stop
	// them anymore.
	if err := disableRemovedServices(systemd, dir, glob, content); err != nil {
		logger.Noticef("cannot stop removed services: %s", err)
	}
	changed, removed, errEnsure := osutil.EnsureDirState(dir, glob, content)
	// Reload systemd whenever something is added or removed
	if len(changed) > 0 || len(removed) > 0 {
		err := systemd.DaemonReload()
		if err != nil {
			logger.Noticef("cannot reload systemd state: %s", err)
		}
	}
	// Ensure the service is running right now and on reboots
	for _, service := range changed {
		if err := systemd.Enable(service); err != nil {
			logger.Noticef("cannot enable service %q: %s", service, err)
		}
		// If we have a new service here which isn't started yet the restart
		// operation will start it.
		if err := systemd.Restart(service, 10*time.Second); err != nil {
			logger.Noticef("cannot restart service %q: %s", service, err)
		}
	}
	return errEnsure
}

// Remove disables, stops and removes systemd services of a given snap.
func (b *Backend) Remove(snapName string) error {
	systemd := sysd.New(dirs.GlobalRootDir, &dummyReporter{})
	// Remove all the files matching snap glob
	glob := interfaces.InterfaceServiceName(snapName, "*")
	_, removed, errEnsure := osutil.EnsureDirState(dirs.SnapServicesDir, glob, nil)
	for _, service := range removed {
		if err := systemd.Disable(service); err != nil {
			logger.Noticef("cannot disable service %q: %s", service, err)
		}
		if err := systemd.Stop(service, 5*time.Second); err != nil {
			logger.Noticef("cannot stop service %q: %s", service, err)
		}
	}
	return errEnsure
}

// NewSpecification returns a new systemd specification.
func (b *Backend) NewSpecification() interfaces.Specification {
	return &Specification{}
}

// SandboxFeatures returns nil
func (b *Backend) SandboxFeatures() []string {
	return nil
}

// deriveContent computes .service files based on requests made to the specification.
func deriveContent(spec *Specification, snapInfo *snap.Info) map[string]*osutil.FileState {
	services := spec.Services()
	if len(services) == 0 {
		return nil
	}
	content := make(map[string]*osutil.FileState)
	for name, service := range services {
		content[name] = &osutil.FileState{
			Content: []byte(service.String()),
			Mode:    0644,
		}
	}
	return content
}

func disableRemovedServices(systemd sysd.Systemd, dir, glob string, content map[string]*osutil.FileState) error {
	paths, err := filepath.Glob(filepath.Join(dir, glob))
	if err != nil {
		return err
	}
	for _, path := range paths {
		service := filepath.Base(path)
		if content[service] == nil {
			if err := systemd.Disable(service); err != nil {
				logger.Noticef("cannot disable service %q: %s", service, err)
			}
			if err := systemd.Stop(service, 5*time.Second); err != nil {
				logger.Noticef("cannot stop service %q: %s", service, err)
			}
		}
	}
	return nil
}

type dummyReporter struct{}

func (dr *dummyReporter) Notify(msg string) {
}
