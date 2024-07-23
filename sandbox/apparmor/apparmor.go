// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2024 Canonical Ltd
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

package apparmor

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
)

// For mocking in tests
var (
	osMkdirAll        = os.MkdirAll
	osutilAtomicWrite = osutil.AtomicWrite
)

// ValidateNoAppArmorRegexp will check that the given string does not
// contain AppArmor regular expressions (AARE), double quotes or \0.
// Note that to check the inverse of this, that is that a string has
// valid AARE, one should use interfaces/utils.NewPathPattern().
func ValidateNoAppArmorRegexp(s string) error {
	const AARE = `?*[]{}^"` + "\x00"

	if strings.ContainsAny(s, AARE) {
		return fmt.Errorf("%q contains a reserved apparmor char from %s", s, AARE)
	}
	return nil
}

type AAREExclusionPatternsOptions struct {
	// Prefix is a string to include on every line in the permutations before
	// the exclusion permutation itself.
	Prefix string
	// Suffix is a string to include on every line in the permutations after
	// the exclusion permutation itself.
	Suffix string

	// TODO: add options for generating non-filepaths like what we need for the
	// unconfined profile transition exclusion in snap-confine's profile as well
	// as an option for adding an extra character to the very first rule like we
	// need in the home interface
}

// InsertAAREExclusionPatterns replaces a ###EXCL{<pref>,<suf>}### snippet
// with matching prefix and comma separated suffixes with a set of rules generated
// by GenerateAAREExclusionPatterns.
func InsertAAREExclusionPatterns(aaRules string, excludePatterns []string, opts *AAREExclusionPatternsOptions) (string, error) {
	exclussionPatterns, err := GenerateAAREExclusionPatterns(excludePatterns, opts)
	if err != nil {
		return "", err
	}

	placeHolder := fmt.Sprintf("###EXCL{%s<>%s:%s}###", opts.Prefix, opts.Suffix, strings.Join(excludePatterns[:], ","))

	if !strings.Contains(aaRules, placeHolder) {
		return "", fmt.Errorf("placeholder pattern %q not found", placeHolder)
	}
	return strings.Replace(aaRules, placeHolder, strings.TrimSuffix(exclussionPatterns, "\n"), -1), nil
}

