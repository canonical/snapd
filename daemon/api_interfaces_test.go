// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

package daemon_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/state"
)

var _ = check.Suite(&interfacesSuite{})

type interfacesSuite struct {
	apiBaseSuite
}

func (s *interfacesSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage-interfaces"})
}

func mockIface(c *check.C, d *daemon.Daemon, iface interfaces.Interface) {
	mylog.Check(d.Overlord().InterfaceManager().Repository().AddInterface(iface))
	c.Assert(err, check.IsNil)
}

// inverseCaseMapper implements SnapMapper to use lower case internally and upper case externally.
type inverseCaseMapper struct {
	ifacestate.IdentityMapper // Embed the identity mapper to reuse empty state mapping functions.
}

func (m *inverseCaseMapper) RemapSnapFromRequest(snapName string) string {
	return strings.ToLower(snapName)
}

func (m *inverseCaseMapper) RemapSnapToResponse(snapName string) string {
	return strings.ToUpper(snapName)
}

func (m *inverseCaseMapper) SystemSnapName() string {
	return "core"
}

// Tests for POST /v2/interfaces

const (
	consumerYaml = `
name: consumer
version: 1
apps:
 app:
plugs:
 plug:
  interface: test
  key: value
  label: label
`

	producerYaml = `
name: producer
version: 1
apps:
 app:
slots:
 slot:
  interface: test
  key: value
  label: label
`

	coreProducerYaml = `
name: core
version: 1
slots:
 slot:
  interface: test
  key: value
  label: label
`

	differentProducerYaml = `
name: producer
version: 1
apps:
 app:
slots:
 slot:
  interface: different
  key: value
  label: label
`
)

func (s *interfacesSuite) TestConnectPlugSuccess(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()
	// Install an inverse case mapper to exercise the interface mapping at the same time.
	restore = ifacestate.MockSnapMapper(&inverseCaseMapper{})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	action := &client.InterfaceAction{
		Action: "connect",
		Plugs:  []client.Plug{{Snap: "CONSUMER", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "PRODUCER", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	mylog.Check(chg.Err())
	st.Unlock()
	c.Assert(err, check.IsNil)

	repo := d.Overlord().InterfaceManager().Repository()
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, check.HasLen, 1)
	c.Check(ifaces.Connections, check.DeepEquals, []*interfaces.ConnRef{{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}})
}

func (s *interfacesSuite) TestConnectPlugFailureInterfaceMismatch(c *check.C) {
	d := s.daemon(c)

	mockIface(c, d, &ifacetest.TestInterface{InterfaceName: "test"})
	mockIface(c, d, &ifacetest.TestInterface{InterfaceName: "different"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, differentProducerYaml)

	action := &client.InterfaceAction{
		Action: "connect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "cannot connect consumer:plug (\"test\" interface) to producer:slot (\"different\" interface)",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
	repo := d.Overlord().InterfaceManager().Repository()
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, check.HasLen, 0)
}

func (s *interfacesSuite) TestConnectPlugFailureNoSuchPlug(c *check.C) {
	d := s.daemon(c)

	mockIface(c, d, &ifacetest.TestInterface{InterfaceName: "test"})
	// there is no consumer, no plug defined
	s.mockSnap(c, producerYaml)
	s.mockSnap(c, consumerYaml)

	action := &client.InterfaceAction{
		Action: "connect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "missingplug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)

	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "snap \"consumer\" has no plug named \"missingplug\"",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})

	repo := d.Overlord().InterfaceManager().Repository()
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, check.HasLen, 0)
}

func (s *interfacesSuite) TestConnectAlreadyConnected(c *check.C) {
	d := s.daemon(c)

	mockIface(c, d, &ifacetest.TestInterface{InterfaceName: "test"})
	// there is no consumer, no plug defined
	s.mockSnap(c, producerYaml)
	s.mockSnap(c, consumerYaml)

	repo := d.Overlord().InterfaceManager().Repository()
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	_ := mylog.Check2(repo.Connect(connRef, nil, nil, nil, nil, nil))
	c.Assert(err, check.IsNil)
	conns := map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"auto": false,
		},
	}
	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", conns)
	st.Unlock()

	action := &client.InterfaceAction{
		Action: "connect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st.Lock()
	chg := st.Change(id)
	c.Assert(chg.Tasks(), check.HasLen, 0)
	c.Assert(chg.Status(), check.Equals, state.DoneStatus)
	st.Unlock()
}

