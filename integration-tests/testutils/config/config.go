// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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

package config

import (
	"encoding/json"
	"io/ioutil"
	"log"
)

// DefaultFileName is the path where we write the test config to be available from the testbed
const DefaultFileName = "integration-tests/data/output/testconfig.json"

// Config contains the values to pass to the test bed from the host.
type Config struct {
	FileName      string
	Release       string
	Channel       string
	RemoteTestbed bool
	Update        bool
	Rollback      bool
	FromBranch    bool
}

// Write writes the config to a file that will be copied to the test bed.
func (cfg Config) Write() {
	encoded, err := json.Marshal(cfg)
	if err != nil {
		log.Panicf("Error encoding the test config: %v", err)
	}
	err = ioutil.WriteFile(cfg.FileName, encoded, 0644)
	if err != nil {
		log.Panicf("Error writing the test config: %v", err)
	}
}

// ReadConfig the config from a file
func ReadConfig(fileName string) (*Config, error) {
	b, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	var decoded Config
	if err = json.Unmarshal(b, &decoded); err != nil {
		return nil, err
	}
	return &decoded, nil
}
