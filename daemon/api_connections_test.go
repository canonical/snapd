// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/ifacetest"
)

// Tests for GET /v2/connections

func (s *apiSuite) testConnections(c *check.C, query string, expected map[string]interface{}) {
	req, err := http.NewRequest("GET", query, nil)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	connectionsCmd.GET(connectionsCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, expected)
}

func (s *apiSuite) TestConnectionsUnhappy(c *check.C) {
	s.daemon(c)
	req, err := http.NewRequest("GET", "/v2/connections?select=bad", nil)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	connectionsCmd.GET(connectionsCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "unsupported select qualifier",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func (s *apiSuite) TestConnectionsEmpty(c *check.C) {
	s.daemon(c)
	s.testConnections(c, "/v2/connections", map[string]interface{}{
		"result":      map[string]interface{}{},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
	s.testConnections(c, "/v2/connections?select=all", map[string]interface{}{
		"result":      map[string]interface{}{},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *apiSuite) TestConnectionsUnconnected(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.testConnections(c, "/v2/connections?select=all", map[string]interface{}{
		"result": map[string]interface{}{
			"plugs": []interface{}{
				map[string]interface{}{
					"snap":      "consumer",
					"plug":      "plug",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
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
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *apiSuite) TestConnectionsBySnapName(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.testConnections(c, "/v2/connections?select=all&snap=producer", map[string]interface{}{
		"result": map[string]interface{}{
			"slots": []interface{}{
				map[string]interface{}{
					"snap":      "producer",
					"slot":      "slot",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})

	s.testConnections(c, "/v2/connections?select=all&snap=consumer", map[string]interface{}{
		"result": map[string]interface{}{
			"plugs": []interface{}{
				map[string]interface{}{
					"snap":      "consumer",
					"plug":      "plug",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})

	// connect the interface
	repo := d.overlord.InterfaceManager().Repository()
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	_, err := repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, check.IsNil)

	s.testConnections(c, "/v2/connections?snap=producer", map[string]interface{}{
		"result": map[string]interface{}{
			"plugs": []interface{}{
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

func (s *apiSuite) TestConnectionsByIfaceName(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()
	restore = builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "different"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	var differentProducerYaml = `
name: different-producer
version: 1
apps:
 app:
slots:
 slot:
  interface: different
  key: value
  label: label
`
	var differentConsumerYaml = `
name: different-consumer
version: 1
apps:
 app:
plugs:
 plug:
  interface: different
  key: value
  label: label
`
	s.mockSnap(c, differentProducerYaml)
	s.mockSnap(c, differentConsumerYaml)

	s.testConnections(c, "/v2/connections?select=all&interface=test", map[string]interface{}{
		"result": map[string]interface{}{
			"plugs": []interface{}{
				map[string]interface{}{
					"snap":      "consumer",
					"plug":      "plug",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
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
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
	s.testConnections(c, "/v2/connections?select=all&interface=different", map[string]interface{}{
		"result": map[string]interface{}{
			"plugs": []interface{}{
				map[string]interface{}{
					"snap":      "different-consumer",
					"plug":      "plug",
					"interface": "different",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
				},
			},
			"slots": []interface{}{
				map[string]interface{}{
					"snap":      "different-producer",
					"slot":      "slot",
					"interface": "different",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})

	// connect the interface
	repo := d.overlord.InterfaceManager().Repository()
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	_, err := repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, check.IsNil)

	s.testConnections(c, "/v2/connections?interface=test", map[string]interface{}{
		"result": map[string]interface{}{
			"plugs": []interface{}{
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
						map[string]interface{}{"snap": "consumer", "plug": "plug"},
					},
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
	s.testConnections(c, "/v2/connections?interface=different", map[string]interface{}{
		"result":      map[string]interface{}{},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *apiSuite) TestConnectionsDefault(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	repo := d.overlord.InterfaceManager().Repository()
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	_, err := repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, check.IsNil)

	s.testConnections(c, "/v2/connections", map[string]interface{}{
		"result": map[string]interface{}{
			"plugs": []interface{}{
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
