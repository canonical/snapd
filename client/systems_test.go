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

package client_test

import (
	"encoding/json"
	"io"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/snap"
)

func (cs *clientSuite) TestListSystemsSome(c *check.C) {
	cs.rsp = `{
	    "type": "sync",
	    "status-code": 200,
	    "result": {
	        "systems": [
	           {
	                "current": true,
	                "label": "20200101",
	                "model": {
	                    "model": "this-is-model-id",
	                    "brand-id": "brand-id-1",
	                    "display-name": "wonky model"
	                },
	                "brand": {
	                    "id": "brand-id-1",
	                    "username": "brand",
	                    "display-name": "wonky publishing"
	                },
	                "actions": [
	                    {"title": "recover", "mode": "recover"},
	                    {"title": "reinstall", "mode": "install"}
	                ]
	           }, {
	                "label": "20200311",
	                "model": {
	                    "model": "different-model-id",
	                    "brand-id": "bulky-brand-id-1",
	                    "display-name": "bulky model"
	                },
	                "brand": {
	                    "id": "bulky-brand-id-1",
	                    "username": "bulky-brand",
	                    "display-name": "bulky publishing"
	                },
	                "actions": [
	                    {"title": "factory-reset", "mode": "install"}
	                ]
	            }
	        ]
	    }
	}`
	systems := mylog.Check2(cs.cli.ListSystems())
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems")
	c.Check(systems, check.DeepEquals, []client.System{
		{
			Current: true,
			Label:   "20200101",
			Model: client.SystemModelData{
				Model:       "this-is-model-id",
				BrandID:     "brand-id-1",
				DisplayName: "wonky model",
			},
			Brand: snap.StoreAccount{
				ID:          "brand-id-1",
				Username:    "brand",
				DisplayName: "wonky publishing",
			},
			Actions: []client.SystemAction{
				{Title: "recover", Mode: "recover"},
				{Title: "reinstall", Mode: "install"},
			},
		}, {
			Label: "20200311",
			Model: client.SystemModelData{
				Model:       "different-model-id",
				BrandID:     "bulky-brand-id-1",
				DisplayName: "bulky model",
			},
			Brand: snap.StoreAccount{
				ID:          "bulky-brand-id-1",
				Username:    "bulky-brand",
				DisplayName: "bulky publishing",
			},
			Actions: []client.SystemAction{
				{Title: "factory-reset", Mode: "install"},
			},
		},
	})
}

func (cs *clientSuite) TestListSystemsNone(c *check.C) {
	cs.rsp = `{
	    "type": "sync",
	    "status-code": 200,
	    "result": {}
	}`
	systems := mylog.Check2(cs.cli.ListSystems())
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems")
	c.Check(systems, check.HasLen, 0)
}

func (cs *clientSuite) TestRequestSystemActionHappy(c *check.C) {
	cs.rsp = `{
	    "type": "sync",
	    "status-code": 200,
	    "result": {}
	}`
	mylog.Check(cs.cli.DoSystemAction("1234", &client.SystemAction{
		Title: "reinstall",
		Mode:  "install",
	}))
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/1234")

	body := mylog.Check2(io.ReadAll(cs.req.Body))
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	mylog.Check(json.Unmarshal(body, &req))
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action": "do",
		"title":  "reinstall",
		"mode":   "install",
	})
}

func (cs *clientSuite) TestRequestSystemActionError(c *check.C) {
	cs.rsp = `{
	    "type": "error",
	    "status-code": 500,
	    "result": {"message": "failed"}
	}`
	mylog.Check(cs.cli.DoSystemAction("1234", &client.SystemAction{Mode: "install"}))
	c.Assert(err, check.ErrorMatches, "cannot request system action: failed")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/1234")
}

func (cs *clientSuite) TestRequestSystemActionInvalid(c *check.C) {
	mylog.Check(cs.cli.DoSystemAction("", &client.SystemAction{}))
	c.Assert(err, check.ErrorMatches, "cannot request an action without the system")
	mylog.Check(cs.cli.DoSystemAction("1234", nil))
	c.Assert(err, check.ErrorMatches, "cannot request an action without one")
}

func (cs *clientSuite) TestRequestSystemRebootHappy(c *check.C) {
	cs.rsp = `{
	    "type": "sync",
	    "status-code": 200,
	    "result": {}
	}`
	mylog.Check(cs.cli.RebootToSystem("20201212", "install"))
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/20201212")

	body := mylog.Check2(io.ReadAll(cs.req.Body))
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	mylog.Check(json.Unmarshal(body, &req))
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action": "reboot",
		"mode":   "install",
	})
}

func (cs *clientSuite) TestRequestSystemRebootErrorNoSystem(c *check.C) {
	cs.rsp = `{
	    "type": "error",
	    "status-code": 500,
	    "result": {"message": "failed"}
	}`
	mylog.Check(cs.cli.RebootToSystem("", "install"))
	c.Assert(err, check.ErrorMatches, `cannot request system reboot: failed`)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems")
}

func (cs *clientSuite) TestRequestSystemRebootErrorWithSystem(c *check.C) {
	cs.rsp = `{
	    "type": "error",
	    "status-code": 500,
	    "result": {"message": "failed"}
	}`
	mylog.Check(cs.cli.RebootToSystem("1234", "install"))
	c.Assert(err, check.ErrorMatches, `cannot request system reboot into "1234": failed`)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/1234")
}