// GenerateAAREExclusionPatterns generates a series of valid AppArmor
// regular expression negation rules such that anything except the specific
// excludePatterns will match with the specified prefix and suffix rules. For
// example to allow reading any file except those matching /usr/*/foo, you would
// call this function with "/usr/*/foo" as the first argument, "" as the prefix
// and " r," as the suffix (the suffix being the main part of the read rule) and
// this function would return the following multi-line string with the relevant
// rules:
//
// /[^u]** r,
// /u[^s]** r,
// /us[^r]** r,
// /usr[^/]** r,
// /usr/*/[^f]** r,
// /usr/*/f[^o]** r,
// /usr/*/fo[^o]** r,
//
// This function only treats '*' specially in the string and does not handle any
// other alternations etc. that AARE may more generally support, and all
// patterns provided must be absolute filepaths that are at least 2 runes long.
//
// This function also works with multiple exclude patterns such as specifying to
// exclude "/usr/lib/snapd" and "/var/lib/snapd" with suffix " r," would yield:
//
// /[^uv]** r,
// /{u[^s],v[^a]}** r,
// /{us[^r],va[^r]}** r,
// /{usr[^/],var[^/]}** r,
// /{usr/[^l],var/[^l]}** r,
// /{usr/l[^i],var/l[^i]}** r,
// /{usr/li[^b],var/li[^b]}** r,
// /{usr/lib[^/],var/lib[^/]}** r,
// /{usr/lib/[^s],var/lib/[^s]}** r,
// /{usr/lib/s[^n],var/lib/s[^n]}** r,
// /{usr/lib/sn[^a],var/lib/sn[^a]}** r,
// /{usr/lib/sna[^p],var/lib/sna[^p]}** r,
// /{usr/lib/snap[^d],var/lib/snap[^d]}** r,
//
// Note that with the previous rules, /usr/lib/snapdaemon would not match any rule
//
// This function has the following limitations:
// The first character after a subpattern common to two or more excludePatterns cannot
// be '*' on any of the excludePatterns that share the common prefix.
// Eg. ["/snap/core/**", "/snap/*/core/**"] where '/snap/' would be the subpattern common
// to both excludePattern, and '*' would be the first character after the common subpattern
// in the second excludePattern.
// This is because there are no apparmor rules that can fulfill both requirements.
// For /snap/[^c]** -> It will also match /snap/a/core/** which should be excluded by
// the second pattern.
// For /snap/[^c*]** -> It will also exclude access to /snap/a/a that should be allowed
// as it is not explicitly excluded by any pattern
//
// When the '*' is used to exclude suffixes, like in ["/*.bin"], rules should be generated
// in a reverse way:
//
// /*[^n]{,/**} rw,
// /*[^i]n{,/**} rw,
// /*[^b]in{,/**} rw,
// /*[^.]bin{,/**} rw,
//
// While generating those rules is technically possible, it will make the logic way more
// complex, thus the function would just return an error if a pattern of this kind is found.
// This functionality can be added in a subsequent PR if needed in the future
func GenerateAAREExclusionPatterns(excludePatterns []string, opts *AAREExclusionPatternsOptions) (string, error) {
	if len(excludePatterns) == 0 {
		return "", errors.New("no patterns provided")
	}
	seen := map[string]bool{}
	for _, patt := range excludePatterns {
		// check for duplicates
		if seen[patt] {
			return "", errors.New("exclude patterns contain duplicates")
		}
		seen[patt] = true

		// check if it is at least legnth 2
		if len(patt) < 2 {
			return "", errors.New("exclude patterns must be at least length 2")
		}

		// check that it starts as an absolute path
		if patt[0] != '/' {
			return "", errors.New("exclude patterns must be absolute filepaths")
		}

		// TODO: should we also validate that the only character in the pattern
		// from AARE is "*" ?
	}
	if opts == nil {
		opts = &AAREExclusionPatternsOptions{}
	}
	return generateAAREExclusionPatternsGenericImpl(excludePatterns, opts)

}

