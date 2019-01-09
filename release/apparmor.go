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
	"sync"

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

// appArmorAssessment represents what is supported AppArmor-wise by the system.
var appArmorAssessment = &appArmorAssess{appArmorProber: &appArmorProbe{}}

// AppArmorLevel quantifies how well apparmor is supported on the current
// kernel. The computation is costly to perform. The result is cached internally.
func AppArmorLevel() AppArmorLevelType {
	appArmorAssessment.assess()
	return appArmorAssessment.level
}

// AppArmorSummary describes how well apparmor is supported on the current
// kernel. The computation is costly to perform. The result is cached
// internally.
func AppArmorSummary() string {
	appArmorAssessment.assess()
	return appArmorAssessment.summary
}

// AppArmorKernelFeatures returns a sorted list of apparmor features like
// []string{"dbus", "network"}. The result is cached internally.
func AppArmorKernelFeatures() ([]string, error) {
	return appArmorAssessment.KernelFeatures()
}

// AppArmorParserFeatures returns a sorted list of apparmor parser features
// like []string{"unsafe", ...}. The computation is costly to perform. The
// result is cached internally.
func AppArmorParserFeatures() ([]string, error) {
	return appArmorAssessment.ParserFeatures()
}

// AppArmorParserMtime returns the mtime of the parser, else 0.
func AppArmorParserMtime() int64 {
	var mtime int64
	mtime = 0

	if path, err := findAppArmorParser(); err == nil {
		if fi, err := os.Stat(path); err == nil {
			mtime = fi.ModTime().Unix()
		}
	}
	return mtime
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

type appArmorProber interface {
	KernelFeatures() ([]string, error)
	ParserFeatures() ([]string, error)
}

type appArmorAssess struct {
	appArmorProber
	// level contains the assessment of the "level" of apparmor support.
	level AppArmorLevelType
	// summary contains a human readable description of the assessment.
	summary string

	once sync.Once
}

func (aaa *appArmorAssess) assess() {
	aaa.once.Do(func() {
		aaa.level, aaa.summary = aaa.doAssess()
	})
}

func (aaa *appArmorAssess) doAssess() (level AppArmorLevelType, summary string) {
	// First, quickly check if apparmor is available in the kernel at all.
	kernelFeatures, err := aaa.KernelFeatures()
	if os.IsNotExist(err) {
		return NoAppArmor, "apparmor not enabled"
	}
	// Then check that the parser supports the required parser features.
	// If we have any missing required features then apparmor is unusable.
	parserFeatures, err := aaa.ParserFeatures()
	if os.IsNotExist(err) {
		return NoAppArmor, "apparmor_parser not found"
	}
	var missingParserFeatures []string
	for _, feature := range requiredAppArmorParserFeatures {
		if !strutil.SortedListContains(parserFeatures, feature) {
			missingParserFeatures = append(missingParserFeatures, feature)
		}
	}
	if len(missingParserFeatures) > 0 {
		summary := fmt.Sprintf("apparmor_parser is available but required parser features are missing: %s",
			strings.Join(missingParserFeatures, ", "))
		return UnusableAppArmor, summary
	}

	// Next, check that the kernel supports the required kernel features.
	var missingKernelFeatures []string
	for _, feature := range requiredAppArmorKernelFeatures {
		if !strutil.SortedListContains(kernelFeatures, feature) {
			missingKernelFeatures = append(missingKernelFeatures, feature)
		}
	}
	if len(missingKernelFeatures) > 0 {
		summary := fmt.Sprintf("apparmor is enabled but required kernel features are missing: %s",
			strings.Join(missingKernelFeatures, ", "))
		return UnusableAppArmor, summary
	}

	// Next check that the parser supports preferred parser features.
	// If we have any missing preferred features then apparmor is partially enabled.
	for _, feature := range preferredAppArmorParserFeatures {
		if !strutil.SortedListContains(parserFeatures, feature) {
			missingParserFeatures = append(missingParserFeatures, feature)
		}
	}
	if len(missingParserFeatures) > 0 {
		summary := fmt.Sprintf("apparmor_parser is available but some features are missing: %s",
			strings.Join(missingParserFeatures, ", "))
		return PartialAppArmor, summary
	}

	// Lastly check that the kernel supports preferred kernel features.
	for _, feature := range preferredAppArmorKernelFeatures {
		if !strutil.SortedListContains(kernelFeatures, feature) {
			missingKernelFeatures = append(missingKernelFeatures, feature)
		}
	}
	if len(missingKernelFeatures) > 0 {
		summary := fmt.Sprintf("apparmor is enabled but some kernel features are missing: %s",
			strings.Join(missingKernelFeatures, ", "))
		return PartialAppArmor, summary
	}

	// If we got here then all features are available and supported.
	return FullAppArmor, "apparmor is enabled and all features are available"
}

type appArmorProbe struct {
	// kernelFeatures contains a list of kernel features that are supported.
	kernelFeatures []string
	// kernelError contains an error, if any, encountered when
	// discovering available kernel features.
	kernelError error
	// parserFeatures contains a list of parser features that are supported.
	parserFeatures []string
	// parserError contains an error, if any, encountered when
	// discovering available parser features.
	parserError error

	probeKernelOnce sync.Once
	probeParserOnce sync.Once
}

func (aap *appArmorProbe) KernelFeatures() ([]string, error) {
	aap.probeKernelOnce.Do(func() {
		aap.kernelFeatures, aap.kernelError = probeAppArmorKernelFeatures()
	})
	return aap.kernelFeatures, aap.kernelError
}

func (aap *appArmorProbe) ParserFeatures() ([]string, error) {
	aap.probeParserOnce.Do(func() {
		aap.parserFeatures, aap.parserError = probeAppArmorParserFeatures()
	})
	return aap.parserFeatures, aap.parserError
}

func probeAppArmorKernelFeatures() ([]string, error) {
	// note that ioutil.ReadDir() is already sorted
	dentries, err := ioutil.ReadDir(appArmorFeaturesSysPath)
	if err != nil {
		return []string{}, err
	}
	features := make([]string, 0, len(dentries))
	for _, fi := range dentries {
		if fi.IsDir() {
			features = append(features, fi.Name())
		}
	}
	return features, nil
}

func probeAppArmorParserFeatures() ([]string, error) {
	parser, err := findAppArmorParser()
	if err != nil {
		return []string{}, err
	}
	features := make([]string, 0, 1)
	if tryAppArmorParserFeature(parser, "change_profile unsafe /**,") {
		features = append(features, "unsafe")
	}
	sort.Strings(features)
	return features, nil
}

// findAppArmorParser returns the path of the apparmor_parser binary if one is found.
func findAppArmorParser() (string, error) {
	for _, dir := range filepath.SplitList(appArmorParserSearchPath) {
		path := filepath.Join(dir, "apparmor_parser")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", os.ErrNotExist
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

// mocking

type mockAppArmorProbe struct {
	kernelFeatures []string
	kernelError    error
	parserFeatures []string
	parserError    error
}

func (m *mockAppArmorProbe) KernelFeatures() ([]string, error) {
	return m.kernelFeatures, m.kernelError
}

func (m *mockAppArmorProbe) ParserFeatures() ([]string, error) {
	return m.parserFeatures, m.parserError
}

// MockAppArmorLevel makes the system believe it has certain level of apparmor
// support.
//
// AppArmor kernel and parser features are set to unrealistic values that do
// not match the requested level. Use this function to observe behavior that
// relies solely on the apparmor level value.
func MockAppArmorLevel(level AppArmorLevelType) (restore func()) {
	oldAppArmorAssessment := appArmorAssessment
	mockProbe := &mockAppArmorProbe{
		kernelFeatures: []string{"mocked-kernel-feature"},
		parserFeatures: []string{"mocked-parser-feature"},
	}
	appArmorAssessment = &appArmorAssess{
		appArmorProber: mockProbe,
		level:          level,
		summary:        fmt.Sprintf("mocked apparmor level: %s", level),
	}
	appArmorAssessment.once.Do(func() {})
	return func() {
		appArmorAssessment = oldAppArmorAssessment
	}
}

// MockAppArmorFeatures makes the system believe it has certain kernel and
// parser features.
//
// AppArmor level and summary are automatically re-assessed as needed
// on both the change and the restore process. Use this function to
// observe real assessment of arbitrary features.
func MockAppArmorFeatures(kernelFeatures []string, kernelError error, parserFeatures []string, parserError error) (restore func()) {
	oldAppArmorAssessment := appArmorAssessment
	mockProbe := &mockAppArmorProbe{
		kernelFeatures: kernelFeatures,
		kernelError:    kernelError,
		parserFeatures: parserFeatures,
		parserError:    parserError,
	}
	appArmorAssessment = &appArmorAssess{
		appArmorProber: mockProbe,
	}
	appArmorAssessment.assess()
	return func() {
		appArmorAssessment = oldAppArmorAssessment
	}

}