func (s *interfacesSuite) TestConnectPlugFailureNoSuchSlot(c *check.C) {
	d := s.daemon(c)

	mockIface(c, d, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	// there is no producer, no slot defined

	action := &client.InterfaceAction{
		Action: "connect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "missingslot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)

	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "snap \"producer\" has no slot named \"missingslot\"",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})

	repo := d.Overlord().InterfaceManager().Repository()
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, check.HasLen, 0)
}

func (s *interfacesSuite) testConnectFailureNoSnap(c *check.C, installedSnap string) {
	// validity, either consumer or producer needs to be enabled
	consumer := installedSnap == "consumer"
	producer := installedSnap == "producer"
	c.Assert(consumer || producer, check.Equals, true, check.Commentf("installed snap must be consumer or producer"))

	d := s.daemon(c)

	mockIface(c, d, &ifacetest.TestInterface{InterfaceName: "test"})

	if consumer {
		s.mockSnap(c, consumerYaml)
	}
	if producer {
		s.mockSnap(c, producerYaml)
	}

	action := &client.InterfaceAction{
		Action: "connect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)

	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	if producer {
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"result": map[string]interface{}{
				"message": "snap \"consumer\" is not installed",
			},
			"status":      "Bad Request",
			"status-code": 400.0,
			"type":        "error",
		})
	} else {
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"result": map[string]interface{}{
				"message": "snap \"producer\" is not installed",
			},
			"status":      "Bad Request",
			"status-code": 400.0,
			"type":        "error",
		})
	}
}

func (s *interfacesSuite) TestConnectPlugFailureNoPlugSnap(c *check.C) {
	s.testConnectFailureNoSnap(c, "producer")
}

func (s *interfacesSuite) TestConnectPlugFailureNoSlotSnap(c *check.C) {
	s.testConnectFailureNoSnap(c, "consumer")
}

func (s *interfacesSuite) TestConnectPlugChangeConflict(c *check.C) {
	d := s.daemon(c)

	mockIface(c, d, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	// there is no producer, no slot defined

	s.simulateConflict("consumer")

	action := &client.InterfaceAction{
		Action: "connect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 409)

	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"status-code": 409.,
		"status":      "Conflict",
		"result": map[string]interface{}{
			"message": `snap "consumer" has "manip" change in progress`,
			"kind":    "snap-change-conflict",
			"value": map[string]interface{}{
				"change-kind": "manip",
				"snap-name":   "consumer",
			},
		},
		"type": "error",
	})
}

func (s *interfacesSuite) TestConnectCoreSystemAlias(c *check.C) {
	revert := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer revert()
	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, coreProducerYaml)

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	action := &client.InterfaceAction{
		Action: "connect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "system", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	mylog.Check(chg.Err())
	st.Unlock()
	c.Assert(err, check.IsNil)

	repo := d.Overlord().InterfaceManager().Repository()
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, check.HasLen, 1)
	c.Check(ifaces.Connections, check.DeepEquals, []*interfaces.ConnRef{{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "slot"},
	}})
}

func (s *interfacesSuite) testDisconnect(c *check.C, plugSnap, plugName, slotSnap, slotName string) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()
	// Install an inverse case mapper to exercise the interface mapping at the same time.
	restore = ifacestate.MockSnapMapper(&inverseCaseMapper{})
	defer restore()
	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	repo := d.Overlord().InterfaceManager().Repository()
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	_ := mylog.Check2(repo.Connect(connRef, nil, nil, nil, nil, nil))
	c.Assert(err, check.IsNil)

	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
		},
	})
	st.Unlock()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	action := &client.InterfaceAction{
		Action: "disconnect",
		Plugs:  []client.Plug{{Snap: plugSnap, Name: plugName}},
		Slots:  []client.Slot{{Snap: slotSnap, Name: slotName}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	mylog.Check(chg.Err())
	st.Unlock()
	c.Assert(err, check.IsNil)

	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, check.HasLen, 0)
}

func (s *interfacesSuite) TestDisconnectPlugSuccess(c *check.C) {
	s.testDisconnect(c, "CONSUMER", "plug", "PRODUCER", "slot")
}

func (s *interfacesSuite) TestDisconnectPlugSuccessWithEmptyPlug(c *check.C) {
	s.testDisconnect(c, "", "", "PRODUCER", "slot")
}

