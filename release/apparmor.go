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

	"github.com/snapcore/snapd/strutil"
)

// AppArmorLevelType encodes the kind of support for apparmor
// found on this system.
type AppArmorLevelType int

const (
	// UnknownAppArmor indicates that apparmor was not probed yet.
	UnknownAppArmor AppArmorLevelType = iota
	// NoAppArmor indicates that apparmor is not enabled.
	NoAppArmor
	// UnusableAppArmor indicates that apparmor is enabled but cannot be used.
	UnusableAppArmor
	// PartialAppArmor indicates that apparmor is enabled but some
	// features are missing.
	PartialAppArmor
	// FullAppArmor indicates that all features are supported.
	FullAppArmor
)

func (level AppArmorLevelType) String() string {
	switch level {
	case UnknownAppArmor:
		return "unknown"
	case NoAppArmor:
		return "none"
	case UnusableAppArmor:
		return "unusable"
	case PartialAppArmor:
		return "partial"
	case FullAppArmor:
		return "full"
	}
	return fmt.Sprintf("AppArmorLevelType:%d", level)
}

var (
	// appArmorLevel contains the assessment of the "level" of apparmor support.
	appArmorLevel = UnknownAppArmor
	// appArmorSummary contains a human readable description of the assessment.
	appArmorSummary string
	// appArmorKernelFeatures contains a list of kernel features that are supported.
	// If the value is nil then the features were not probed yet.
	appArmorKernelFeatures []string
	// appArmorParserFeatures contains a list of parser features that are supported.
	// If the value is nil then the features were not probed yet.
	appArmorParserFeatures []string
)

// AppArmorLevel quantifies how well apparmor is supported on the current
// kernel. The computation is costly to perform. The result is cached internally.
func AppArmorLevel() AppArmorLevelType {
	if appArmorLevel == UnknownAppArmor {
		assessAppArmor()
	}
	return appArmorLevel
}

// AppArmorSummary describes how well apparmor is supported on the current
// kernel. The computation is costly to perform. The result is cached
// internally.
func AppArmorSummary() string {
	if appArmorLevel == UnknownAppArmor {
		assessAppArmor()
	}
	return appArmorSummary
}

// AppArmorKernelFeatures returns a sorted list of apparmor features like
// []string{"dbus", "network"}. The result is cached internally.
func AppArmorKernelFeatures() []string {
	if appArmorKernelFeatures == nil {
		appArmorKernelFeatures = probeAppArmorKernelFeatures()
	}
	return appArmorKernelFeatures
}

// AppArmorParserFeatures returns a sorted list of apparmor parser features
// like []string{"unsafe", ...}. The computation is costly to perform. The
// result is cached internally.
func AppArmorParserFeatures() []string {
	if appArmorParserFeatures == nil {
		appArmorParserFeatures = probeAppArmorParserFeatures()
	}
	return appArmorParserFeatures
}

// AppArmorFeatures is a deprecated name for AppArmorKernelFeatures.
func AppArmorFeatures() []string {
	return AppArmorKernelFeatures()
}

// AppArmorParserMtime returns the mtime of the parser, else 0.
func AppArmorParserMtime() int64 {
	var mtime int64
	mtime = 0

	if path := findAppArmorParser(); path != "" {
		if s, err := os.Stat(path); err == nil {
			mtime = s.ModTime().Unix()
		}
	}
	return mtime
}

// MockAppArmorLevel makes the system believe it has certain level of apparmor
// support.
//
// AppArmor kernel and parser features are set to unrealistic values that do
// not match the requested level. Use this function to observe behavior that
// relies solely on the apparmor level value.
func MockAppArmorLevel(level AppArmorLevelType) (restore func()) {
	oldAppArmorLevel := appArmorLevel
	oldAppArmorSummary := appArmorSummary
	oldAppArmorKernelFeatures := appArmorKernelFeatures
	oldAppArmorParserFeatures := appArmorParserFeatures
	appArmorLevel = level
	appArmorSummary = fmt.Sprintf("mocked apparmor level: %s", level)
	appArmorKernelFeatures = []string{"mocked-kernel-feature"}
	appArmorParserFeatures = []string{"mocked-parser-feature"}
	return func() {
		appArmorLevel = oldAppArmorLevel
		appArmorSummary = oldAppArmorSummary
		appArmorKernelFeatures = oldAppArmorKernelFeatures
		appArmorParserFeatures = oldAppArmorParserFeatures
	}
}

// MockAppArmorFeatures makes the system believe it has certain kernel and
// parser features.
//
// AppArmor level and summary are automatically re-assessed on both the change
// and the restore process. Use this function to observe real assessment of
// arbitrary features.
func MockAppArmorFeatures(kernelFeatures, parserFeatures []string) (restore func()) {
	oldAppArmorKernelFeatures := appArmorKernelFeatures
	oldAppArmorParserFeatures := appArmorParserFeatures
	appArmorKernelFeatures = kernelFeatures
	appArmorParserFeatures = parserFeatures
	if appArmorKernelFeatures != nil && appArmorParserFeatures != nil {
		assessAppArmor()
	}
	return func() {
		appArmorKernelFeatures = oldAppArmorKernelFeatures
		appArmorParserFeatures = oldAppArmorParserFeatures
		if appArmorKernelFeatures != nil && appArmorParserFeatures != nil {
			assessAppArmor()
		}
	}
}

// probe related code

