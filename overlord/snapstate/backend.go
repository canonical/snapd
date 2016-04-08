// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snapstate

import (
	"fmt"
	"path/filepath"

	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
)

type managerBackend interface {
	// install releated
	Download(name, channel string, meter progress.Meter) (string, string, error)
	CheckSnap(snapFilePath string, flags int) error
	SetupSnap(snapFilePath string, flags int) error
	CopySnapData(instSnapPath string, flags int) error
	SetupSnapSecurity(instSnapPath string) error
	LinkSnap(instSnapPath string) error
	GarbageCollect(snap string, flags int, meter progress.Meter) error
	// the undoers for install
	UndoSetupSnap(snapFilePath string) error
	UndoCopySnapData(instSnapPath string, flags int) error
	UndoLinkSnap(oldInstSnapPath, instSnapPath string) error

	// remove releated
	CanRemove(instSnapPath string) error
	UnlinkSnap(instSnapPath string, meter progress.Meter) error
	RemoveSnapSecurity(instSnapPath string) error
	RemoveSnapFiles(instSnapPath string, meter progress.Meter) error
	RemoveSnapData(name, version string) error

	// TODO: need to be split into fine grained tasks
	Update(name, channel string, flags int, meter progress.Meter) error
	Rollback(name, ver string, meter progress.Meter) (string, error)
	Activate(name string, active bool, meter progress.Meter) error

	// info
	ActiveSnap(name string) *snap.Info
}

type defaultBackend struct{}

func (s *defaultBackend) ActiveSnap(name string) *snap.Info {
	if snap := snappy.ActiveSnapByName(name); snap != nil {
		return snap.Info()
	}
	return nil
}

func (s *defaultBackend) Update(name, channel string, flags int, meter progress.Meter) error {
	// FIXME: support "channel" in snappy.Update()
	_, err := snappy.Update(name, snappy.InstallFlags(flags), meter)
	return err
}

func (s *defaultBackend) Rollback(name, ver string, meter progress.Meter) (string, error) {
	return snappy.Rollback(name, ver, meter)
}

func (s *defaultBackend) Activate(name string, active bool, meter progress.Meter) error {
	return snappy.SetActive(name, active, meter)
}

func (s *defaultBackend) Download(name, channel string, meter progress.Meter) (string, string, error) {
	mStore := snappy.NewConfiguredUbuntuStoreSnapRepository()
	snap, err := mStore.Snap(name, channel)
	if err != nil {
		return "", "", err
	}

	downloadedSnapFile, err := mStore.Download(snap, meter)
	if err != nil {
		return "", "", err
	}

	// FIXME: add undo task so that we delete the store manifest
	//        again if we can not install the snap
	// XXX: do this a bit later?
	// XXX: pass in also info from the parsed yaml from the file?
	if err := snappy.SaveManifest(snap); err != nil {
		return "", "", err
	}

	return downloadedSnapFile, snap.Version, nil
}

func (s *defaultBackend) CheckSnap(snapFilePath string, flags int) error {
	meter := &progress.NullProgress{}
	return snappy.CheckSnap(snapFilePath, snappy.InstallFlags(flags), meter)
}

func (s *defaultBackend) SetupSnap(snapFilePath string, flags int) error {
	meter := &progress.NullProgress{}
	_, err := snappy.SetupSnap(snapFilePath, snappy.InstallFlags(flags), meter)
	return err
}

func (s *defaultBackend) CopySnapData(snapInstPath string, flags int) error {
	sn, err := snappy.NewInstalledSnap(filepath.Join(snapInstPath, "meta", "snap.yaml"))
	if err != nil {
		return err
	}
	meter := &progress.NullProgress{}
	return snappy.CopyData(sn, snappy.InstallFlags(flags), meter)
}

func (s *defaultBackend) SetupSnapSecurity(snapInstPath string) error {
	sn, err := snappy.NewInstalledSnap(filepath.Join(snapInstPath, "meta", "snap.yaml"))
	if err != nil {
		return err
	}
	return snappy.SetupSnapSecurity(sn)
}

func (s *defaultBackend) LinkSnap(snapInstPath string) error {
	sn, err := snappy.NewInstalledSnap(filepath.Join(snapInstPath, "meta", "snap.yaml"))
	if err != nil {
		return err
	}
	meter := &progress.NullProgress{}
	if err := snappy.GenerateWrappers(sn, meter); err != nil {
		return err
	}

	return snappy.UpdateCurrentSymlink(sn, meter)
}

func (s *defaultBackend) UndoSetupSnap(snapFilePath string) error {
	meter := &progress.NullProgress{}
	snappy.UndoSetupSnap(snapFilePath, meter)
	return nil
}

func (s *defaultBackend) UndoCopySnapData(instSnapPath string, flags int) error {
	sn, err := snappy.NewInstalledSnap(filepath.Join(instSnapPath, "meta", "snap.yaml"))
	if err != nil {
		return err
	}
	meter := &progress.NullProgress{}
	snappy.UndoCopyData(sn, snappy.InstallFlags(flags), meter)
	return nil
}

func (s *defaultBackend) UndoLinkSnap(oldInstSnapPath, instSnapPath string) error {
	new, err := snappy.NewInstalledSnap(filepath.Join(instSnapPath, "meta", "snap.yaml"))
	if err != nil {
		return err
	}
	old, err := snappy.NewInstalledSnap(filepath.Join(oldInstSnapPath, "meta", "snap.yaml"))
	if err != nil {
		return err
	}

	meter := &progress.NullProgress{}
	err1 := snappy.RemoveGeneratedWrappers(new, meter)
	err2 := snappy.UndoUpdateCurrentSymlink(old, new, meter)

	// return firstErr
	if err1 != nil {
		return err1
	}
	return err2
}

func (s *defaultBackend) CanRemove(instSnapPath string) error {
	sn, err := snappy.NewInstalledSnap(filepath.Join(instSnapPath, "meta", "snap.yaml"))
	if err != nil {
		return err
	}
	if !snappy.CanRemove(sn) {
		return fmt.Errorf("snap %q is not removable", sn.Name())
	}
	return nil
}

func (s *defaultBackend) UnlinkSnap(instSnapPath string, meter progress.Meter) error {
	sn, err := snappy.NewInstalledSnap(filepath.Join(instSnapPath, "meta", "snap.yaml"))
	if err != nil {
		return err
	}

	return snappy.UnlinkSnap(sn, meter)
}

func (s *defaultBackend) RemoveSnapSecurity(instSnapPath string) error {
	sn, err := snappy.NewInstalledSnap(filepath.Join(instSnapPath, "meta", "snap.yaml"))
	if err != nil {
		return err
	}
	return snappy.RemoveGeneratedSnapSecurity(sn)
}

func (s *defaultBackend) RemoveSnapFiles(instSnapPath string, meter progress.Meter) error {
	sn, err := snappy.NewInstalledSnap(filepath.Join(instSnapPath, "meta", "snap.yaml"))
	if err != nil {
		return err
	}
	return snappy.RemoveSnapFiles(sn, meter)
}
func (s *defaultBackend) RemoveSnapData(name, version string) error {

	return snappy.RemoveSnapData(name, version)
}

func (s *defaultBackend) GarbageCollect(snap string, flags int, meter progress.Meter) error {
	return snappy.GarbageCollect(snap, snappy.InstallFlags(flags), meter)
}
