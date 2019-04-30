// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package seccomp

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

var (
	// version-info format: <build-id> <libseccomp-version> <hash> <features>
	// Where, the hash is calculated over all syscall names supported by the
	// libseccomp library. The build-id is a 160-bit SHA-1 (40 char) string
	// and the hash is a 256-bit SHA-256 (64 char) string. Allow libseccomp
	// version to be 1-5 chars per field (eg, 1.2.3 or 12345.23456.34567)
	// and 1-30 chars of colon-separated features.
	// Ex: 7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c bpf-actlog
	validVersionInfo = regexp.MustCompile(`^[0-9a-f]{1,40} [0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5} [0-9a-f]{1,64} [-a-z0-9:]{1,30}$`)
)

type Compiler struct {
	snapSeccomp string
}

// New returns a wrapper for the compiler binary. The path to the binary is
// looked up using the lookupTool helper.
func New(lookupTool func(name string) (string, error)) (*Compiler, error) {
	if lookupTool == nil {
		panic("lookup tool func not provided")
	}

	path, err := lookupTool("snap-seccomp")
	if err != nil {
		return nil, err
	}

	return &Compiler{snapSeccomp: path}, nil
}

// VersionInfo returns the version information of the compiler. The format of
// version information is: <build-id> <libseccomp-version> <hash> <features>.
// Where, the hash is calculated over all syscall names supported by the
// libseccomp library.
func (c *Compiler) VersionInfo() (string, error) {
	cmd := exec.Command(c.snapSeccomp, "version-info")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", osutil.OutputErr(output, err)
	}
	raw := bytes.TrimSpace(output)
	// Example valid output:
	// 7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c bpf-actlog
	if match := validVersionInfo.Match(raw); !match {
		return "", fmt.Errorf("invalid format of version-info: %q", raw)
	}

	return string(raw), nil
}

// GetLibseccompVersion parses the output of VersionInfo and provides the
// libseccomp version
func GetLibseccompVersion(versionInfo string) (string, error) {
	if match := validVersionInfo.Match([]byte(versionInfo)); !match {
		return "", fmt.Errorf("invalid format of version-info: %q", versionInfo)
	}
	return strings.Split(versionInfo, " ")[1], nil
}

// GetGoSeccompFeatures parses the output of VersionInfo and provides the
// golang seccomp features
func GetGoSeccompFeatures(versionInfo string) (string, error) {
	if match := validVersionInfo.Match([]byte(versionInfo)); !match {
		return "", fmt.Errorf("invalid format of version-info: %q", versionInfo)
	}
	return strings.Split(versionInfo, " ")[3], nil
}

// HasGoSeccompFeature parses the output of VersionInfo and answers whether or
// not golang-seccomp supports the feature
func HasGoSeccompFeature(versionInfo string, feature string) (bool, error) {
	features, err := GetGoSeccompFeatures(versionInfo)
	if err != nil {
		return false, err
	}
	for _, f := range strings.Split(features, ":") {
		if f == feature {
			return true, nil
		}
	}
	return false, nil
}

// Compile compiles given source profile and saves the result to the out
// location.
func (c *Compiler) Compile(in, out string) error {
	cmd := exec.Command(c.snapSeccomp, "compile", in, out)
	if output, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}