func generateAAREExclusionPatternsGenericImpl(excludePatterns []string, opts *AAREExclusionPatternsOptions) (string, error) {
	// Find the length of longest pattern (size)
	size := 0
	for _, pattern := range excludePatterns {
		if len(pattern) > size {
			size = len(pattern)
		}
	}

	// Find the longest prefix common to ALL patterns.
	commonPrefix, _ := strutil.FindCommonPrefix(excludePatterns)

	// This loop will iterate over the length of the longest excludePattern
	// (charInd = 1..size), generating an apparmor rule on each iteration
	// for the corresponding subpatterns, understanding as such, the first
	// (charInd+1) characters of the excludePatterns.
	var builder strings.Builder
	for charInd := 1; charInd < size; charInd++ {
		// This loop will group the subpatterns properly, generating the subpatterns map, where:
		//     - the key would be the subpatternPrefix, considering as such the subpattern except
		//       its last character (pattern[0:charInd]).
		//     - the value would be the charset, which would be the subpattern last character
		//       (pattern[charInd]). If several subpatterns share the same subpatternPrefix, the
		//       charset would be a string including the last character of all those subpatterns.
		subpatternPrefix := ""
		subpatterns := map[string]string{}
		for _, pattern := range excludePatterns {
			// Handle unsupported cases
			if (charInd < len(pattern)) && pattern[charInd] == '*' {
				// Check if the excludePattern has a character different from '/' after a wildcard
				if ((charInd + 1) < len(pattern)) && (pattern[charInd+1] != '/') {
					return "", errors.New("exclude patterns does not support suffixes for now")
				}
				// Check if '*' is the first character after a common subpattern
				for _, patt := range excludePatterns {
					if (patt != pattern) && ((charInd) < len(patt)) && (pattern[:charInd] == patt[:charInd]) && (patt[charInd] != '*') {
						return "", errors.New("first character after a common subpattern cannot be a wildcard")
					}
				}
			}

			// Skip patterns that are already finished, wildcards and slashes preceded by wildcards.
			if (charInd >= len(pattern)) ||
				(pattern[charInd] == '*') ||
				((pattern[charInd] == '/') && (pattern[charInd-1] == '*')) {
				continue
			}
			// Group subpatterns
			subpatternPrefix = pattern[:charInd]
			if charset, exists := subpatterns[subpatternPrefix]; !exists {
				// Add the pattern if it didn't exist yet
				subpatterns[subpatternPrefix] = string(pattern[charInd])
			} else {
				if !strings.Contains(charset, string(pattern[charInd])) {
					// Two patterns only differ on the last character
					subpatterns[subpatternPrefix] = charset + string(pattern[charInd])
				}
			}
		}

		// Write patterns
		if len(subpatterns) > 0 {
			// First order keys to ensure profiles are always the same
			// Sort key in map to ensure consistency in results
			prefixes := make([]string, 0, len(subpatterns))
			for prefix := range subpatterns {
				prefixes = append(prefixes, prefix)
			}
			sort.Strings(prefixes)

			// <prefix><common-prefix><exp><suffix>
			// eg. /squashfs-root/usr/lib/[^a]**			if len(subpatterns) == 1
			// eg. /squashfs-root/usr/lib/{[^a],[^b]}**		if len(subpatterns) > 1
			builder.WriteString(opts.Prefix)
			if charInd < len(commonPrefix) {
				builder.WriteString(commonPrefix[:charInd])
			} else {
				builder.WriteString(commonPrefix)
			}
			if len(subpatterns) > 1 {
				builder.WriteRune('{')
			}
			for i := range prefixes {
				if i > 0 {
					builder.WriteRune(',')
				}
				if len(commonPrefix) < len(prefixes[i]) {
					builder.WriteString(prefixes[i][len(commonPrefix):])
				}
				builder.WriteString("[^" + subpatterns[prefixes[i]] + "]")
			}
			if len(subpatterns) > 1 {
				builder.WriteRune('}')
			}
			builder.WriteString("**")
			builder.WriteString(opts.Suffix)
			builder.WriteRune('\n')
		}
	}
	return builder.String(), nil
}

// LevelType encodes the kind of support for apparmor
// found on this system.
type LevelType int

const (
	// Unknown indicates that apparmor was not probed yet.
	Unknown LevelType = iota
	// Unsupported indicates that apparmor is not enabled.
	Unsupported
	// Unusable indicates that apparmor is enabled but cannot be used.
	Unusable
	// Partial indicates that apparmor is enabled but some
	// features are missing.
	Partial
	// Full indicates that all features are supported.
	Full
)

func setupConfCacheDirs(newrootdir string) {
	ConfDir = filepath.Join(newrootdir, "/etc/apparmor.d")
	CacheDir = filepath.Join(newrootdir, "/var/cache/apparmor")
	hostAbi30File = filepath.Join(newrootdir, "/etc/apparmor.d/abi/3.0")
	hostAbi40File = filepath.Join(newrootdir, "/etc/apparmor.d/abi/4.0")

	SystemCacheDir = filepath.Join(ConfDir, "cache")
	exists, isDir, _ := osutil.DirExists(SystemCacheDir)
	if !exists || !isDir {
		// some systems use a single cache dir instead of splitting
		// out the system cache
		// TODO: it seems Solus has a different setup too, investigate this
		SystemCacheDir = CacheDir
	}

	snapConfineDir := "snap-confine"
	if _, internal, err := AppArmorParser(); err == nil {
		if internal {
			snapConfineDir = "snap-confine.internal"
		}
	}
	SnapConfineAppArmorDir = filepath.Join(dirs.SnapdStateDir(newrootdir), "apparmor", snapConfineDir)
}

