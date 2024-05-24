// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020, 2024 Canonical Ltd
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

	"github.com/jessevdk/go-flags"
	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/i18n"
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

func (o *simpleObserver) ChosenEncryptionKey(key keys.EncryptionKey) {}

type uc20Constraints struct{}

func (c uc20Constraints) Classic() bool             { return false }
func (c uc20Constraints) Grade() asserts.ModelGrade { return asserts.ModelSigned }

func main() {
	if err := logger.SimpleSetup(nil); err != nil {
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
		if installSideData == nil || len(installSideData.ResetterForRole) == 0 {
			panic("expected encryption keys")
		}
		dataKeyResetter := installSideData.ResetterForRole[gadget.SystemData]
		if dataKeyResetter == nil {
			panic("ubuntu-data encryption key is unset")
		}
		dataKey, err := keys.NewEncryptionKey()
		if err != nil {
			panic("cannot create data key")
		}
		const token = false
		if _, err := dataKeyResetter.AddKey("", sb.DiskUnlockKey(dataKey), token); err != nil {
			panic("cannot reset data key")
		}
		saveKeyResetter := installSideData.ResetterForRole[gadget.SystemSave]
		if saveKeyResetter == nil {
			panic("ubuntu-save encryption key is unset")
		}
		saveKey, err := keys.NewEncryptionKey()
		if err != nil {
			panic("cannot create save key")
		}
		if _, err := saveKeyResetter.AddKey("", sb.DiskUnlockKey(saveKey), token); err != nil {
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

		if err := dataKeyResetter.RemoveInstallationKey(); err != nil {
			panic(err)
		}
		if err := saveKeyResetter.RemoveInstallationKey(); err != nil {
			panic(err)
		}
	}
}
