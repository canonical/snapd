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

package interfaces

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/osutil"
)

// systemKey describes the environment for which security profiles
// have been generated. It is useful to compare if the current
// running system is similar enough to the generated profiles or
// if the profiles need to be re-generated to match the new system.
type systemKey struct {
	BuildID          string   `yaml:"build-id"`
	ApparmorFeatures []string `yaml:"apparmor-features"`
}

var mySystemKey systemKey

func init() {
	// add build-id
	buildID, err := osutil.MyBuildID()
	if err != nil {
		buildID = "unknown"
	}
	mySystemKey.BuildID = buildID

	// add apparmor-feature, note that ioutil.ReadDir() is already sorted
	if dentries, err := ioutil.ReadDir("/sys/kernel/security/apparmor/features"); err == nil {
		mySystemKey.ApparmorFeatures = make([]string, len(dentries))
		for i, f := range dentries {
			mySystemKey.ApparmorFeatures[i] = f.Name()
		}
	}
}

// SystemKey outputs a string that identifies what security profiles
// environment this snapd is using. Security profiles that were generated
// with a different Systemkey should be re-generated.
func SystemKey() string {
	sk, err := yaml.Marshal(mySystemKey)
	if err != nil {
		panic(err)
	}
	return string(sk)
}
