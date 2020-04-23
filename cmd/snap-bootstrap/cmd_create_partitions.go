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
	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
)

var bootstrapRun = bootstrap.Run

func init() {
	const (
		short = "Create missing partitions for the device"
		long  = ""
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("create-partitions", short, long, &cmdCreatePartitions{}); err != nil {
			panic(err)
		}
	})
}

type cmdCreatePartitions struct {
	Mount                bool   `short:"m" long:"mount" description:"Also mount filesystems after creation"`
	Encrypt              bool   `long:"encrypt" description:"Encrypt the data partition"`
	KeyFile              string `long:"key-file" value-name:"filename" description:"Where the key file will be stored"`
	RecoveryKeyFile      string `long:"recovery-key-file" value-name:"filename" description:"Where the recovery key file will be stored"`
	TPMLockoutAuthFile   string `long:"tpm-lockout-auth" value-name:"filename" descrition:"Where the TPM lockout authorization data file will be stored"`
	PolicyUpdateDataFile string `long:"policy-update-data-file" value-name:"filename" description:"Where the authorization policy update data file will be stored"`
	KernelPath           string `long:"kernel" value-name:"path" description:"Path to the kernel to be installed"`
	ModelPath            string `long:"model" value-name:"filename" description:"The model to seal the key file to"`

	Positional struct {
		GadgetRoot string `positional-arg-name:"<gadget-root>"`
		Device     string `positional-arg-name:"<device>"`
	} `positional-args:"yes"`
}

func (c *cmdCreatePartitions) Execute(args []string) error {
	var model *asserts.Model
	if c.ModelPath != "" {
		var err error
		model, err = loadModel(c.ModelPath)
		if err != nil {
			return fmt.Errorf("cannot load model: %v", err)
		}
	}

	options := bootstrap.Options{
		Mount:                c.Mount,
		Encrypt:              c.Encrypt,
		KeyFile:              c.KeyFile,
		RecoveryKeyFile:      c.RecoveryKeyFile,
		TPMLockoutAuthFile:   c.TPMLockoutAuthFile,
		PolicyUpdateDataFile: c.PolicyUpdateDataFile,
		KernelPath:           c.KernelPath,
		Model:                model,
	}

	return bootstrapRun(c.Positional.GadgetRoot, c.Positional.Device, options)
}

func loadModel(modelPath string) (*asserts.Model, error) {
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