var (
	// requiredAppArmorParserFeatures denotes the features that must be present in the parser.
	// Absence of any of those features results in the effective level be at most UnusableAppArmor.
	requiredAppArmorParserFeatures = []string{
		"unsafe",
	}
	// preferredAppArmorParserFeatures denotes the features that should be present in the parser.
	// Absence of any of those features results in the effective level be at most PartialAppArmor.
	preferredAppArmorParserFeatures = []string{
		"unsafe",
	}
	// requiredAppArmorKernelFeatures denotes the features that must be present in the kernel.
	// Absence of any of those features results in the effective level be at most UnusableAppArmor.
	requiredAppArmorKernelFeatures = []string{
		// For now, require at least file and simply prefer the rest.
		"file",
	}
	// preferredAppArmorKernelFeatures denotes the features that should be present in the kernel.
	// Absence of any of those features results in the effective level be at most PartialAppArmor.
	preferredAppArmorKernelFeatures = []string{
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
	// Since AppArmorParserMtime() will be called by generateKey() in
	// system-key and that could be called by different users on the
	// system, use a predictable search path for finding the parser.
	appArmorParserSearchPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	// Each apparmor feature is manifested as a directory entry.
	appArmorFeaturesSysPath = "/sys/kernel/security/apparmor/features"
)

func assessAppArmor() {
	// First, quickly check if apparmor is available in the kernel at all.
	kernelFeatures := AppArmorKernelFeatures()
	if len(kernelFeatures) == 0 {
		appArmorLevel = NoAppArmor
		appArmorSummary = "apparmor not enabled"
		return
	}

	// Then check that the parser supports the required parser features.
	// If we have any missing required features then apparmor is unusable.
	parserFeatures := AppArmorParserFeatures()
	var missingParserFeatures []string
	for _, feature := range requiredAppArmorParserFeatures {
		if !strutil.SortedListContains(parserFeatures, feature) {
			missingParserFeatures = append(missingParserFeatures, feature)
		}
	}
	if len(missingParserFeatures) > 0 {
		appArmorLevel = UnusableAppArmor
		appArmorSummary = fmt.Sprintf("apparmor_parser is available but required parser features are missing: %s",
			strings.Join(missingParserFeatures, ", "))
		return
	}

	// Next, check that the kernel supports the required kernel features.
	var missingKernelFeatures []string
	for _, feature := range requiredAppArmorKernelFeatures {
		if !strutil.SortedListContains(kernelFeatures, feature) {
			missingKernelFeatures = append(missingKernelFeatures, feature)
		}
	}
	if len(missingKernelFeatures) > 0 {
		appArmorLevel = UnusableAppArmor
		appArmorSummary = fmt.Sprintf("apparmor is enabled but required kernel features are missing: %s",
			strings.Join(missingKernelFeatures, ", "))
		return
	}

	// Next check that the parser supports preferred parser features.
	// If we have any missing preferred features then apparmor is partially enabled.
	for _, feature := range preferredAppArmorParserFeatures {
		if !strutil.SortedListContains(parserFeatures, feature) {
			missingParserFeatures = append(missingParserFeatures, feature)
		}
	}
	if len(missingParserFeatures) > 0 {
		appArmorLevel = PartialAppArmor
		appArmorSummary = fmt.Sprintf("apparmor_parser is available but some features are missing: %s",
			strings.Join(missingParserFeatures, ", "))
		return
	}

	// Lastly check that the kernel supports preferred kernel features.
	for _, feature := range preferredAppArmorKernelFeatures {
		if !strutil.SortedListContains(kernelFeatures, feature) {
			missingKernelFeatures = append(missingKernelFeatures, feature)
		}
	}
	if len(missingKernelFeatures) > 0 {
		appArmorLevel = PartialAppArmor
		appArmorSummary = fmt.Sprintf("apparmor is enabled but some kernel features are missing: %s",
			strings.Join(missingKernelFeatures, ", "))
		return
	}

	// If we got here then all features are available and supported.
	appArmorLevel = FullAppArmor
	appArmorSummary = "apparmor is enabled and all features are available"
}

func probeAppArmorKernelFeatures() []string {
	// note that ioutil.ReadDir() is already sorted
	dentries, err := ioutil.ReadDir(appArmorFeaturesSysPath)
	if err != nil {
		return []string{}
	}
	features := make([]string, 0, len(dentries))
	for _, fi := range dentries {
		if fi.IsDir() {
			features = append(features, fi.Name())
		}
	}
	return features
}

func probeAppArmorParserFeatures() []string {
	parser := findAppArmorParser()
	if parser == "" {
		return []string{}
	}
	features := make([]string, 0, 1)
	if tryAppArmorParserFeature(parser, "change_profile unsafe /**,") {
		features = append(features, "unsafe")
	}
	sort.Strings(features)
	return features
}

// findAppArmorParser returns the path of the apparmor_parser binary if one is found.
func findAppArmorParser() string {
	for _, dir := range filepath.SplitList(appArmorParserSearchPath) {
		path := filepath.Join(dir, "apparmor_parser")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// tryAppArmorParserFeature attempts to pre-process a bit of apparmor syntax with a given parser.
func tryAppArmorParserFeature(parser, rule string) bool {
	cmd := exec.Command(parser, "--preprocess")
	cmd.Stdin = bytes.NewBufferString(fmt.Sprintf("profile snap-test {\n %s\n}", rule))
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
