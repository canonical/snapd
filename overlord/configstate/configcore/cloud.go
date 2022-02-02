// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

/*
 * Copyright (C) 2018 Canonical Ltd
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

package configcore

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

func alreadySeeded(tr config.Conf) (bool, error) {
	st := tr.State()
	st.Lock()
	defer st.Unlock()
	var seeded bool
	err := tr.State().Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return false, err
	}
	return seeded, nil
}

type cloudInitInstanceData struct {
	V1 struct {
		Region           string
		Name             string
		AvailabilityZone string
	}
}

func (c *cloudInitInstanceData) UnmarshalJSON(bs []byte) error {
	var instanceDataJSON struct {
		V1 struct {
			Region string `json:"region"`
			// these fields can come with - or _ as separators
			Name                string `json:"cloud_name"`
			AltName             string `json:"cloud-name"`
			AvailabilityZone    string `json:"availability_zone"`
			AltAvailabilityZone string `json:"availability-zone"`
		} `json:"v1"`
	}

	if err := json.Unmarshal(bs, &instanceDataJSON); err != nil {
		return err
	}

	c.V1.Region = instanceDataJSON.V1.Region
	switch {
	case instanceDataJSON.V1.Name != "":
		c.V1.Name = instanceDataJSON.V1.Name
		c.V1.AvailabilityZone = instanceDataJSON.V1.AvailabilityZone
	case instanceDataJSON.V1.AltName != "":
		c.V1.Name = instanceDataJSON.V1.AltName
		c.V1.AvailabilityZone = instanceDataJSON.V1.AltAvailabilityZone
	}
	return nil
}

func setCloudInfoWhenSeeding(tr config.Conf, opts *fsOnlyContext) error {
	// if we are during seeding try to capture cloud information
	seeded, err := alreadySeeded(tr)
	if err != nil {
		return err
	}
	if seeded {
		// already done
		return nil
	}

	data, err := ioutil.ReadFile(dirs.CloudInstanceDataFile)
	if os.IsNotExist(err) {
		// nothing to do
		return nil
	}
	if err != nil {
		logger.Noticef("cannot read cloud instance information %q: %v", dirs.CloudInstanceDataFile, err)
		return nil
	}

	var instanceData cloudInitInstanceData
	err = json.Unmarshal(data, &instanceData)
	if err != nil {
		logger.Noticef("cannot unmarshal cloud instance information %q: %v", dirs.CloudInstanceDataFile, err)
		return nil
	}

	cloudName := instanceData.V1.Name
	if cloudName == "" || cloudName == "nocloud" || cloudName == "none" {
		// not a cloud
		return nil
	}

	tr.Set("core", "cloud", auth.CloudInfo{
		Name:             cloudName,
		Region:           instanceData.V1.Region,
		AvailabilityZone: instanceData.V1.AvailabilityZone,
	})
	return nil
}
