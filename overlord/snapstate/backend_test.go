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
	"github.com/ubuntu-core/snappy/snappy"
)

type fakeOp struct {
	op string

	name      string
	ver       string
	channel   string
	developer string
	flags     int
	active    bool
}

type fakeSnappyBackend struct {
	ops []fakeOp

	fakeCurrentProgress int
	fakeTotalProgress   int
}

func (f *fakeSnappyBackend) InstallLocal(path, developer string, flags snappy.InstallFlags, p progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:        "install-local",
		name:      path,
		developer: developer,
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
	return "downloaded-snap-path", "some-developer", nil
}

func (f *fakeSnappyBackend) Update(name, channel string, flags snappy.InstallFlags, p progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:      "update",
		name:    name,
		channel: channel,
	})
	return nil
}

func (f *fakeSnappyBackend) Remove(name string, flags snappy.RemoveFlags, p progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove",
		name: name,
	})
	return nil
}

func (f *fakeSnappyBackend) Rollback(name, ver string, p progress.Meter) (string, error) {
	f.ops = append(f.ops, fakeOp{
		op:   "rollback",
		name: name,
		ver:  ver,
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

func (f *fakeSnappyBackend) CheckSnap(snapFilePath, developer string, flags snappy.InstallFlags) error {
	f.ops = append(f.ops, fakeOp{
		op:        "check-snap",
		name:      snapFilePath,
		developer: developer,
	})
	return nil
}

func (f *fakeSnappyBackend) SetupSnap(snapFilePath, developer string, flags snappy.InstallFlags) (string, error) {
	f.ops = append(f.ops, fakeOp{
		op:        "setup-snap",
		name:      snapFilePath,
		developer: developer,
	})
	return "some-inst-path", nil
}

func (f *fakeSnappyBackend) CopySnapData(instSnapPath, developer string, flags snappy.InstallFlags) error {
	f.ops = append(f.ops, fakeOp{
		op:        "copy-data",
		name:      instSnapPath,
		developer: developer,
	})
	return nil
}

func (f *fakeSnappyBackend) GenerateSecurityProfile(instSnapPath, developer string) error {
	f.ops = append(f.ops, fakeOp{
		op:        "generate-security-profile",
		name:      instSnapPath,
		developer: developer,
	})
	return nil
}

func (f *fakeSnappyBackend) FinalizeSnap(instSnapPath, developer string, flags snappy.InstallFlags) error {
	f.ops = append(f.ops, fakeOp{
		op:        "finalize-snap",
		name:      instSnapPath,
		developer: developer,
	})
	return nil
}

func (f *fakeSnappyBackend) UndoSetupSnap(snapFilePath, developer string) error {
	f.ops = append(f.ops, fakeOp{
		op:        "undo-setup-snap",
		name:      snapFilePath,
		developer: developer,
	})
	return nil
}
func (f *fakeSnappyBackend) UndoGenerateSecurityProfile(instSnapPath, developer string) error {
	f.ops = append(f.ops, fakeOp{
		op:        "undo-generate-security-profile",
		name:      instSnapPath,
		developer: developer,
	})
	return nil
}
func (f *fakeSnappyBackend) UndoCopySnapData(instSnapPath, developer string, flags snappy.InstallFlags) error {
	f.ops = append(f.ops, fakeOp{
		op:        "undo-copy-snap-data",
		name:      instSnapPath,
		developer: developer,
	})
	return nil
}
func (f *fakeSnappyBackend) UndoFinalizeSnap(oldInstSnapPath, instSnapPath, developer string, flags snappy.InstallFlags) error {
	f.ops = append(f.ops, fakeOp{
		op:        "undo-finalize-snap",
		name:      instSnapPath,
		developer: developer,
	})
	return nil
}