func (cs *clientSuite) TestSystemDetailsNone(c *check.C) {
	cs.rsp = `{
	    "type": "sync",
	    "status-code": 404,
	    "result": {
	       "kind": "assertion-not-found",
	       "value": "model"
            }
	}`
	_ := mylog.Check2(cs.cli.SystemDetails("20190102"))
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/20190102")
}

func (cs *clientSuite) TestSystemDetailsHappy(c *check.C) {
	cs.rsp = `{
	    "type": "sync",
	    "status-code": 200,
	    "result": {
                "current": true,
                "label": "20200101",
                "model": {
                    "model": "this-is-model-id",
                    "brand-id": "brand-id-1",
                    "display-name": "wonky model"
                },
                "brand": {
                    "id": "brand-id-1",
                    "username":"brand-username-1",
                    "display-name":"Brandy Display Name",
                    "validation":"validated"
                },
                "actions": [
                    {"title": "recover", "mode": "recover"},
                    {"title": "reinstall", "mode": "install"}
                ],
                "storage-encryption": {
                    "support":"available",
                    "storage-safety":"prefer-encrypted",
                    "encryption-type":"cryptsetup"
                },
                "volumes": {
                    "pc": {
                        "schema":"gpt",
                        "bootloader":"grub",
                        "structure":[{"name":"mbr","type":"mbr","size":440}]
                    }
                }
            }
	}`
	sys := mylog.Check2(cs.cli.SystemDetails("20190102"))
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/20190102")
	vols := map[string]*gadget.Volume{
		"pc": {
			Schema:     "gpt",
			Bootloader: "grub",
			Structure: []gadget.VolumeStructure{
				{Name: "mbr", Type: "mbr", Size: 440},
			},
		},
	}
	gadget.SetEnclosingVolumeInStructs(vols)
	c.Check(sys, check.DeepEquals, &client.SystemDetails{
		Current: true,
		Label:   "20200101",
		Model: map[string]interface{}{
			"model":        "this-is-model-id",
			"brand-id":     "brand-id-1",
			"display-name": "wonky model",
		},
		Brand: snap.StoreAccount{
			ID:          "brand-id-1",
			Username:    "brand-username-1",
			DisplayName: "Brandy Display Name",
			Validation:  "validated",
		},
		Actions: []client.SystemAction{
			{Title: "recover", Mode: "recover"},
			{Title: "reinstall", Mode: "install"},
		},
		StorageEncryption: &client.StorageEncryption{
			Support:       "available",
			StorageSafety: "prefer-encrypted",
			Type:          "cryptsetup",
		},
		Volumes: vols,
	})
}

func (cs *clientSuite) TestRequestSystemInstallErrorNoSystem(c *check.C) {
	cs.rsp = `{
	    "type": "error",
	    "status-code": 500,
	    "result": {"message": "failed"}
	}`
	opts := &client.InstallSystemOptions{
		Step: client.InstallStepFinish,
	}
	_ := mylog.Check2(cs.cli.InstallSystem("1234", opts))
	c.Assert(err, check.ErrorMatches, `cannot request system install for "1234": failed`)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/1234")
}

func (cs *clientSuite) TestRequestSystemInstallEmptySystemLabel(c *check.C) {
	cs.rsp = `{
	    "type": "error",
	    "status-code": 500,
	    "result": {"message": "failed"}
	}`
	_ := mylog.Check2(cs.cli.InstallSystem("", nil))
	c.Assert(err, check.ErrorMatches, `cannot install with an empty system label`)
	// no request was performed
	c.Check(cs.req, check.IsNil)
}

func (cs *clientSuite) TestRequestSystemInstallHappy(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
		"status-code": 202,
		"change": "42"
	}`
	vols := map[string]*gadget.Volume{
		"pc": {
			Schema:     "dos",
			Bootloader: "mbr",
			ID:         "id",
			// Note that name is not exported as json
			Name: "pc",
			Structure: []gadget.VolumeStructure{
				{
					Device: "/dev/sda1",

					Label:      "label",
					Name:       "vol-name",
					ID:         "id",
					MinSize:    1234,
					Size:       1234,
					Type:       "type",
					Filesystem: "fs",
					Role:       "system-boot",
					// not exported to json
					VolumeName: "vol-name",
				},
			},
		},
	}
	opts := &client.InstallSystemOptions{
		Step:      client.InstallStepFinish,
		OnVolumes: vols,
	}
	chgID := mylog.Check2(cs.cli.InstallSystem("1234", opts))
	c.Assert(err, check.IsNil)
	c.Assert(chgID, check.Equals, "42")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/1234")

	body := mylog.Check2(io.ReadAll(cs.req.Body))
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	mylog.Check(json.Unmarshal(body, &req))
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action": "install",
		"step":   "finish",
		"on-volumes": map[string]interface{}{
			"pc": map[string]interface{}{
				"schema":     "dos",
				"bootloader": "mbr",
				"id":         "id",
				"structure": []interface{}{
					map[string]interface{}{
						"device":           "/dev/sda1",
						"filesystem-label": "label",
						"name":             "vol-name",
						"id":               "id",
						"min-size":         float64(1234),
						"size":             float64(1234),
						"type":             "type",
						"filesystem":       "fs",
						"role":             "system-boot",
						"offset":           nil,
						"offset-write":     nil,
						"content":          nil,
						"update": map[string]interface{}{
							"edition":  float64(0),
							"preserve": nil,
						},
					},
				},
			},
		},
	})
}
