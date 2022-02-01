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
	"io/ioutil"
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
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

func (o *simpleObserver) Observe(op gadget.ContentOperation, affectedStruct *gadget.LaidOutStructure, root, dst string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
	return gadget.ChangeApply, nil
}

func (o *simpleObserver) ChosenEncryptionKey(key secboot.EncryptionKey) {}

type uc20Constraints struct{}

func (c uc20Constraints) Classic() bool             { return false }
func (c uc20Constraints) Grade() asserts.ModelGrade { return asserts.ModelSigned }

func main() {
	if err := logger.SimpleSetup(); err != nil {
		fmt.Fprintf(os.Stderr, i18n.G("WARNING: failed to activate logging: %v\n"), err)
	}

	args := &cmdCreatePartitions{}
	if _, err := flags.ParseArgs(args, os.Args[1:]); err != nil {
		panic(err)
	}

	obs := &simpleObserver{}

	var encryptionType secboot.EncryptionType
	if args.Encrypt {
		encryptionType = secboot.EncryptionTypeLUKS
	}

	options := install.Options{
		Mount:          args.Mount,
		EncryptionType: encryptionType,
	}
	installSideData, err := installRun(uc20Constraints{}, args.Positional.GadgetRoot, args.Positional.KernelRoot, args.Positional.Device, options, obs, timings.New(nil))
	if err != nil {
		panic(err)
	}

	if args.Encrypt {
		if installSideData == nil || installSideData.KeysForRoles == nil {
			panic("expected encryption keys")
		}
		dataKey := installSideData.KeysForRoles[gadget.SystemData]
		if dataKey == nil {
			panic("ubuntu-data encryption key is unset")
		}
		saveKey := installSideData.KeysForRoles[gadget.SystemSave]
		if saveKey == nil {
			panic("ubuntu-save encryption key is unset")
		}
		toWrite := map[string][]byte{
			"unsealed-key":  dataKey.Key[:],
			"recovery-key":  dataKey.RecoveryKey[:],
			"save-key":      saveKey.Key[:],
			"reinstall-key": saveKey.RecoveryKey[:],
		}
		for keyFileName, keyData := range toWrite {
			if err := ioutil.WriteFile(keyFileName, keyData, 0644); err != nil {
				panic(err)
			}
		}
	}
}
