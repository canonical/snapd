// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"log"
)

// Config contains the values to pass to the test bed from the host.
type Config struct {
	FileName      string
	Release       string
	Channel       string
	TargetRelease string
	TargetChannel string
	TestbedIP     string
	Update        bool
	Rollback      bool
}

// NewConfig is the Config constructor
func NewConfig(fileName, release, channel, targetRelease, targetChannel, testbedIP string, update, rollback bool) *Config {
	// if we are connecting to a remote testbed we can't specify
	// origin release and channel
	if testbedIP != "" {
		release, channel = ".*", ".*"
	}
	return &Config{
		FileName: fileName, Release: release, Channel: channel,
		TargetRelease: targetRelease, TargetChannel: targetChannel,
		TestbedIP: testbedIP, Update: update, Rollback: rollback,
	}
}

// Write writes the config to a file that will be copied to the test bed.
func (cfg Config) Write() {
	fmt.Println("Writing test config...")
	fmt.Println(cfg)
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
