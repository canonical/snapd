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
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snappy"
)

type managerBackend interface {
	// needs to go, its the huge all-in-one install thing
	InstallLocal(snap, developer string, flags snappy.InstallFlags, meter progress.Meter) error

	// the individual tasks for installing a snap
	Download(name, channel string, meter progress.Meter) (string, string, error)
	CheckSnap(snapFilePath, developer string, flags snappy.InstallFlags) error
	SetupSnap(snapFilePath, developer string, flags snappy.InstallFlags) (string, error)
	CopySnapData(instSnapPath, developer string, flags snappy.InstallFlags) error
	GenerateSecurityProfile(instSnapPath, developer string) error
	FinalizeSnap(instSnapPath, developer string, flags snappy.InstallFlags) error

	// TODO: need to be split into fine grained tasks
	Update(name, channel string, flags snappy.InstallFlags, meter progress.Meter) error
	Remove(name string, flags snappy.RemoveFlags, meter progress.Meter) error
	Rollback(name, ver string, meter progress.Meter) (string, error)
	Activate(name string, active bool, meter progress.Meter) error
}

type defaultBackend struct{}

func (s *defaultBackend) InstallLocal(snap, developer string, flags snappy.InstallFlags, meter progress.Meter) error {
	// FIXME: the name `snappy.Overlord` is confusing :/
	_, err := (&snappy.Overlord{}).Install(snap, developer, flags, meter)
	return err
}

func (s *defaultBackend) Update(name, channel string, flags snappy.InstallFlags, meter progress.Meter) error {
	// FIXME: support "channel" in snappy.Update()
	_, err := snappy.Update(name, flags, meter)
	return err
}

func (s *defaultBackend) Remove(name string, flags snappy.RemoveFlags, meter progress.Meter) error {
	return snappy.Remove(name, flags, meter)
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
	if err := snappy.SaveStoreManifest(snap); err != nil {
		return "", "", err
	}

	return downloadedSnapFile, snap.Developer(), nil
}

func (s *defaultBackend) CheckSnap(snapFilePath, developer string, flags snappy.InstallFlags) error {
	meter := &progress.NullProgress{}
	return snappy.CheckSnap(snapFilePath, developer, flags, meter)
}

func (s *defaultBackend) SetupSnap(snapFilePath, developer string, flags snappy.InstallFlags) (string, error) {
	meter := &progress.NullProgress{}
	return snappy.SetupSnap(snapFilePath, developer, flags, meter)
}

func (s *defaultBackend) CopySnapData(snapInstPath, developer string, flags snappy.InstallFlags) error {
	sn, err := snappy.NewInstalledSnap(snapInstPath, developer)
	if err != nil {
		return err
	}
	return snappy.CopyData(sn, flags, &progress.NullProgress{})
}

func (s *defaultBackend) GenerateSecurityProfile(snapInstPath, developer string) error {
	sn, err := snappy.NewInstalledSnap(snapInstPath, developer)
	if err != nil {
		return err
	}
	return snappy.GenerateSecurityProfile(sn)
}

func (s *defaultBackend) FinalizeSnap(snapInstPath, developer string, flags snappy.InstallFlags) error {
	sn, err := snappy.NewInstalledSnap(snapInstPath, developer)
	if err != nil {
		return err
	}
	return snappy.FinalizeSnap(sn, flags, &progress.NullProgress{})
}
