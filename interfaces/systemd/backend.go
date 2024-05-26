// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	sysd "github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timings"
)

func serviceName(snapName, distinctServiceSuffix string) string {
	return snap.ScopedSecurityTag(snapName, "interface", distinctServiceSuffix) + ".service"
}

// Backend is responsible for maintaining apparmor profiles for ubuntu-core-launcher.
type Backend struct {
	preseed bool
}

// Initialize does nothing.
func (b *Backend) Initialize(opts *interfaces.SecurityBackendOptions) error {
	if opts != nil && opts.Preseed {
		b.preseed = true
	}
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
func (b *Backend) Setup(appSet *interfaces.SnapAppSet, confinement interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	// Record all the extra systemd services for this snap.
	snapName := appSet.InstanceName()
	// Get the services that apply to this snap
	spec := mylog.Check2(repo.SnapSpecification(b.Name(), appSet))

	content := deriveContent(spec.(*Specification), appSet)
	// synchronize the content with the filesystem
	dir := dirs.SnapServicesDir
	mylog.Check(os.MkdirAll(dir, 0755))

	glob := serviceName(snapName, "*")

	var systemd sysd.Systemd
	if b.preseed {
		systemd = sysd.NewEmulationMode(dirs.GlobalRootDir)
	} else {
		systemd = sysd.New(sysd.SystemMode, &noopReporter{})
	}
	mylog.Check(

		// We need to be carefully here and stop all removed service units before
		// we remove their files as otherwise systemd is not able to disable/stop
		// them anymore.
		b.disableRemovedServices(systemd, dir, glob, content))

	changed, removed, errEnsure := osutil.EnsureDirState(dir, glob, content)
	// Reload systemd whenever something is added or removed
	if !b.preseed && (len(changed) > 0 || len(removed) > 0) {
		mylog.Check(systemd.DaemonReload())
	}
	if len(changed) > 0 {
		mylog.Check(
			// Ensure the services are running right now and on reboots
			systemd.EnableNoReload(changed))

		if !b.preseed {
			mylog.Check(
				// If we have new services here which aren't started yet the restart
				// operation will start them.
				systemd.Restart(changed))
		}
	}
	if !b.preseed && len(changed) > 0 {
		mylog.Check(systemd.DaemonReload())
	}
	return errEnsure
}

// Remove disables, stops and removes systemd services of a given snap.
func (b *Backend) Remove(snapName string) error {
	var systemd sysd.Systemd
	if b.preseed {
		// removing while preseeding is not a viable scenario, but implemented
		// for completeness.
		systemd = sysd.NewEmulationMode(dirs.GlobalRootDir)
	} else {
		systemd = sysd.New(sysd.SystemMode, &noopReporter{})
	}
	// Remove all the files matching snap glob
	glob := serviceName(snapName, "*")
	_, removed, errEnsure := osutil.EnsureDirState(dirs.SnapServicesDir, glob, nil)

	if len(removed) > 0 {
		logger.Noticef("systemd-backend: Disable: removed services: %q", removed)
		mylog.Check(systemd.DisableNoReload(removed))

		if !b.preseed {
			mylog.Check(systemd.Stop(removed))
		}
	}
	// Reload systemd whenever something is removed
	if !b.preseed && len(removed) > 0 {
		mylog.Check(systemd.DaemonReload())
	}
	return errEnsure
}

// NewSpecification returns a new systemd specification.
func (b *Backend) NewSpecification(*interfaces.SnapAppSet) interfaces.Specification {
	return &Specification{}
}

// SandboxFeatures returns nil
func (b *Backend) SandboxFeatures() []string {
	return nil
}

// deriveContent computes .service files based on requests made to the specification.
func deriveContent(spec *Specification, appSet *interfaces.SnapAppSet) map[string]osutil.FileState {
	services := spec.Services()
	if len(services) == 0 {
		return nil
	}
	content := make(map[string]osutil.FileState)
	for suffix, service := range services {
		filename := serviceName(appSet.InstanceName(), suffix)
		content[filename] = &osutil.MemoryFileState{
			Content: []byte(service.String()),
			Mode:    0644,
		}
	}
	return content
}

func (b *Backend) disableRemovedServices(systemd sysd.Systemd, dir, glob string, content map[string]osutil.FileState) error {
	paths := mylog.Check2(filepath.Glob(filepath.Join(dir, glob)))

	var stopUnits []string
	var disableUnits []string
	for _, path := range paths {
		service := filepath.Base(path)
		if content[service] == nil {
			disableUnits = append(disableUnits, service)
			if !b.preseed {
				stopUnits = append(stopUnits, service)
			}
		}
	}
	if len(disableUnits) > 0 {
		mylog.Check(systemd.DisableNoReload(disableUnits))
	}
	if len(stopUnits) > 0 {
		mylog.Check(systemd.Stop(stopUnits))
	}
	return nil
}

type noopReporter struct{}

func (dr *noopReporter) Notify(msg string) {
}
