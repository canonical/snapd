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

package configcore

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
)

// CloudInfo holds the content for the optional "cloud" entry of core config
// that reflects cloud information for the system if available.
type CloudInfo struct {
	Name             string `json:"name"`
	Region           string `json:"region,omitempty"`
	AvailabilityZone string `json:"availability-zone,omitempty"`
}

func alreadySeeded(tr Conf) (bool, error) {
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

func handleCloud(tr Conf) error {
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
	var instanceData struct {
		V1 struct {
			Name             string `json:"cloud-name"`
			Region           string `json:"region"`
			AvailabilityZone string `json:"availability-zone"`
		} `json:"v1"`
	}
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

	tr.Set("core", "cloud", CloudInfo{
		Name:             cloudName,
		Region:           instanceData.V1.Region,
		AvailabilityZone: instanceData.V1.AvailabilityZone,
	})
	return nil
}
