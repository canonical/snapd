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

package daemon_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/strutil"
)

// Tests for GET /v2/connections

func (s *interfacesSuite) testConnectionsConnected(c *check.C, d *daemon.Daemon, query string, connsState map[string]interface{}, repoConnected []string, expected map[string]interface{}) {
	repo := d.Overlord().InterfaceManager().Repository()
	for crefStr, cstate := range connsState {
		// if repoConnected is defined, then given connection must be on
		// list, otherwise it's not going to be connected in the repository
		// to simulate missing plugs/slots.
		if repoConnected != nil && !strutil.ListContains(repoConnected, crefStr) {
			continue
		}
		cref := mylog.Check2(interfaces.ParseConnRef(crefStr))
		c.Assert(err, check.IsNil)
		conn := cstate.(map[string]interface{})
		if undesiredRaw, ok := conn["undesired"]; ok {
			undesired, ok := undesiredRaw.(bool)
			c.Assert(ok, check.Equals, true, check.Commentf("unexpected value for key 'undesired': %v", cstate))
			if undesired {
				// do not add connections that are undesired
				continue
			}
		}
		staticPlugAttrs, _ := conn["plug-static"].(map[string]interface{})
		dynamicPlugAttrs, _ := conn["plug-dynamic"].(map[string]interface{})
		staticSlotAttrs, _ := conn["slot-static"].(map[string]interface{})
		dynamicSlotAttrs, _ := conn["slot-dynamic"].(map[string]interface{})
		_ = mylog.Check2(repo.Connect(cref, staticPlugAttrs, dynamicPlugAttrs, staticSlotAttrs, dynamicSlotAttrs, nil))
		c.Assert(err, check.IsNil)
	}

	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", connsState)
	st.Unlock()

	s.testConnections(c, query, expected)
}

func (s *interfacesSuite) testConnections(c *check.C, query string, expected map[string]interface{}) {
	req := mylog.Check2(http.NewRequest("GET", query, nil))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, expected)
}