func init() {
	dirs.AddRootDirCallback(setupConfCacheDirs)
	setupConfCacheDirs(dirs.GlobalRootDir)
}

var (
	ConfDir                string
	CacheDir               string
	SystemCacheDir         string
	SnapConfineAppArmorDir string
)

func (level LevelType) String() string {
	switch level {
	case Unknown:
		return "unknown"
	case Unsupported:
		return "none"
	case Unusable:
		return "unusable"
	case Partial:
		return "partial"
	case Full:
		return "full"
	}
	return fmt.Sprintf("AppArmorLevelType:%d", level)
}

// appArmorAssessment represents what is supported AppArmor-wise by the system.
var appArmorAssessment = &appArmorAssess{appArmorProber: &appArmorProbe{}}

// ProbedLevel quantifies how well apparmor is supported on the current
// kernel. The computation is costly to perform. The result is cached internally.
func ProbedLevel() LevelType {
	appArmorAssessment.assess()
	return appArmorAssessment.level
}

// Summary describes how well apparmor is supported on the current
// kernel. The computation is costly to perform. The result is cached
// internally.
func Summary() string {
	appArmorAssessment.assess()
	return appArmorAssessment.summary
}

// KernelFeatures returns a sorted list of apparmor features like
// []string{"dbus", "network"}. The result is cached internally.
func KernelFeatures() ([]string, error) {
	return appArmorAssessment.KernelFeatures()
}

// ParserFeatures returns a sorted list of apparmor parser features
// like []string{"unsafe", ...}. The computation is costly to perform. The
// result is cached internally.
func ParserFeatures() ([]string, error) {
	return appArmorAssessment.ParserFeatures()
}

// ParserMtime returns the mtime of the AppArmor parser, else 0.
func ParserMtime() int64 {
	var mtime int64
	mtime = 0

	if cmd, _, err := AppArmorParser(); err == nil {
		if fi, err := os.Stat(cmd.Path); err == nil {
			mtime = fi.ModTime().Unix()
		}
	}
	return mtime
}

// FeaturesSupported contains information about supported AppArmor kernel and
// parser features.
type FeaturesSupported struct {
	KernelFeatures []string
	ParserFeatures []string
}

// PromptingSupported returns true if prompting is supported by the system.
// Otherwise, returns false, along with a string explaining why prompting is
// unsupported.
func PromptingSupported() (bool, string) {
	kernelFeatures, err := appArmorAssessment.KernelFeatures()
	if err != nil {
		return false, fmt.Sprintf("cannot check apparmor kernel features: %v", err)
	}
	parserFeatures, err := appArmorAssessment.ParserFeatures()
	if err != nil {
		return false, fmt.Sprintf("cannot check apparmor parser features: %v", err)
	}
	apparmorFeatures := FeaturesSupported{
		KernelFeatures: kernelFeatures,
		ParserFeatures: parserFeatures,
	}
	return PromptingSupportedByFeatures(&apparmorFeatures)
}

// PromptingSupportedByFeatures returns whether prompting is supported by the
// given AppArmor kernel and parser features.
func PromptingSupportedByFeatures(apparmorFeatures *FeaturesSupported) (bool, string) {
	if apparmorFeatures == nil {
		return false, "no apparmor features provided"
	}
	if !strutil.ListContains(apparmorFeatures.KernelFeatures, "policy:permstable32:prompt") {
		return false, "apparmor kernel features do not support prompting"
	}
	if !strutil.ListContains(apparmorFeatures.ParserFeatures, "prompt") {
		return false, "apparmor parser does not support the prompt qualifier"
	}
	// TODO: return true once the prompting API is merged and ready
	// return true, ""
	return false, "requires newer version of snapd"
}

