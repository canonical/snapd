// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package devstate_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/asserts/systestkeys"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devstate"
	"github.com/snapcore/snapd/overlord/state"
)

func TestDeviceManager(t *testing.T) { TestingT(t) }

type deviceManagerSuite struct {
	state     *state.State
	manager   *devstate.DeviceManager
	assertMgr *assertstate.AssertManager
}

var _ = Suite(&deviceManagerSuite{})

func (s *deviceManagerSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.state = state.New(nil)

	// Inject trusted assertions that we can chain from while we
	// construct the AssertManager.
	restore := sysdb.InjectTrusted([]asserts.Assertion{systestkeys.TestRootAccount, systestkeys.TestRootAccountKey})
	assertMgr, err := assertstate.Manager(s.state)
	restore()

	c.Assert(err, IsNil)
	s.assertMgr = assertMgr
	manager, err := devstate.Manager(s.state, s.assertMgr)
	c.Assert(err, IsNil)
	s.manager = manager
}

func (s *deviceManagerSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *deviceManagerSuite) TestSmoke(c *C) {
	s.manager.Ensure()
	s.manager.Wait()
}

// addSerialAssertion constructs a trustworthy serial assertion and adds
// it to the database.
func addSerialAssertion(assertMgr *assertstate.AssertManager, brand string, model string, serial string) (*asserts.Serial, error) {
	rootPrivKey, _ := assertstest.ReadPrivKey(systestkeys.TestRootPrivKey)
	assertMgr.DB().ImportKey("testrootorg", rootPrivKey)

	devPrivKey, _ := assertstest.GenerateKey(512)
	deviceKeyEncoded, err := asserts.EncodePublicKey(devPrivKey.PublicKey())
	if err != nil {
		return nil, err
	}

	// XXX: asserts doesn't check model consistency!
	headers := map[string]string{
		"authority-id": brand,
		"brand-id":     brand,
		"model":        model,
		"serial":       serial,
		"device-key":   string(deviceKeyEncoded),
		"timestamp":    "2016-07-07T04:52:00Z",
	}
	serialAssertion, err := assertMgr.DB().Sign(asserts.SerialType, headers, []byte{}, rootPrivKey.PublicKey().ID())
	if err != nil {
		return nil, err
	}
	err = assertMgr.DB().Add(serialAssertion)
	if err != nil {
		return nil, err
	}
	return serialAssertion.(*asserts.Serial), nil
}

func (s *deviceManagerSuite) TestSetDeviceIdentityTask(c *C) {
	_, err := addSerialAssertion(s.assertMgr, "testrootorg", "the-model", "the-serial")
	c.Assert(err, IsNil)

	// Create a task to set the identity.
	s.state.Lock()
	change := s.state.NewChange("kind", "summary")
	ts, err := devstate.SetDeviceIdentity(s.state, "testrootorg", "the-model", "the-serial")
	c.Assert(err, IsNil)
	change.AddAll(ts)
	s.state.Unlock()

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	// The task has succeeded.
	task := change.Tasks()[0]
	c.Check(task.Kind(), Equals, "set-device-identity")
	c.Check(task.Summary(), Equals, `Set device identity to brand "testrootorg", model "the-model", serial "the-serial"`)
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(change.Status(), Equals, state.DoneStatus)
	c.Check(change.Err(), IsNil)

	// The device identity is updated in state.
	dev, err := auth.Device(s.state)
	c.Check(err, IsNil)
	c.Check(dev.Brand, Equals, "testrootorg")
	c.Check(dev.Model, Equals, "the-model")
	c.Check(dev.Serial, Equals, "the-serial")
}

func (s *deviceManagerSuite) TestSetDeviceIdentityTaskNoSerialAssertion(c *C) {
	_, err := addSerialAssertion(s.assertMgr, "testrootorg", "the-model", "the-serial")
	c.Assert(err, IsNil)

	// Create a task to set the identity.
	s.state.Lock()
	change := s.state.NewChange("kind", "summary")
	ts, err := devstate.SetDeviceIdentity(s.state, "testrootorg", "the-model", "wrong-serial")
	c.Assert(err, IsNil)
	change.AddAll(ts)
	s.state.Unlock()

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	// The task has failed.
	task := change.Tasks()[0]
	c.Check(task.Kind(), Equals, "set-device-identity")
	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log()[len(task.Log())-1], Matches, ".* ERROR no matching serial assertion")
	c.Check(change.Status(), Equals, state.ErrorStatus)

	// The device identity is unchanged in state.
	dev, err := auth.Device(s.state)
	c.Check(err, IsNil)
	c.Check(dev.Brand, Equals, "")
	c.Check(dev.Model, Equals, "")
	c.Check(dev.Serial, Equals, "")
}
