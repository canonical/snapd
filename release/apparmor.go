// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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

package release

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ApparmorLevelType encodes the kind of support for apparmor
// found on this system.
type AppArmorLevelType int

const (
	// NoAppArmor indicates that apparmor is not enabled.
	NoAppArmor AppArmorLevelType = iota
	// PartialAppArmor indicates that apparmor is enabled but some
	// features are missing.
	PartialAppArmor
	// FullAppArmor indicates that all features are supported.
	FullAppArmor
)

var (
	appArmorLevel          AppArmorLevelType
	appArmorSummary        string
	appArmorParserFeatures []string
)

func init() {
	appArmorLevel, appArmorSummary = probeAppArmor()
}

// AppArmorLevel quantifies how well apparmor is supported on the
// current kernel.
func AppArmorLevel() AppArmorLevelType {
	return appArmorLevel
}

// AppArmorSummary describes how well apparmor is supported on the
// current kernel.
func AppArmorSummary() string {
	return appArmorSummary
}

// MockAppArmorSupportLevel makes the system believe it has certain
// level of apparmor support.
func MockAppArmorLevel(level AppArmorLevelType) (restore func()) {
	oldAppArmorLevel := appArmorLevel
	oldAppArmorSummary := appArmorSummary
	appArmorLevel = level
	appArmorSummary = fmt.Sprintf("mocked apparmor level: %v", level)
	return func() {
		appArmorLevel = oldAppArmorLevel
		appArmorSummary = oldAppArmorSummary
	}
}

// probe related code
var (
	appArmorFeaturesSysPath  = "/sys/kernel/security/apparmor/features"
	requiredAppArmorFeatures = []string{
		"caps",
		"dbus",
		"domain",
		"file",
		"mount",
		"namespaces",
		"network",
		"ptrace",
		"signal",
	}
)

// isDirectoy is like osutil.IsDirectory but we cannot import this
// because of import cycles
func isDirectory(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.IsDir()
}

func probeAppArmor() (AppArmorLevelType, string) {
	if !isDirectory(appArmorFeaturesSysPath) {
		return NoAppArmor, "apparmor not enabled"
	}
	var missing []string
	for _, feature := range requiredAppArmorFeatures {
		if !isDirectory(filepath.Join(appArmorFeaturesSysPath, feature)) {
			missing = append(missing, feature)
		}
	}
	if len(missing) > 0 {
		return PartialAppArmor, fmt.Sprintf("apparmor is enabled but some features are missing: %s", strings.Join(missing, ", "))
	}
	return FullAppArmor, "apparmor is enabled and all features are available"
}

// AppArmorFeatures returns a sorted list of apparmor features like
// []string{"dbus", "network"}.
func AppArmorFeatures() []string {
	// note that ioutil.ReadDir() is already sorted
	dentries, err := ioutil.ReadDir(appArmorFeaturesSysPath)
	if err != nil {
		return nil
	}
	appArmorFeatures := make([]string, 0, len(dentries))
	for _, f := range dentries {
		if isDirectory(filepath.Join(appArmorFeaturesSysPath, f.Name())) {
			appArmorFeatures = append(appArmorFeatures, f.Name())
		}
	}
	return appArmorFeatures
}

// parser probe related code
type apparmorParserFeature struct {
	feature string
	rule    string
}

var (
	requestedParserFeatures = []apparmorParserFeature{
		{"unsafe", "change_profile unsafe /**,"},
	}
	// Since AppArmorParserMtime() will be called by generateKey() in
	// system-key and that could be called by different users on the
	// system, use a predictable search path for finding the parser.
	parserSearchPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin"
)

// lookupParser will look for apparmor_parser in a predictable search path
func lookupParser() (string, error) {
	for _, dir := range filepath.SplitList(parserSearchPath) {
		path := filepath.Join(dir, "apparmor_parser")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("apparmor_parser not found in '%s'", parserSearchPath)
}

// tryParser will run the parser on the rule to determine if the feature is
// supported.
func tryParser(parser, rule string) bool {
	cmd := exec.Command(parser, "--preprocess")
	cmd.Stdin = bytes.NewBufferString(fmt.Sprintf("profile snap-test {\n %s\n}", rule))
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// AppArmorParserMtime returns the mtime of the parser, else 0
func AppArmorParserMtime() int64 {
	var mtime int64
	mtime = 0

	if path, err := lookupParser(); err == nil {
		if s, err := os.Stat(path); err == nil {
			mtime = s.ModTime().Unix()
		}
	}
	return mtime
}

// AppArmorParserFeatures returns a sorted list of apparmor parser features
// like []string{"unsafe", ...}.
func AppArmorParserFeatures() []string {
	parser, err := lookupParser()
	if err != nil {
		return nil
	}

	parserFeatures := make([]string, 0, len(requestedParserFeatures))
	for _, f := range requestedParserFeatures {
		if tryParser(parser, f.rule) {
			parserFeatures = append(parserFeatures, f.feature)
		}
	}
	sort.Strings(parserFeatures)
	return parserFeatures
}
