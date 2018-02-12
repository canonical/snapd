// -*- Mode: Go; indent-tabs-mode: t -*-

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

package interfaces

import (
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

// systemKey describes the environment for which security profiles
// have been generated. It is useful to compare if the current
// running system is similar enough to the generated profiles or
// if the profiles need to be re-generated to match the new system.
type systemKey struct {
	BuildID          string   `yaml:"build-id"`
	AppArmorFeatures []string `yaml:"apparmor-features"`
}

var mockedSystemKey *systemKey

func generateSystemKey() *systemKey {
	// for testing only
	if mockedSystemKey != nil {
		return mockedSystemKey
	}

	var sk systemKey
	buildID, err := osutil.MyBuildID()
	if err != nil {
		buildID = ""
	}
	sk.BuildID = buildID

	// Add apparmor-feature, note that ioutil.ReadDir() is already sorted.
	//
	// We prefix the dirs.GlobalRootDir (which is usually "/") to make
	// this testable.
	sk.AppArmorFeatures = release.AppArmorFeatures()

	return &sk
}

// SystemKey outputs a string that identifies what security profiles
// environment this snapd is using. Security profiles that were generated
// with a different Systemkey should be re-generated.
func SystemKey() string {
	sk := generateSystemKey()

	// special case: unknown build-ids always trigger a rebuild
	if sk.BuildID == "" {
		return ""
	}
	sks, err := yaml.Marshal(sk)
	if err != nil {
		panic(err)
	}
	return string(sks)
}

func MockSystemKey(s string) func() {
	var sk systemKey
	err := yaml.Unmarshal([]byte(s), &sk)
	if err != nil {
		panic(err)
	}
	mockedSystemKey = &sk
	return func() { mockedSystemKey = nil }
}