func (s *interfacesSuite) TestDisconnectPlugSuccessWithEmptySlot(c *check.C) {
	s.testDisconnect(c, "CONSUMER", "plug", "", "")
}

func (s *interfacesSuite) TestDisconnectPlugFailureNoSuchPlug(c *check.C) {
	revert := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer revert()
	s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	action := &client.InterfaceAction{
		Action: "disconnect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "missingplug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "snap \"consumer\" has no plug named \"missingplug\"",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func (s *interfacesSuite) testDisconnectFailureNoSnap(c *check.C, installedSnap string) {
	// validity, either consumer or producer needs to be enabled
	consumer := installedSnap == "consumer"
	producer := installedSnap == "producer"
	c.Assert(consumer || producer, check.Equals, true, check.Commentf("installed snap must be consumer or producer"))

	revert := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer revert()
	s.daemon(c)

	if consumer {
		s.mockSnap(c, consumerYaml)
	}
	if producer {
		s.mockSnap(c, producerYaml)
	}

	action := &client.InterfaceAction{
		Action: "disconnect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)

	if producer {
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"result": map[string]interface{}{
				"message": "snap \"consumer\" is not installed",
			},
			"status":      "Bad Request",
			"status-code": 400.0,
			"type":        "error",
		})
	} else {
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"result": map[string]interface{}{
				"message": "snap \"producer\" is not installed",
			},
			"status":      "Bad Request",
			"status-code": 400.0,
			"type":        "error",
		})
	}
}

func (s *interfacesSuite) TestDisconnectPlugFailureNoPlugSnap(c *check.C) {
	s.testDisconnectFailureNoSnap(c, "producer")
}

func (s *interfacesSuite) TestDisconnectPlugFailureNoSlotSnap(c *check.C) {
	s.testDisconnectFailureNoSnap(c, "consumer")
}

func (s *interfacesSuite) TestDisconnectPlugNothingToDo(c *check.C) {
	revert := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer revert()
	s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	action := &client.InterfaceAction{
		Action: "disconnect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "", Name: ""}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "nothing to do",
			"kind":    "interfaces-unchanged",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func (s *interfacesSuite) TestDisconnectPlugFailureNoSuchSlot(c *check.C) {
	revert := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer revert()
	s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	action := &client.InterfaceAction{
		Action: "disconnect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "missingslot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)

	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "snap \"producer\" has no slot named \"missingslot\"",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func (s *interfacesSuite) TestDisconnectPlugFailureNotConnected(c *check.C) {
	revert := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer revert()
	s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	action := &client.InterfaceAction{
		Action: "disconnect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)

	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "cannot disconnect consumer:plug from producer:slot, it is not connected",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func (s *interfacesSuite) TestDisconnectForgetPlugFailureNotConnected(c *check.C) {
	revert := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer revert()
	s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	action := &client.InterfaceAction{
		Action: "disconnect",
		Forget: true,
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)

	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "cannot forget connection consumer:plug from producer:slot, it was not connected",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func (s *interfacesSuite) TestDisconnectConflict(c *check.C) {
	revert := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer revert()
	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	repo := d.Overlord().InterfaceManager().Repository()
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	_ := mylog.Check2(repo.Connect(connRef, nil, nil, nil, nil, nil))
	c.Assert(err, check.IsNil)

	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
		},
	})
	st.Unlock()

	s.simulateConflict("consumer")

	action := &client.InterfaceAction{
		Action: "disconnect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "producer", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)

	c.Check(rec.Code, check.Equals, 409)

	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"status-code": 409.,
		"status":      "Conflict",
		"result": map[string]interface{}{
			"message": `snap "consumer" has "manip" change in progress`,
			"kind":    "snap-change-conflict",
			"value": map[string]interface{}{
				"change-kind": "manip",
				"snap-name":   "consumer",
			},
		},
		"type": "error",
	})
}

func (s *interfacesSuite) TestDisconnectCoreSystemAlias(c *check.C) {
	revert := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer revert()
	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, coreProducerYaml)

	repo := d.Overlord().InterfaceManager().Repository()
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "slot"},
	}
	_ := mylog.Check2(repo.Connect(connRef, nil, nil, nil, nil, nil))
	c.Assert(err, check.IsNil)

	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", map[string]interface{}{
		"consumer:plug core:slot": map[string]interface{}{
			"interface": "test",
		},
	})
	st.Unlock()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	action := &client.InterfaceAction{
		Action: "disconnect",
		Plugs:  []client.Plug{{Snap: "consumer", Name: "plug"}},
		Slots:  []client.Slot{{Snap: "system", Name: "slot"}},
	}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	mylog.Check(chg.Err())
	st.Unlock()
	c.Assert(err, check.IsNil)

	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, check.HasLen, 0)
}

