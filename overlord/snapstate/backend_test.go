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

package snapstate_test

import (
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
)

type fakeOp struct {
	op string

	name    string
	version string
	channel string
	flags   int
	active  bool
}

type fakeSnappyBackend struct {
	ops []fakeOp

	fakeCurrentProgress int
	fakeTotalProgress   int
}

func (f *fakeSnappyBackend) InstallLocal(path string, flags int, p progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "install-local",
		name: path,
	})
	return nil
}

func (f *fakeSnappyBackend) Download(name, channel string, p progress.Meter) (string, string, error) {
	f.ops = append(f.ops, fakeOp{
		op:      "download",
		name:    name,
		channel: channel,
	})
	p.SetTotal(float64(f.fakeTotalProgress))
	p.Set(float64(f.fakeCurrentProgress))
	return "downloaded-snap-path", "1.0", nil
}

func (f *fakeSnappyBackend) Update(name, channel string, flags int, p progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:      "update",
		name:    name,
		channel: channel,
	})
	return nil
}

func (f *fakeSnappyBackend) Remove(name string, flags int, p progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove",
		name: name,
	})
	return nil
}

func (f *fakeSnappyBackend) Rollback(name, ver string, p progress.Meter) (string, error) {
	f.ops = append(f.ops, fakeOp{
		op:      "rollback",
		name:    name,
		version: ver,
	})
	return "", nil
}

func (f *fakeSnappyBackend) Activate(name string, active bool, p progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:     "activate",
		name:   name,
		active: active,
	})
	return nil
}

func (f *fakeSnappyBackend) CheckSnap(snapFilePath string, flags int) error {
	f.ops = append(f.ops, fakeOp{
		op:   "check-snap",
		name: snapFilePath,
	})
	return nil
}

func (f *fakeSnappyBackend) SetupSnap(snapFilePath string, flags int) error {
	f.ops = append(f.ops, fakeOp{
		op:   "setup-snap",
		name: snapFilePath,
	})
	return nil
}

func (f *fakeSnappyBackend) CopySnapData(instSnapPath string, flags int) error {
	f.ops = append(f.ops, fakeOp{
		op:   "copy-data",
		name: instSnapPath,
	})
	return nil
}

func (f *fakeSnappyBackend) SetupSnapSecurity(instSnapPath string) error {
	f.ops = append(f.ops, fakeOp{
		op:   "setup-snap-security",
		name: instSnapPath,
	})
	return nil
}

func (f *fakeSnappyBackend) LinkSnap(instSnapPath string) error {
	f.ops = append(f.ops, fakeOp{
		op:   "link-snap",
		name: instSnapPath,
	})
	return nil
}

func (f *fakeSnappyBackend) UndoSetupSnap(snapFilePath string) error {
	f.ops = append(f.ops, fakeOp{
		op:   "undo-setup-snap",
		name: snapFilePath,
	})
	return nil
}

func (f *fakeSnappyBackend) UndoCopySnapData(instSnapPath string, flags int) error {
	f.ops = append(f.ops, fakeOp{
		op:   "undo-copy-snap-data",
		name: instSnapPath,
	})
	return nil
}

func (f *fakeSnappyBackend) UndoLinkSnap(oldInstSnapPath, instSnapPath string) error {
	f.ops = append(f.ops, fakeOp{
		op:   "undo-link-snap",
		name: instSnapPath,
	})
	return nil
}

func (f *fakeSnappyBackend) ActiveSnap(name string) *snap.Info {
	return &snap.Info{
		SuggestedName: "an-active-snap",
		Version:       "1.64872",
	}
}

func (f *fakeSnappyBackend) CanRemove(instSnapPath string) error {
	f.ops = append(f.ops, fakeOp{
		op:   "can-remove",
		name: instSnapPath,
	})
	return nil
}

func (f *fakeSnappyBackend) UnlinkSnap(instSnapPath string, meter progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "unlink-snap",
		name: instSnapPath,
	})
	return nil
}

func (f *fakeSnappyBackend) RemoveSnapSecurity(instSnapPath string) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove-snap-security",
		name: instSnapPath,
	})
	return nil
}

func (f *fakeSnappyBackend) RemoveSnapFiles(instSnapPath string, meter progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove-snap-files",
		name: instSnapPath,
	})
	return nil
}

func (f *fakeSnappyBackend) RemoveSnapData(name, version string) error {
	f.ops = append(f.ops, fakeOp{
		op:      "remove-snap-data",
		name:    name,
		version: version,
	})
	return nil
}