// probe related code

var (
	// requiredParserFeatures denotes the features that must be present in the parser.
	// Absence of any of those features results in the effective level be at most UnusableAppArmor.
	requiredParserFeatures = []string{
		"unsafe",
	}
	// preferredParserFeatures denotes the features that should be present in the parser.
	// Absence of any of those features results in the effective level be at most PartialAppArmor.
	preferredParserFeatures = []string{
		"unsafe",
	}
	// requiredKernelFeatures denotes the features that must be present in the kernel.
	// Absence of any of those features results in the effective level be at most UnusableAppArmor.
	requiredKernelFeatures = []string{
		// For now, require at least file and simply prefer the rest.
		"file",
	}
	// preferredKernelFeatures denotes the features that should be present in the kernel.
	// Absence of any of those features results in the effective level be at most PartialAppArmor.
	preferredKernelFeatures = []string{
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
	parserSearchPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

	// Filesystem root defined locally to avoid dependency on the
	// 'dirs' package
	rootPath = "/"

	// hostAbi30File is the path to the apparmor "3.0" ABI file and is typically
	// /etc/apparmor.d/abi/3.0. It is not present on all systems. It is notably
	// absent when using apparmor 2.x. The variable reacts to changes to global
	// root directory.
	hostAbi30File = ""
	// hostAbi40File is like hostAbi30File but for ABI 4.0
	hostAbi40File = ""
)

// Each apparmor feature is manifested as a directory entry.
const featuresSysPath = "sys/kernel/security/apparmor/features"

type appArmorProber interface {
	KernelFeatures() ([]string, error)
	ParserFeatures() ([]string, error)
}

type appArmorAssess struct {
	appArmorProber
	// level contains the assessment of the "level" of apparmor support.
	level LevelType
	// summary contains a human readable description of the assessment.
	summary string

	once sync.Once
}

func (aaa *appArmorAssess) assess() {
	aaa.once.Do(func() {
		aaa.level, aaa.summary = aaa.doAssess()
	})
}

func (aaa *appArmorAssess) doAssess() (level LevelType, summary string) {
	// First, quickly check if apparmor is available in the kernel at all.
	kernelFeatures, err := aaa.KernelFeatures()
	if os.IsNotExist(err) {
		return Unsupported, "apparmor not enabled"
	}
	// Then check that the parser supports the required parser features.
	// If we have any missing required features then apparmor is unusable.
	parserFeatures, err := aaa.ParserFeatures()
	if os.IsNotExist(err) {
		return Unsupported, "apparmor_parser not found"
	}
	var missingParserFeatures []string
	for _, feature := range requiredParserFeatures {
		if !strutil.SortedListContains(parserFeatures, feature) {
			missingParserFeatures = append(missingParserFeatures, feature)
		}
	}
	if len(missingParserFeatures) > 0 {
		summary := fmt.Sprintf("apparmor_parser is available but required parser features are missing: %s",
			strings.Join(missingParserFeatures, ", "))
		return Unusable, summary
	}

	// Next, check that the kernel supports the required kernel features.
	var missingKernelFeatures []string
	for _, feature := range requiredKernelFeatures {
		if !strutil.SortedListContains(kernelFeatures, feature) {
			missingKernelFeatures = append(missingKernelFeatures, feature)
		}
	}
	if len(missingKernelFeatures) > 0 {
		summary := fmt.Sprintf("apparmor is enabled but required kernel features are missing: %s",
			strings.Join(missingKernelFeatures, ", "))
		return Unusable, summary
	}

	// Next check that the parser supports preferred parser features.
	// If we have any missing preferred features then apparmor is partially enabled.
	for _, feature := range preferredParserFeatures {
		if !strutil.SortedListContains(parserFeatures, feature) {
			missingParserFeatures = append(missingParserFeatures, feature)
		}
	}
	if len(missingParserFeatures) > 0 {
		summary := fmt.Sprintf("apparmor_parser is available but some features are missing: %s",
			strings.Join(missingParserFeatures, ", "))
		return Partial, summary
	}

	// Lastly check that the kernel supports preferred kernel features.
	for _, feature := range preferredKernelFeatures {
		if !strutil.SortedListContains(kernelFeatures, feature) {
			missingKernelFeatures = append(missingKernelFeatures, feature)
		}
	}
	if len(missingKernelFeatures) > 0 {
		summary := fmt.Sprintf("apparmor is enabled but some kernel features are missing: %s",
			strings.Join(missingKernelFeatures, ", "))
		return Partial, summary
	}

	// If we got here then all features are available and supported.
	note := ""
	if strutil.SortedListContains(parserFeatures, "snapd-internal") {
		note = " (using snapd provided apparmor_parser)"
	}
	return Full, "apparmor is enabled and all features are available" + note
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
		aap.kernelFeatures, aap.kernelError = probeKernelFeatures()
	})
	return aap.kernelFeatures, aap.kernelError
}

