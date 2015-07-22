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
	"strconv"
)

// Config contains the values to pass to the test bed from the host.
type Config struct {
	fileName      string
	release       string
	channel       string
	targetRelease string
	targetChannel string
	update        bool
	rollback      bool
}

// NewConfig is the Config constructor
func NewConfig(fileName, release, channel, targetRelease, targetChannel string, update, rollback bool) *Config {
	return &Config{
		fileName: fileName, release: release, channel: channel,
		targetRelease: targetRelease, targetChannel: targetChannel,
		update: update, rollback: rollback,
	}
}

// Write writes the config to a file that will be copied to the test bed.
func (cfg Config) Write() {
	fmt.Println("Writing test config...")
	testConfig := map[string]string{
		"release": cfg.release,
		"channel": cfg.channel,
	}
	if cfg.targetRelease != "" {
		testConfig["targetRelease"] = cfg.targetRelease
	}
	if cfg.targetChannel != "" {
		testConfig["targetChannel"] = cfg.targetChannel
	}
	testConfig["update"] = strconv.FormatBool(cfg.update)
	testConfig["rollback"] = strconv.FormatBool(cfg.rollback)
	fmt.Println(testConfig)
	encoded, err := json.Marshal(testConfig)
	if err != nil {
		log.Fatalf("Error encoding the test config: %v", err)
	}
	err = ioutil.WriteFile(cfg.fileName, encoded, 0644)
	if err != nil {
		log.Fatalf("Error writing the test config: %v", err)
	}
}
