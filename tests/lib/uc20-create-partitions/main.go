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
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

var installRun = install.Run

type cmdCreatePartitions struct {
	Mount   bool `short:"m" long:"mount" description:"Also mount filesystems after creation"`
	Encrypt bool `long:"encrypt" description:"Encrypt the data partition"`

	Positional struct {
		GadgetRoot     string `positional-arg-name:"<gadget-root>"`
		KernelName     string `positional-arg-name:"<kernel-name>"`
		KernelRoot     string `positional-arg-name:"<kernel-root>"`
		KernelRevision string `positional-arg-name:"<kernel-revision>"`
		Device         string `positional-arg-name:"<device>"`
	} `positional-args:"yes"`
}

type simpleObserver struct{}

func (o *simpleObserver) Observe(op gadget.ContentOperation, partRole, root, dst string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
	return gadget.ChangeApply, nil
}

func (o *simpleObserver) ChosenBootstrappedContainer(key secboot.BootstrappedContainer) {}

type uc20Constraints struct{}

func (c uc20Constraints) Classic() bool             { return false }
func (c uc20Constraints) Grade() asserts.ModelGrade { return asserts.ModelSigned }

func main() {
	logger.SimpleSetup(nil)

	args := &cmdCreatePartitions{}
	if _, err := flags.ParseArgs(args, os.Args[1:]); err != nil {
		panic(err)
	}

	obs := &simpleObserver{}

	var encryptionType device.EncryptionType
	if args.Encrypt {
		encryptionType = device.EncryptionTypeLUKS
	}

	options := install.Options{
		Mount:          args.Mount,
		EncryptionType: encryptionType,
	}
	kernelSnapInfo := &install.KernelSnapInfo{
		Name:       args.Positional.KernelName,
		MountPoint: args.Positional.KernelRoot,
		Revision:   snap.R(args.Positional.KernelRevision),
		IsCore:     true,
	}
	installSideData, err := installRun(uc20Constraints{}, args.Positional.GadgetRoot, kernelSnapInfo, args.Positional.Device, options, obs, timings.New(nil))
	if err != nil {
		panic(err)
	}

	if args.Encrypt {
		if installSideData == nil || len(installSideData.BootstrappedContainerForRole) == 0 {
			panic("expected encryption keys")
		}
		dataBootstrapKey := installSideData.BootstrappedContainerForRole[gadget.SystemData]
		if dataBootstrapKey == nil {
			panic("ubuntu-data encryption key is unset")
		}

		dataKey, err := keys.NewEncryptionKey()
		if err != nil {
			panic("cannot create data key")
		}
		if err := dataBootstrapKey.AddKey("", secboot.DiskUnlockKey(dataKey)); err != nil {
			panic("cannot reset data key")
		}

		saveBootstrapKey := installSideData.BootstrappedContainerForRole[gadget.SystemSave]
		if saveBootstrapKey == nil {
			panic("ubuntu-save encryption key is unset")
		}

		saveKey, err := keys.NewEncryptionKey()
		if err != nil {
			panic("cannot create save key")
		}
		if err := saveBootstrapKey.AddKey("", secboot.DiskUnlockKey(saveKey)); err != nil {
			panic("cannot reset save key")
		}

		toWrite := map[string][]byte{
			"unsealed-key": dataKey[:],
			"save-key":     saveKey[:],
		}
		for keyFileName, keyData := range toWrite {
			if err := os.WriteFile(keyFileName, keyData, 0644); err != nil {
				panic(err)
			}
		}

		if err := dataBootstrapKey.RemoveBootstrapKey(); err != nil {
			panic(err)
		}
		if err := saveBootstrapKey.RemoveBootstrapKey(); err != nil {
			panic(err)
		}
	}
}
