// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdClusterAssemble struct {
	waitMixin
	Secret       string `long:"secret" required:"yes"`
	Address      string `long:"address" required:"yes"`
	ExpectedSize int    `long:"expected-size"`
	Domain       string `long:"domain"`
}

var shortClusterAssembleHelp = i18n.G("Assemble a cluster with other devices")
var longClusterAssembleHelp = i18n.G(`
The cluster assemble command initiates the cluster assembly process on this device.

This command will:
1. Generate cryptographic credentials for secure communication
2. Start listening on the specified address for other cluster members
3. Use multicast DNS to discover other devices with the same secret
4. Negotiate cluster topology and establish secure connections
5. Complete when the expected number of devices have joined (or run indefinitely if expected-size is 0)

The secret must be shared among all devices that should join the cluster.
The address should be accessible by other devices on the network.

Examples:
  snap cluster assemble --secret=my-cluster-secret --address=192.168.1.100:8080
  snap cluster assemble --secret=my-cluster-secret --address=192.168.1.100:8080 --expected-size=3
  snap cluster assemble --secret=my-cluster-secret --address=192.168.1.100:8080 --domain=mycompany.local
`)

func init() {
	addClusterCommand("assemble", shortClusterAssembleHelp, longClusterAssembleHelp, func() flags.Commander {
		return &cmdClusterAssemble{}
	}, waitDescs.also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"secret": i18n.G("Shared secret for cluster authentication"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"address": i18n.G("IP:port address to bind for cluster assembly"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"expected-size": i18n.G("Expected number of devices in cluster (0 for indefinite)"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"domain": i18n.G("Domain used with mDNS device discovery (default: local)"),
	}), nil)
}

func (x *cmdClusterAssemble) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.ClusterAssembleOptions{
		Secret:       x.Secret,
		Address:      x.Address,
		ExpectedSize: x.ExpectedSize,
		Domain:       x.Domain,
	}

	changeID, err := x.client.ClusterAssemble(opts)
	if err != nil {
		return err
	}

	if _, err := x.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Cluster assembly completed successfully.\n"))
	return nil
}
