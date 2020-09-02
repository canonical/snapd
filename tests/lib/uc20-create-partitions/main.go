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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget/install"
)

var installRun = install.Run

type cmdCreatePartitions struct {
	Mount      bool   `short:"m" long:"mount" description:"Also mount filesystems after creation"`
	Encrypt    bool   `long:"encrypt" description:"Encrypt the data partition"`
	KernelPath string `long:"kernel" value-name:"path" description:"Path to the kernel to be installed"`
	ModelPath  string `long:"model" value-name:"filename" description:"The model to seal the key file to"`

	Positional struct {
		GadgetRoot string `positional-arg-name:"<gadget-root>"`
		Device     string `positional-arg-name:"<device>"`
	} `positional-args:"yes"`
}

const (
	short = "Create missing partitions for the device"
	long  = ""
)

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

	var model *asserts.Model
	if args.ModelPath != "" {
		var err error
		model, err = readModel(args.ModelPath)
		if err != nil {
			panic(fmt.Sprintf("cannot load model: %v", err))
		}
	}
	options := install.Options{
		Mount:      args.Mount,
		Encrypt:    args.Encrypt,
		KernelPath: args.KernelPath,
		Model:      model,
	}
	err = installRun(args.Positional.GadgetRoot, args.Positional.Device, options, nil)
	if err != nil {
		panic(err)
	}
}
