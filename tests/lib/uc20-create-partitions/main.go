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
	"github.com/snapcore/snapd/secboot"
)

var installRun = install.Run

type cmdCreatePartitions struct {
	Mount   bool `short:"m" long:"mount" description:"Also mount filesystems after creation"`
	Encrypt bool `long:"encrypt" description:"Encrypt the data partition"`

	Positional struct {
		GadgetRoot string `positional-arg-name:"<gadget-root>"`
		Device     string `positional-arg-name:"<device>"`
	} `positional-args:"yes"`
}

const (
	short = "Create missing partitions for the device"
	long  = ""
)

type simpleObserver struct{}

func (o *simpleObserver) Observe(op gadget.ContentOperation, affectedStruct *gadget.LaidOutStructure, root, dst string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
	return gadget.ChangeApply, nil
}

func (o *simpleObserver) ChosenEncryptionKey(key secboot.EncryptionKey) {}

func readModel(modelPath string) (*asserts.Model, error) {
	f, err := os.Open(modelPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	a, err := asserts.NewDecoder(f).Decode()
	if err != nil {
		return nil, fmt.Errorf("cannot decode assertion: %v", err)
	}
	if a.Type() != asserts.ModelType {
		return nil, fmt.Errorf("not a model assertion")
	}
	return a.(*asserts.Model), nil
}

func main() {
	args := &cmdCreatePartitions{}
	_, err := flags.ParseArgs(args, os.Args[1:])
	if err != nil {
		panic(err)
	}

	obs := &simpleObserver{}

	options := install.Options{
		Mount:   args.Mount,
		Encrypt: args.Encrypt,
	}
	installSideData, err := installRun(args.Positional.GadgetRoot, args.Positional.Device, options, obs)
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
