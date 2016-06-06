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
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snappy"
	"github.com/snapcore/snapd/store"
)

type managerBackend interface {
	// install releated
	Download(name, channel string, checker func(*snap.Info) error, meter progress.Meter, auther store.Authenticator) (*snap.Info, string, error)
	SetupSnap(snapFilePath string, si *snap.SideInfo) error
	CopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter) error
	LinkSnap(info *snap.Info) error
	// the undoers for install
	UndoSetupSnap(s snap.PlaceInfo) error
	UndoCopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter) error

	// remove releated
	UnlinkSnap(info *snap.Info, meter progress.Meter) error
	RemoveSnapFiles(s snap.PlaceInfo, meter progress.Meter) error
	RemoveSnapData(info *snap.Info) error
	RemoveSnapCommonData(info *snap.Info) error

	// testing helpers
	Current(cur *snap.Info)
	Candidate(sideInfo *snap.SideInfo)
}

type defaultBackend struct {
	// XXX defaultBackend will go away and be replaced by this in the end.
	backend.Backend
}

func (b *defaultBackend) Candidate(*snap.SideInfo) {}
func (b *defaultBackend) Current(*snap.Info)       {}

func (b *defaultBackend) Download(name, channel string, checker func(*snap.Info) error, meter progress.Meter, auther store.Authenticator) (*snap.Info, string, error) {
	mStore := snappy.NewConfiguredUbuntuStoreSnapRepository()
	snap, err := mStore.Snap(name, channel, auther)
	if err != nil {
		return nil, "", err
	}

	err = checker(snap)
	if err != nil {
		return nil, "", err
	}

	downloadedSnapFile, err := mStore.Download(snap, meter, auther)
	if err != nil {
		return nil, "", err
	}

	return snap, downloadedSnapFile, nil
}

func (b *defaultBackend) SetupSnap(snapFilePath string, sideInfo *snap.SideInfo) error {
	meter := &progress.NullProgress{}
	// XXX: pass 0 for flags temporarely, until SetupSnap is moved over,
	// anyway they aren't used atm, and probably we don't want to pass flags
	// as before but more precise information
	_, err := snappy.SetupSnap(snapFilePath, sideInfo, 0, meter)
	return err
}

func (b *defaultBackend) UndoSetupSnap(s snap.PlaceInfo) error {
	meter := &progress.NullProgress{}
	snappy.UndoSetupSnap(s, meter)
	return nil
}

func (b *defaultBackend) RemoveSnapFiles(s snap.PlaceInfo, meter progress.Meter) error {
	return snappy.RemoveSnapFiles(s, meter)
}
