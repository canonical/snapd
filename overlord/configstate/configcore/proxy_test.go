// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2017-2022 Canonical Ltd
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
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/state"
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
	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755))

	s.mockEtcEnvironment = filepath.Join(dirs.GlobalRootDir, "/etc/environment")

	s.storeSigning = assertstest.NewStoreStack("canonical", nil)

	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	}))


	s.state.Lock()
	assertstate.ReplaceDB(s.state, db)
	s.state.Unlock()
	mylog.Check(db.Add(s.storeSigning.StoreAccountKey("")))

}

func (s *proxySuite) makeMockEtcEnvironment(c *C) {
	mylog.Check(os.WriteFile(s.mockEtcEnvironment, []byte(`
PATH="/usr/bin"
`), 0644))

}

func (s *proxySuite) TestConfigureProxyUnhappy(c *C) {
	dirs.SetRootDir(c.MkDir())
	mylog.Check(configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"proxy.http": "http://example.com",
		},
	}))
	c.Assert(err, ErrorMatches, "open .*/etc/environment: no such file or directory")
}

func (s *proxySuite) TestConfigureProxy(c *C) {
	for _, proto := range []string{"http", "https", "ftp"} {
		// populate with content
		s.makeMockEtcEnvironment(c)
		mylog.Check(configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				fmt.Sprintf("proxy.%s", proto): fmt.Sprintf("%s://example.com", proto),
			},
		}))


		c.Check(s.mockEtcEnvironment, testutil.FileEquals, fmt.Sprintf(`
PATH="/usr/bin"
%[1]s_proxy=%[1]s://example.com`, proto))
	}
}

func (s *proxySuite) TestConfigureNoProxy(c *C) {
	// populate with content
	s.makeMockEtcEnvironment(c)
	mylog.Check(configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"proxy.no-proxy": "example.com,bar.com",
		},
	}))


	c.Check(s.mockEtcEnvironment, testutil.FileEquals, `
PATH="/usr/bin"
no_proxy=example.com,bar.com`)
}

func (s *proxySuite) TestConfigureProxyStore(c *C) {
	sessionResets := 0
	defer configcore.MockDevicestateResetSession(func(s *state.State) error {
		s.Unlock()
		defer s.Lock()
		sessionResets++
		return nil
	})()
	mylog.

		// no change
		Check(configcore.Run(classicDev, &mockConf{
			state: s.state,
		}))
	c.Check(err, IsNil)
	mylog.

		// no related change
		Check(configcore.Run(classicDev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"refresh.rate-limit": "1MB",
			},
		}))
	c.Check(err, IsNil)
	mylog.

		// set to ""
		Check(configcore.Run(classicDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"proxy.store": "",
			},
		}))
	c.Check(err, IsNil)

	// no assertion
	conf := &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"proxy.store": "foo",
		},
	}
	mylog.Check(configcore.Run(classicDev, conf))
	c.Check(err, ErrorMatches, `cannot set proxy.store to "foo" without a matching store assertion`)

	c.Check(sessionResets, Equals, 0)

	operatorAcct := assertstest.NewAccount(s.storeSigning, "foo-operator", nil, "")
	// have a store assertion
	stoAs := mylog.Check2(s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "foo",
		"operator-id": operatorAcct.AccountID(),
		"url":         "http://store.interal:9943",
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, ""))

	func() {
		s.state.Lock()
		defer s.state.Unlock()
		assertstatetest.AddMany(s.state, operatorAcct, stoAs)
	}()
	mylog.Check(configcore.Run(classicDev, conf))
	c.Check(err, IsNil)

	c.Check(sessionResets, Equals, 1)

	// no value change
	conf = &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"proxy.store": "foo",
		},
		changes: map[string]interface{}{
			"proxy.store": "foo",
		},
	}
	mylog.Check(configcore.Run(classicDev, conf))
	c.Check(err, IsNil)

	c.Check(sessionResets, Equals, 1)
}

func (s *proxySuite) TestConfigureProxyStoreNoURL(c *C) {
	conf := &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"proxy.store": "foo",
		},
	}

	operatorAcct := assertstest.NewAccount(s.storeSigning, "foo-operator", nil, "")
	// have a store assertion but no url
	stoAs := mylog.Check2(s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "foo",
		"operator-id": operatorAcct.AccountID(),
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, ""))

	func() {
		s.state.Lock()
		defer s.state.Unlock()
		assertstatetest.AddMany(s.state, operatorAcct, stoAs)
	}()
	mylog.Check(configcore.Run(coreDev, conf))
	c.Check(err, ErrorMatches, `cannot set proxy.store to "foo" with a matching store assertion with url unset`)
}
