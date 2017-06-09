// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

type systemKey struct {
	BuildID          string   `yaml:"build-id"`
	ApparmorFeatures []string `yaml:"apparmor-features"`
}

var mySystemKey systemKey

func init() {
	buildID, err := osutil.MyBuildID()
	if err != nil {
		logger.Noticef("cannot get builID: %s", err)
	}
	mySystemKey.BuildID = buildID

	// ioutil.ReadDir() is already sorted
	if dentries, err := ioutil.ReadDir("/sys/kernel/security/apparmor/features"); err == nil {
		mySystemKey.ApparmorFeatures = make([]string, len(dentries))
		for i, f := range dentries {
			mySystemKey.ApparmorFeatures[i] = f.Name()
		}
	}

	// FIXME: add $( ls
	// FIXME2: make this yaml
	// FIXME3: do not make it a hash
}

// SystemKey outputs a digest that uniquely identifies what security
// profiles this snapd understands. Everytime there is an incompatible
// change in any of snapds format this digest will change. Later more
// inputs (like what kernel version etc) may be added.
func SystemKey() string {
	sk, err := yaml.Marshal(mySystemKey)
	if err != nil {
		panic(err)
	}
	return string(sk)
}
