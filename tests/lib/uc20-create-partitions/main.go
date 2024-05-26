// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package main

import (
	"fmt"
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/timings"
)

var installRun = install.Run

type cmdCreatePartitions struct {
	Mount   bool `short:"m" long:"mount" description:"Also mount filesystems after creation"`
	Encrypt bool `long:"encrypt" description:"Encrypt the data partition"`

	Positional struct {
		GadgetRoot string `positional-arg-name:"<gadget-root>"`
		KernelRoot string `positional-arg-name:"<kernel-root>"`
		Device     string `positional-arg-name:"<device>"`
	} `positional-args:"yes"`
}

type simpleObserver struct{}

func (o *simpleObserver) Observe(op gadget.ContentOperation, partRole, root, dst string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
	return gadget.ChangeApply, nil
}

func (o *simpleObserver) ChosenEncryptionKey(key keys.EncryptionKey) {}

type uc20Constraints struct{}

func (c uc20Constraints) Classic() bool             { return false }
func (c uc20Constraints) Grade() asserts.ModelGrade { return asserts.ModelSigned }

func main() {
	mylog.Check(logger.SimpleSetup(nil))

	args := &cmdCreatePartitions{}
	mylog.Check2(flags.ParseArgs(args, os.Args[1:]))

	obs := &simpleObserver{}

	var encryptionType secboot.EncryptionType
	if args.Encrypt {
		encryptionType = secboot.EncryptionTypeLUKS
	}

	options := install.Options{
		Mount:          args.Mount,
		EncryptionType: encryptionType,
	}
	installSideData := mylog.Check2(installRun(uc20Constraints{}, args.Positional.GadgetRoot, args.Positional.KernelRoot, args.Positional.Device, options, obs, timings.New(nil)))

	if args.Encrypt {
		if installSideData == nil || len(installSideData.KeyForRole) == 0 {
			panic("expected encryption keys")
		}
		dataKey := installSideData.KeyForRole[gadget.SystemData]
		if dataKey == nil {
			panic("ubuntu-data encryption key is unset")
		}
		saveKey := installSideData.KeyForRole[gadget.SystemSave]
		if saveKey == nil {
			panic("ubuntu-save encryption key is unset")
		}
		toWrite := map[string][]byte{
			"unsealed-key": dataKey[:],
			"save-key":     saveKey[:],
		}
		for keyFileName, keyData := range toWrite {
			mylog.Check(os.WriteFile(keyFileName, keyData, 0644))
		}
	}
}