func (s *interfacesSuite) TestConnectionsUnhappy(c *check.C) {
	s.daemon(c)
	req := mylog.Check2(http.NewRequest("GET", "/v2/connections?select=bad", nil))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
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

func (s *interfacesSuite) TestConnectionsEmpty(c *check.C) {
	s.daemon(c)
	s.testConnections(c, "/v2/connections", map[string]interface{}{
		"result": map[string]interface{}{
			"established": []interface{}{},
			"plugs":       []interface{}{},
			"slots":       []interface{}{},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
	s.testConnections(c, "/v2/connections?select=all", map[string]interface{}{
		"result": map[string]interface{}{
			"established": []interface{}{},
			"plugs":       []interface{}{},
			"slots":       []interface{}{},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestConnectionsNotFound(c *check.C) {
	s.daemon(c)
	req := mylog.Check2(http.NewRequest("GET", "/v2/connections?snap=not-found", nil))
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 404)
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": `no state entry for key "snaps"`,
			"kind":    "snap-not-found",
			"value":   "not-found",
		},
		"status":      "Not Found",
		"status-code": 404.0,
		"type":        "error",
	})
}

func (s *interfacesSuite) TestConnectionsUnconnected(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.testConnections(c, "/v2/connections?select=all", map[string]interface{}{
		"result": map[string]interface{}{
			"established": []interface{}{},
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

func (s *interfacesSuite) TestConnectionsBySnapName(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.testConnections(c, "/v2/connections?select=all&snap=producer", map[string]interface{}{
		"result": map[string]interface{}{
			"established": []interface{}{},
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
			"plugs": []interface{}{},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})

	s.testConnections(c, "/v2/connections?select=all&snap=consumer", map[string]interface{}{
		"result": map[string]interface{}{
			"established": []interface{}{},
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
			"slots": []interface{}{},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})

	s.testConnectionsConnected(c, d, "/v2/connections?snap=producer", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
		},
	}, nil, map[string]interface{}{
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
			"established": []interface{}{
				map[string]interface{}{
					"plug":      map[string]interface{}{"snap": "consumer", "plug": "plug"},
					"slot":      map[string]interface{}{"snap": "producer", "slot": "slot"},
					"manual":    true,
					"interface": "test",
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestConnectionsMissingPlugSlotFilteredOut(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	for _, missingPlugOrSlot := range []string{"consumer:plug2 producer:slot", "consumer:plug producer:slot2"} {
		s.testConnectionsConnected(c, d, "/v2/connections?snap=producer", map[string]interface{}{
			"consumer:plug producer:slot": map[string]interface{}{
				"interface": "test",
			},
			missingPlugOrSlot: map[string]interface{}{
				"interface": "test",
			},
		},
			[]string{"consumer:plug producer:slot"},
			map[string]interface{}{
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
					"established": []interface{}{
						map[string]interface{}{
							"plug":      map[string]interface{}{"snap": "consumer", "plug": "plug"},
							"slot":      map[string]interface{}{"snap": "producer", "slot": "slot"},
							"manual":    true,
							"interface": "test",
						},
					},
				},
				"status":      "OK",
				"status-code": 200.0,
				"type":        "sync",
			})
	}
}

func (s *interfacesSuite) TestConnectionsBySnapAlias(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, coreProducerYaml)

	expectedUnconnected := map[string]interface{}{
		"established": []interface{}{},
		"slots": []interface{}{
			map[string]interface{}{
				"snap":      "core",
				"slot":      "slot",
				"interface": "test",
				"attrs":     map[string]interface{}{"key": "value"},
				"label":     "label",
			},
		},
		"plugs": []interface{}{},
	}
	s.testConnections(c, "/v2/connections?select=all&snap=core", map[string]interface{}{
		"result":      expectedUnconnected,
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
	// try using a well know alias
	s.testConnections(c, "/v2/connections?select=all&snap=system", map[string]interface{}{
		"result":      expectedUnconnected,
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})

	expectedConnmected := map[string]interface{}{
		"plugs": []interface{}{
			map[string]interface{}{
				"snap":      "consumer",
				"plug":      "plug",
				"interface": "test",
				"attrs":     map[string]interface{}{"key": "value"},
				"apps":      []interface{}{"app"},
				"label":     "label",
				"connections": []interface{}{
					map[string]interface{}{"snap": "core", "slot": "slot"},
				},
			},
		},
		"slots": []interface{}{
			map[string]interface{}{
				"snap":      "core",
				"slot":      "slot",
				"interface": "test",
				"attrs":     map[string]interface{}{"key": "value"},
				"label":     "label",
				"connections": []interface{}{
					map[string]interface{}{"snap": "consumer", "plug": "plug"},
				},
			},
		},
		"established": []interface{}{
			map[string]interface{}{
				"plug":      map[string]interface{}{"snap": "consumer", "plug": "plug"},
				"slot":      map[string]interface{}{"snap": "core", "slot": "slot"},
				"manual":    true,
				"interface": "test",
			},
		},
	}

	s.testConnectionsConnected(c, d, "/v2/connections?snap=core", map[string]interface{}{
		"consumer:plug core:slot": map[string]interface{}{
			"interface": "test",
		},
	}, nil, map[string]interface{}{
		"result":      expectedConnmected,
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
	// connection was already established
	s.testConnections(c, "/v2/connections?snap=system", map[string]interface{}{
		"result":      expectedConnmected,
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestConnectionsByIfaceName(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()
	restore = builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "different"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	differentProducerYaml := `
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
	differentConsumerYaml := `
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
			"established": []interface{}{},
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
			"established": []interface{}{},
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

	// modifies state internally
	s.testConnectionsConnected(c, d, "/v2/connections?interfaces=test", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
		},
	}, nil, map[string]interface{}{
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
			"established": []interface{}{
				map[string]interface{}{
					"plug":      map[string]interface{}{"snap": "consumer", "plug": "plug"},
					"slot":      map[string]interface{}{"snap": "producer", "slot": "slot"},
					"manual":    true,
					"interface": "test",
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
	// use state modified by previous cal
	s.testConnections(c, "/v2/connections?interface=different", map[string]interface{}{
		"result": map[string]interface{}{
			"established": []interface{}{},
			"slots":       []interface{}{},
			"plugs":       []interface{}{},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestConnectionsDefaultManual(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.testConnectionsConnected(c, d, "/v2/connections", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
		},
	}, nil, map[string]interface{}{
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
			"established": []interface{}{
				map[string]interface{}{
					"plug":      map[string]interface{}{"snap": "consumer", "plug": "plug"},
					"slot":      map[string]interface{}{"snap": "producer", "slot": "slot"},
					"manual":    true,
					"interface": "test",
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestConnectionsDefaultAuto(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.testConnectionsConnected(c, d, "/v2/connections", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
			"auto":      true,
			"plug-static": map[string]interface{}{
				"key": "value",
			},
			"plug-dynamic": map[string]interface{}{
				"foo-plug-dynamic": "bar-dynamic",
			},
			"slot-static": map[string]interface{}{
				"key": "value",
			},
			"slot-dynamic": map[string]interface{}{
				"foo-slot-dynamic": "bar-dynamic",
			},
		},
	}, nil, map[string]interface{}{
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
			"established": []interface{}{
				map[string]interface{}{
					"plug":      map[string]interface{}{"snap": "consumer", "plug": "plug"},
					"slot":      map[string]interface{}{"snap": "producer", "slot": "slot"},
					"interface": "test",
					"plug-attrs": map[string]interface{}{
						"key":              "value",
						"foo-plug-dynamic": "bar-dynamic",
					},
					"slot-attrs": map[string]interface{}{
						"key":              "value",
						"foo-slot-dynamic": "bar-dynamic",
					},
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestConnectionsDefaultGadget(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.testConnectionsConnected(c, d, "/v2/connections", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
			"by-gadget": true,
			"auto":      true,
		},
	}, nil, map[string]interface{}{
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
			"established": []interface{}{
				map[string]interface{}{
					"plug":      map[string]interface{}{"snap": "consumer", "plug": "plug"},
					"slot":      map[string]interface{}{"snap": "producer", "slot": "slot"},
					"gadget":    true,
					"interface": "test",
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestConnectionsAll(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.testConnectionsConnected(c, d, "/v2/connections?select=all", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
			"by-gadget": true,
			"auto":      true,
			"undesired": true,
		},
	}, nil, map[string]interface{}{
		"result": map[string]interface{}{
			"established": []interface{}{},
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
			"undesired": []interface{}{
				map[string]interface{}{
					"plug":      map[string]interface{}{"snap": "consumer", "plug": "plug"},
					"slot":      map[string]interface{}{"snap": "producer", "slot": "slot"},
					"gadget":    true,
					"manual":    true,
					"interface": "test",
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestConnectionsOnlyUndesired(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.testConnectionsConnected(c, d, "/v2/connections", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
			"by-gadget": true,
			"auto":      true,
			"undesired": true,
		},
	}, nil, map[string]interface{}{
		"result": map[string]interface{}{
			"established": []interface{}{},
			"plugs":       []interface{}{},
			"slots":       []interface{}{},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestConnectionsHotplugGone(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	defer restore()

	d := s.daemon(c)

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.testConnectionsConnected(c, d, "/v2/connections", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface":    "test",
			"hotplug-gone": true,
		},
	}, nil, map[string]interface{}{
		"result": map[string]interface{}{
			"established": []interface{}{},
			"plugs":       []interface{}{},
			"slots":       []interface{}{},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *interfacesSuite) TestConnectionsSorted(c *check.C) {
	restore := builtin.MockInterface(&ifacetest.TestInterface{InterfaceName: "test"})
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
	anotherProducerYaml := `
name: another-producer
version: 1
apps:
 app:
slots:
 slot:
  interface: test
  key: value
  label: label
`

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, fmt.Sprintf(anotherConsumerYaml, "def"))
	s.mockSnap(c, fmt.Sprintf(anotherConsumerYaml, "abc"))

	s.mockSnap(c, producerYaml)
	s.mockSnap(c, anotherProducerYaml)

	s.testConnectionsConnected(c, d, "/v2/connections", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
			"by-gadget": true,
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
		"another-consumer-def:plug another-producer:slot": map[string]interface{}{
			"interface": "test",
			"by-gadget": true,
			"auto":      true,
		},
	}, nil, map[string]interface{}{
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
						map[string]interface{}{"snap": "another-producer", "slot": "slot"},
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
					"snap":      "another-producer",
					"slot":      "slot",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "another-consumer-def", "plug": "plug"},
					},
				},
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
			"established": []interface{}{
				map[string]interface{}{
					"plug":      map[string]interface{}{"snap": "another-consumer-abc", "plug": "plug"},
					"slot":      map[string]interface{}{"snap": "producer", "slot": "slot"},
					"interface": "test",
					"gadget":    true,
				},
				map[string]interface{}{
					"plug":      map[string]interface{}{"snap": "another-consumer-def", "plug": "plug"},
					"slot":      map[string]interface{}{"snap": "another-producer", "slot": "slot"},
					"interface": "test",
					"gadget":    true,
				},
				map[string]interface{}{
					"plug":      map[string]interface{}{"snap": "another-consumer-def", "plug": "plug"},
					"slot":      map[string]interface{}{"snap": "producer", "slot": "slot"},
					"interface": "test",
					"gadget":    true,
				},
				map[string]interface{}{
					"plug":      map[string]interface{}{"snap": "consumer", "plug": "plug"},
					"slot":      map[string]interface{}{"snap": "producer", "slot": "slot"},
					"interface": "test",
					"gadget":    true,
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}