func (aap *appArmorProbe) ParserFeatures() ([]string, error) {
	aap.probeParserOnce.Do(func() {
		aap.parserFeatures, aap.parserError = probeParserFeatures()
	})
	return aap.parserFeatures, aap.parserError
}

func probeKernelFeatures() ([]string, error) {
	// note that os.ReadDir() is already sorted
	dentries, err := os.ReadDir(filepath.Join(rootPath, featuresSysPath))
	if err != nil {
		return []string{}, err
	}
	features := make([]string, 0, len(dentries))
	for _, fi := range dentries {
		if fi.IsDir() {
			features = append(features, fi.Name())
			// also read any sub-features
			subdenties, err := os.ReadDir(filepath.Join(rootPath, featuresSysPath, fi.Name()))
			if err != nil {
				return []string{}, err
			}
			for _, subfi := range subdenties {
				if subfi.IsDir() {
					features = append(features, fi.Name()+":"+subfi.Name())
				}
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(rootPath, featuresSysPath, "policy", "permstable32")); err == nil {
		permstableFeatures := strings.Fields(string(data))
		for _, feat := range permstableFeatures {
			features = append(features, fmt.Sprintf("policy:permstable32:%s", feat))
		}
	}
	// Features must be sorted
	sort.Strings(features)
	return features, nil
}

func probeParserFeatures() ([]string, error) {
	var featureProbes = []struct {
		feature string
		flags   []string
		probe   string
		minVer  string
	}{
		{
			feature: "unsafe",
			probe:   "change_profile unsafe /**,",
		},
		{
			feature: "include-if-exists",
			probe:   `#include if exists "/foo"`,
		},
		{
			feature: "qipcrtr-socket",
			probe:   "network qipcrtr dgram,",
		},
		{
			feature: "mqueue",
			probe:   "mqueue,",
			minVer:  "4.0.1",
		},
		{
			feature: "cap-bpf",
			probe:   "capability bpf,",
		},
		{
			feature: "cap-audit-read",
			probe:   "capability audit_read,",
		},
		{
			feature: "xdp",
			probe:   "network xdp,",
		},
		{
			feature: "userns",
			probe:   "userns,",
		},
		{
			feature: "unconfined",
			flags:   []string{"unconfined"},
			probe:   "# test unconfined",
		},
		{
			feature: "prompt",
			probe:   "prompt /foo r,",
		},
	}
	_, internal, err := AppArmorParser()
	if err != nil {
		return []string{}, err
	}

	aaVer := appArmorParserVersion()
	logger.Debugf("apparmor parser version: %q", aaVer)

	features := make([]string, 0, len(featureProbes)+1)
	for _, fp := range featureProbes {
		if minVer := fp.minVer; minVer != "" {
			res, err := strutil.VersionCompare(aaVer, minVer)
			if err != nil {
				logger.Noticef("cannot compare versions: %s", err)
				continue
			}
			if res < 0 {
				logger.Debugf("skipping apparmor feature check for %s due to insufficient version %s", fp.feature, aaVer)
				continue
			}
		}
		// recreate the Cmd each time so we can exec it each time
		cmd, _, _ := AppArmorParser()
		err := tryAppArmorParserFeature(cmd, fp.flags, fp.probe)
		if err != nil {
			logger.Debugf("cannot probe apparmor feature %q: %v", fp.feature, err)
		} else {
			features = append(features, fp.feature)
		}
	}
	if internal {
		features = append(features, "snapd-internal")
	}
	sort.Strings(features)
	logger.Debugf("probed apparmor parser features for version %s (internal=%v): %v", aaVer, internal, features)
	return features, nil
}

func systemAppArmorLoadsSnapPolicy() bool {
	// on older Ubuntu systems the system installed apparmor may try and
	// load snapd generated apparmor policy (LP: #2024637)
	f, err := os.Open(filepath.Join(dirs.GlobalRootDir, "/lib/apparmor/functions"))
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Debugf("cannot open apparmor functions file: %v", err)
		}
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, dirs.SnapAppArmorDir) {
			return true
		}
	}
	if scanner.Err() != nil {
		logger.Debugf("cannot scan apparmor functions file: %v", scanner.Err())
	}

	return false
}

