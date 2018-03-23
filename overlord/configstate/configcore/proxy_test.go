// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package configcore_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type proxySuite struct {
	configcoreSuite

	mockEtcEnvironment string

	storeSigning *assertstest.StoreStack
}

var _ = Suite(&proxySuite{})

func (s *proxySuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)
	s.mockEtcEnvironment = filepath.Join(dirs.GlobalRootDir, "/etc/environment")

	s.storeSigning = assertstest.NewStoreStack("canonical", nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	})
	c.Assert(err, IsNil)

	s.state.Lock()
	assertstate.ReplaceDB(s.state, db)
	s.state.Unlock()

	err = db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
}

func (s *proxySuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *proxySuite) makeMockEtcEnvironment(c *C) {
	err := ioutil.WriteFile(s.mockEtcEnvironment, []byte(`
PATH="/usr/bin"
`), 0644)
	c.Assert(err, IsNil)
}

func (s *proxySuite) TestConfigureProxy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for _, proto := range []string{"http", "https", "ftp"} {
		// populate with content
		s.makeMockEtcEnvironment(c)

		err := configcore.Run(&mockConf{
			state: s.state,
			conf: map[string]interface{}{
				fmt.Sprintf("proxy.%s", proto): fmt.Sprintf("%s://example.com", proto),
			},
		})
		c.Assert(err, IsNil)

		c.Check(s.mockEtcEnvironment, testutil.FileEquals, fmt.Sprintf(`
PATH="/usr/bin"
%[1]s_proxy=%[1]s://example.com`, proto))
	}
}

func (s *proxySuite) TestConfigureNoProxy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// populate with content
	s.makeMockEtcEnvironment(c)
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"proxy.no-proxy": "example.com,bar.com",
		},
	})
	c.Assert(err, IsNil)

	c.Check(s.mockEtcEnvironment, testutil.FileEquals, `
PATH="/usr/bin"
no_proxy=example.com,bar.com`)
}

func (s *proxySuite) TestConfigureProxyStore(c *C) {
	// set to ""
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"proxy.store": "",
		},
	})
	c.Check(err, IsNil)

	// no assertion
	conf := &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"proxy.store": "foo",
		},
	}

	err = configcore.Run(conf)
	c.Check(err, ErrorMatches, `cannot set proxy.store to "foo" without a matching store assertion`)

	operatorAcct := assertstest.NewAccount(s.storeSigning, "foo-operator", nil, "")
	s.state.Lock()
	err = assertstate.Add(s.state, operatorAcct)
	s.state.Unlock()
	c.Assert(err, IsNil)

	// have a store assertion.
	stoAs, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "foo",
		"operator-id": operatorAcct.AccountID(),
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.state.Lock()
	err = assertstate.Add(s.state, stoAs)
	s.state.Unlock()
	c.Assert(err, IsNil)

	err = configcore.Run(conf)
	c.Check(err, IsNil)
}
