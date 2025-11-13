// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package clusterstate_test

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"time"

	"github.com/snapcore/snapd/overlord/clusterstate"
	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/check.v1"
)

type assembleSuite struct{}

var _ = check.Suite(&assembleSuite{})

func (s *assembleSuite) TestAssembleSuccess(c *check.C) {
	st, _ := newStateWithStoreStack(c)

	st.Lock()
	defer st.Unlock()

	// check to ensure that we don't prevent assembly when old changes are done
	done := st.NewChange("assemble-cluster", "previous assembly")
	done.SetStatus(state.DoneStatus)

	opts := clusterstate.AssembleOptions{
		Secret:       "secret",
		Address:      "10.0.0.5:8081",
		ExpectedSize: 4,
		Period:       15 * time.Second,
	}

	ts, err := clusterstate.Assemble(st, opts)
	c.Assert(err, check.IsNil)

	tasks := ts.Tasks()
	c.Assert(tasks, check.HasLen, 1)

	task := tasks[0]
	c.Assert(task.Kind(), check.Equals, "assemble-cluster")
	c.Assert(task.Summary(), check.Equals, "Assemble cluster")

	var setup clusterstate.AssembleClusterSetup
	err = task.Get("assemble-cluster-setup", &setup)
	c.Assert(err, check.IsNil)

	c.Assert(setup.Secret, check.Equals, opts.Secret)
	c.Assert(setup.RDT, check.Not(check.Equals), "")
	c.Assert(setup.IP, check.Equals, "10.0.0.5")
	c.Assert(setup.Port, check.Equals, 8081)
	c.Assert(setup.ExpectedSize, check.Equals, opts.ExpectedSize)
	c.Assert(setup.Period, check.Equals, opts.Period)
	c.Assert(len(setup.TLSCert) > 0, check.Equals, true)
	c.Assert(len(setup.TLSKey) > 0, check.Equals, true)

	certBlock, _ := pem.Decode(setup.TLSCert)
	c.Assert(certBlock, check.NotNil)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	c.Assert(err, check.IsNil)
	c.Assert(cert.IPAddresses, check.HasLen, 1)
	c.Assert(cert.IPAddresses[0].Equal(net.ParseIP("10.0.0.5")), check.Equals, true)
	c.Assert(cert.NotAfter.Sub(cert.NotBefore), check.Equals, time.Hour)

	keyBlock, _ := pem.Decode(setup.TLSKey)
	c.Assert(keyBlock, check.NotNil)
	_, err = x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	c.Assert(err, check.IsNil)
}

func (s *assembleSuite) TestAssembleInputValidation(c *check.C) {
	st, _ := newStateWithStoreStack(c)

	st.Lock()
	defer st.Unlock()

	cases := []struct {
		name    string
		opts    clusterstate.AssembleOptions
		pattern string
	}{
		{
			name: "missing secret",
			opts: clusterstate.AssembleOptions{
				Address: "127.0.0.1:1234",
			},
			pattern: "secret is required",
		},
		{
			name: "missing address",
			opts: clusterstate.AssembleOptions{
				Secret: "secret",
			},
			pattern: "address is required",
		},
		{
			name: "missing port",
			opts: clusterstate.AssembleOptions{
				Secret:  "secret",
				Address: "127.0.0.1",
			},
			pattern: ".*missing port in address",
		},
		{
			name: "invalid ip",
			opts: clusterstate.AssembleOptions{
				Secret:  "secret",
				Address: "example.com:1234",
			},
			pattern: "invalid IP address in address",
		},
		{
			name: "invalid port",
			opts: clusterstate.AssembleOptions{
				Secret:  "secret",
				Address: "127.0.0.1:notaport",
			},
			pattern: `invalid port in address: strconv.Atoi: parsing "notaport": invalid syntax`,
		},
	}

	for _, tc := range cases {
		c.Logf("case: %s", tc.name)

		_, err := clusterstate.Assemble(st, tc.opts)
		c.Assert(err, check.ErrorMatches, tc.pattern)
	}
}

func (s *assembleSuite) TestAssembleInProgress(c *check.C) {
	st, _ := newStateWithStoreStack(c)

	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("assemble-cluster", "existing assembly")
	chg.SetStatus(state.DoStatus)

	ts, err := clusterstate.Assemble(st, clusterstate.AssembleOptions{
		Secret:  "secret",
		Address: "127.0.0.1:1234",
	})
	c.Assert(err, check.ErrorMatches, "cluster assembly is already in progress")
	c.Assert(ts, check.IsNil)
}
