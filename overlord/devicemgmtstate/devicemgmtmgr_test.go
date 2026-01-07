// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025-2026 Canonical Ltd
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

package devicemgmtstate_test

import (
	"bytes"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicemgmtstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type deviceMgmtMgrSuite struct {
	testutil.BaseTest

	st         *state.State
	o          *overlord.Overlord
	storeStack *assertstest.StoreStack
	mgr        *devicemgmtstate.DeviceMgmtManager
	logbuf     *bytes.Buffer
}

var _ = Suite(&deviceMgmtMgrSuite{})

func (s *deviceMgmtMgrSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.o = overlord.Mock()
	s.st = s.o.State()

	s.st.Lock()
	defer s.st.Unlock()

	s.storeStack = assertstest.NewStoreStack("my-brand", nil)

	runner := s.o.TaskRunner()
	s.o.AddManager(runner)

	s.mgr = devicemgmtstate.Manager(s.st, runner, nil)
	s.o.AddManager(s.mgr)

	err := s.o.StartUp()
	c.Assert(err, IsNil)

	var restoreLogger func()
	s.logbuf, restoreLogger = logger.MockLogger()
	s.AddCleanup(restoreLogger)
}

func (s *deviceMgmtMgrSuite) setFeatureFlag(c *C, value bool) {
	_, confOption := features.RemoteDeviceManagement.ConfigOption()

	tr := config.NewTransaction(s.st)
	err := tr.Set("core", confOption, value)
	c.Assert(err, IsNil)
	tr.Commit()
}

func (s *deviceMgmtMgrSuite) TestShouldExchangeMessages(c *C) {
	type test struct {
		name           string
		featureFlagOn  bool
		lastExchange   time.Time
		readyResponses map[string]store.Message
		expectedShould bool
		expectedLimit  int
	}

	wayback := time.Date(2025, 6, 14, 12, 0, 0, 0, time.UTC)
	restoreTime := devicemgmtstate.MockTimeNow(wayback)
	defer restoreTime()

	tooSoon := wayback.Add(-5 * time.Second)
	enoughTimePassed := wayback.Add(-2 * devicemgmtstate.DefaultExchangeInterval)

	tests := []test{
		{
			name:         "feature flag off, no responses, too soon",
			lastExchange: tooSoon,
		},
		{
			name:         "feature flag off, no responses, enough time passed",
			lastExchange: enoughTimePassed,
		},
		{
			name:           "feature flag off, has responses, too soon",
			lastExchange:   tooSoon,
			readyResponses: map[string]store.Message{"mesg-1": {}},
		},
		{
			name:           "feature flag off, has responses, enough time passed",
			lastExchange:   enoughTimePassed,
			readyResponses: map[string]store.Message{"mesg-1": {}},
			expectedShould: true,
		},
		{
			name:           "feature flag on, too soon",
			featureFlagOn:  true,
			lastExchange:   tooSoon,
			expectedShould: false,
		},
		{
			name:           "feature flag on, enough time passed",
			featureFlagOn:  true,
			lastExchange:   enoughTimePassed,
			expectedShould: true,
			expectedLimit:  devicemgmtstate.DefaultExchangeLimit,
		},
	}

	s.st.Lock()
	defer s.st.Unlock()

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		ms := &devicemgmtstate.DeviceMgmtState{
			LastExchange:   tt.lastExchange,
			ReadyResponses: tt.readyResponses,
		}

		s.setFeatureFlag(c, tt.featureFlagOn)

		should, cfg := s.mgr.ShouldExchangeMessages(ms)
		c.Check(should, Equals, tt.expectedShould, cmt)
		c.Check(cfg.Limit, Equals, tt.expectedLimit, cmt)
	}
}

func (s *deviceMgmtMgrSuite) TestEnsureOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.setFeatureFlag(c, true)

	s.st.Unlock()
	err := s.mgr.Ensure()
	s.st.Lock()
	c.Assert(err, IsNil)

	changes := s.st.Changes()
	c.Assert(changes, HasLen, 1)
	chg := changes[0]
	c.Check(chg.Kind(), Equals, "device-management-cycle")
	c.Check(chg.Summary(), Equals, "Process device management messages")

	tasks := chg.Tasks()
	c.Check(tasks, HasLen, 2)

	exchange := tasks[0]
	c.Check(exchange.Kind(), Equals, "mgmt-exchange-messages")
	c.Check(exchange.Summary(), Equals, "Exchange messages with the Store")

	var cfg devicemgmtstate.ExchangeConfig
	err = exchange.Get("config", &cfg)
	c.Assert(err, IsNil)
	c.Check(cfg.Limit, Equals, devicemgmtstate.DefaultExchangeLimit)

	dispatch := tasks[1]
	c.Check(dispatch.Kind(), Equals, "mgmt-dispatch-messages")
	c.Check(dispatch.Summary(), Equals, "Dispatch message(s) to subsystems")
	c.Check(dispatch.WaitTasks(), DeepEquals, []*state.Task{exchange})
}

func (s *deviceMgmtMgrSuite) TestEnsureChangeAlreadyInFlight(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.setFeatureFlag(c, true)

	ms := &devicemgmtstate.DeviceMgmtState{
		LastExchange: time.Now().Add(-10 * time.Minute),
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("device-management-cycle", "Process device management messages")
	chg.SetStatus(state.DoingStatus)

	s.st.Unlock()
	err := s.mgr.Ensure()
	s.st.Lock()
	c.Assert(err, IsNil)

	changes := s.st.Changes()
	c.Assert(changes, HasLen, 1)
	c.Check(changes[0].ID(), Equals, chg.ID())
}

func (s *deviceMgmtMgrSuite) TestEnsureFeatureDisabled(c *C) {
	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()

	changes := s.st.Changes()
	c.Assert(changes, HasLen, 0)
}