func (s *interfacesSuite) TestUnsupportedInterfaceRequest(c *check.C) {
	s.daemon(c)
	buf := bytes.NewBuffer([]byte(`garbage`))
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "cannot decode request body into an interface action: invalid character 'g' looking for beginning of value",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func (s *interfacesSuite) TestMissingInterfaceAction(c *check.C) {
	s.daemon(c)
	action := &client.InterfaceAction{}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "interface action not specified",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func (s *interfacesSuite) TestUnsupportedInterfaceAction(c *check.C) {
	s.daemon(c)
	action := &client.InterfaceAction{Action: "foo"}
	text := mylog.Check2(json.Marshal(action))
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req := mylog.Check2(http.NewRequest("POST", "/v2/interfaces", buf))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "unsupported interface action: \"foo\"",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

// Tests for GET /v2/interfaces

func (s *interfacesSuite) TestInterfacesLegacy(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()
	// Install an inverse case mapper to exercise the interface mapping at the same time.
	restore = ifacestate.MockSnapMapper(&inverseCaseMapper{})
	defer restore()

	d := s.daemon(c)

	anotherConsumerYaml := `
name: another-consumer-%s
version: 1
apps:
 app:
plugs:
 plug:
  interface: test
  key: value
  label: label
`
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, fmt.Sprintf(anotherConsumerYaml, "def"))
	s.mockSnap(c, fmt.Sprintf(anotherConsumerYaml, "abc"))
	s.mockSnap(c, producerYaml)

	repo := d.Overlord().InterfaceManager().Repository()
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	_ := mylog.Check2(repo.Connect(connRef, nil, nil, nil, nil, nil))
	c.Assert(err, check.IsNil)

	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
			"auto":      true,
		},
		"another-consumer-def:plug producer:slot": map[string]interface{}{
			"interface": "test",
			"by-gadget": true,
			"auto":      true,
		},
		"another-consumer-abc:plug producer:slot": map[string]interface{}{
			"interface": "test",
			"by-gadget": true,
			"auto":      true,
		},
	})
	st.Unlock()

	req := mylog.Check2(http.NewRequest("GET", "/v2/interfaces", nil))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"plugs": []interface{}{
				map[string]interface{}{
					"snap":      "another-consumer-abc",
					"plug":      "plug",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "producer", "slot": "slot"},
					},
				},
				map[string]interface{}{
					"snap":      "another-consumer-def",
					"plug":      "plug",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "producer", "slot": "slot"},
					},
				},
				map[string]interface{}{
					"snap":      "consumer",
					"plug":      "plug",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "producer", "slot": "slot"},
					},
				},
			},
			"slots": []interface{}{
				map[string]interface{}{
					"snap":      "producer",
					"slot":      "slot",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "another-consumer-abc", "plug": "plug"},
						map[string]interface{}{"snap": "another-consumer-def", "plug": "plug"},
						map[string]interface{}{"snap": "consumer", "plug": "plug"},
					},
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestInterfacesModern(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()
	// Install an inverse case mapper to exercise the interface mapping at the same time.
	restore = ifacestate.MockSnapMapper(&inverseCaseMapper{})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	repo := d.Overlord().InterfaceManager().Repository()
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	_ := mylog.Check2(repo.Connect(connRef, nil, nil, nil, nil, nil))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("GET", "/v2/interfaces?select=connected&doc=true&plugs=true&slots=true", nil))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": []interface{}{
			map[string]interface{}{
				"name": "test",
				"plugs": []interface{}{
					map[string]interface{}{
						"snap":  "consumer",
						"plug":  "plug",
						"label": "label",
						"attrs": map[string]interface{}{
							"key": "value",
						},
					},
				},
				"slots": []interface{}{
					map[string]interface{}{
						"snap":  "producer",
						"slot":  "slot",
						"label": "label",
						"attrs": map[string]interface{}{
							"key": "value",
						},
					},
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}