func snapdAppArmorSupportsReexecImpl() bool {
	hostInfoDir := filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir)
	_, flags, err := snapdtool.SnapdVersionFromInfoFile(hostInfoDir)
	return err == nil && flags["SNAPD_APPARMOR_REEXEC"] == "1"
}

var snapdAppArmorSupportsReexec = snapdAppArmorSupportsReexecImpl

// AppArmorParser returns an exec.Cmd for the apparmor_parser binary, and a
// boolean to indicate whether this is internal to snapd (ie is provided by
// snapd)
func AppArmorParser() (cmd *exec.Cmd, internal bool, err error) {
	// first see if we have our own internal copy which could come from the
	// snapd snap (likely) or be part of the snapd distro package (unlikely)
	// - but only use the internal one when we know that the system
	// installed snapd-apparmor support re-exec
	if path, err := snapdtool.InternalToolPath("apparmor_parser"); err == nil {
		if osutil.IsExecutable(path) && snapdAppArmorSupportsReexec() && !systemAppArmorLoadsSnapPolicy() {
			prefix := strings.TrimSuffix(path, "apparmor_parser")
			snapdAbi30File := filepath.Join(prefix, "/apparmor.d/abi/3.0")
			snapdAbi40File := filepath.Join(prefix, "/apparmor.d/abi/4.0")

			// When using the internal apparmor_parser also use its own
			// configuration and includes etc plus also ensure we use the 4.0
			// feature ABI to get the widest array of policy features across
			// the widest array of kernel versions.
			//
			// In case snapd is injected into snapd snap, with
			// older apparmor, use that instead so that things
			// don't generally fail.
			abiFile := ""
			fi40, err40 := os.Lstat(snapdAbi40File)
			fi30, err30 := os.Lstat(snapdAbi30File)
			switch {
			case err40 == nil && !fi40.IsDir():
				abiFile = snapdAbi40File
			case err30 == nil && !fi30.IsDir():
				abiFile = snapdAbi30File
			default:
				return nil, false, fmt.Errorf("internal snapd apparmor_parser but no abi files")
			}

			args := []string{
				"--config-file", filepath.Join(prefix, "/apparmor/parser.conf"),
				"--base", filepath.Join(prefix, "/apparmor.d"),
				"--policy-features", abiFile,
			}

			return exec.Command(path, args...), true, nil
		}
	}

	// now search for one in the configured parserSearchPath
	for _, dir := range filepath.SplitList(parserSearchPath) {
		path := filepath.Join(dir, "apparmor_parser")
		if _, err := os.Stat(path); err == nil {
			if fi, err := os.Lstat(hostAbi40File); err == nil && !fi.IsDir() {
				return exec.Command(path, "--policy-features", hostAbi40File), false, nil
			}

			// Perhaps 3.0?
			if fi, err := os.Lstat(hostAbi30File); err == nil && !fi.IsDir() {
				return exec.Command(path, "--policy-features", hostAbi30File), false, nil
			}

			// Most likely 2.0
			return exec.Command(path), false, nil
		}
	}

	return nil, false, os.ErrNotExist
}

func appArmorParserVersion() string {
	cmd, _, _ := AppArmorParser()
	cmd.Args = append(cmd.Args, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	logger.Debugf("apparmor_parser --version\n%s", output)
	// output is like "AppArmor parser version 2.13.4\n"
	// "Copyright ..."
	// get the version number from the first line
	parts := strings.Split(strings.Split(string(output), "\n")[0], " ")
	return parts[len(parts)-1]
}

// tryAppArmorParserFeature attempts to pre-process a bit of apparmor syntax with a given parser.
func tryAppArmorParserFeature(cmd *exec.Cmd, flags []string, rule string) error {
	cmd.Args = append(cmd.Args, "--preprocess")
	flagSnippet := ""
	if len(flags) > 0 {
		flagSnippet = fmt.Sprintf("flags=(%s) ", strings.Join(flags, ","))
	}
	cmd.Stdin = bytes.NewBufferString(fmt.Sprintf("profile snap-test %s{\n %s\n}",
		flagSnippet, rule))
	output, err := cmd.CombinedOutput()
	// older versions of apparmor_parser can exit with success even
	// though they fail to parse
	if err != nil || strings.Contains(string(output), "parser error") {
		return fmt.Errorf("apparmor_parser failed: %v: %s", err, output)
	}
	return nil
}

// UpdateHomedirsTunable sets the AppArmor HOMEDIRS tunable to the list of the
// specified directories. This directly affects the value of the AppArmor
// @{HOME} variable. See the "/etc/apparmor.d/tunables/home" file for more
// information.
func UpdateHomedirsTunable(homedirs []string) error {
	homeTunableDir := filepath.Join(ConfDir, "tunables/home.d")
	tunableFilePath := filepath.Join(homeTunableDir, "snapd")

	// If the file is not there and `homedirs` is empty, do nothing; this is
	// not just an optimisation, but a necessity in Ubuntu Core: the
	// /etc/apparmor.d/ tree is read-only, and attempting to create the file
	// would generate an error.
	if len(homedirs) == 0 && !osutil.FileExists(tunableFilePath) {
		return nil
	}

	if err := osMkdirAll(homeTunableDir, 0755); err != nil {
		return fmt.Errorf("cannot create AppArmor tunable directory: %v", err)
	}

	contents := &bytes.Buffer{}
	fmt.Fprintln(contents, "# Generated by snapd -- DO NOT EDIT!")
	if len(homedirs) > 0 {
		contents.Write([]byte("@{HOMEDIRS}+="))
		separator := ""
		for _, dir := range homedirs {
			fmt.Fprintf(contents, `%s"%s"`, separator, dir)
			separator = " "
		}
		contents.Write([]byte("\n"))
	}
	return osutilAtomicWrite(tunableFilePath, contents, 0644, 0)
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
func MockLevel(level LevelType) (restore func()) {
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
func MockFeatures(kernelFeatures []string, kernelError error, parserFeatures []string, parserError error) (restore func()) {
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

func MockParserSearchPath(new string) (restore func()) {
	oldAppArmorParserSearchPath := parserSearchPath
	parserSearchPath = new
	return func() {
		parserSearchPath = oldAppArmorParserSearchPath
	}
}
