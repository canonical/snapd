// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package sysconfig

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

func ubuntuDataCloudDir(rootdir string) string {
	return filepath.Join(rootdir, "etc/cloud/")
}

// DisableCloudInit will disable cloud-init permanently by writing a
// cloud-init.disabled config file in etc/cloud under the target dir, which
// instructs cloud-init-generator to not trigger new cloud-init invocations.
// Note that even with this disabled file, a root user could still manually run
// cloud-init, but this capability is not provided to any strictly confined
// snap.
func DisableCloudInit(rootDir string) error {
	ubuntuDataCloud := ubuntuDataCloudDir(rootDir)
	if err := os.MkdirAll(ubuntuDataCloud, 0755); err != nil {
		return fmt.Errorf("cannot make cloud config dir: %v", err)
	}
	if err := ioutil.WriteFile(filepath.Join(ubuntuDataCloud, "cloud-init.disabled"), nil, 0644); err != nil {
		return fmt.Errorf("cannot disable cloud-init: %v", err)
	}

	return nil
}

func installCloudInitCfg(src, targetdir string) error {
	ccl, err := filepath.Glob(filepath.Join(src, "*.cfg"))
	if err != nil {
		return err
	}
	if len(ccl) == 0 {
		return nil
	}

	ubuntuDataCloudCfgDir := filepath.Join(ubuntuDataCloudDir(targetdir), "cloud.cfg.d/")
	if err := os.MkdirAll(ubuntuDataCloudCfgDir, 0755); err != nil {
		return fmt.Errorf("cannot make cloud config dir: %v", err)
	}

	for _, cc := range ccl {
		if err := osutil.CopyFile(cc, filepath.Join(ubuntuDataCloudCfgDir, filepath.Base(cc)), 0); err != nil {
			return err
		}
	}
	return nil
}

// TODO:UC20: - allow cloud.conf coming from the gadget
//            - think about if/what cloud-init means on "secured" models
func configureCloudInit(opts *Options) (err error) {
	if opts.TargetRootDir == "" {
		return fmt.Errorf("unable to configure cloud-init, missing target dir")
	}

	switch opts.CloudInitSrcDir {
	case "":
		// disable cloud-init by default using the writable dir
		err = DisableCloudInit(WritableDefaultsDir(opts.TargetRootDir))
	default:
		err = installCloudInitCfg(opts.CloudInitSrcDir, WritableDefaultsDir(opts.TargetRootDir))
	}
	return err
}

// CloudInitState represents the various cloud-init states
type CloudInitState int

var (
	// the (?m) is needed since cloud-init output will have newlines
	cloudInitStatusRe = regexp.MustCompile(`(?m)^status: (.*)$`)
	datasourceRe      = regexp.MustCompile(`DataSource([a-zA-Z0-9]+).*`)

	cloudInitSnapdRestrictFile = "/etc/cloud/cloud.cfg.d/zzzz_snapd.cfg"
	cloudInitDisabledFile      = "/etc/cloud/cloud-init.disabled"

	nocloudRestrictYaml = []byte(`datasource_list: [NoCloud]
datasource:
  NoCloud:
    fs_label: null`)

	genericCloudRestrictYamlPattern = `datasource_list: [%s]`
)

const (
	// CloudInitDisabledPermanently is when cloud-init is disabled as per the
	// cloud-init.disabled file.
	CloudInitDisabledPermanently CloudInitState = iota
	// CloudInitRestrictedBySnapd is when cloud-init has been restricted by
	// snapd with a specific config file.
	CloudInitRestrictedBySnapd
	// CloudInitUntriggered is when cloud-init is disabled because nothing has
	// triggered it to run, but it could still be run.
	CloudInitUntriggered
	// CloudInitDone is when cloud-init has been run on this boot.
	CloudInitDone
	// CloudInitEnabled is when cloud-init is active, but not necessarily
	// finished. This matches the "running" and "not run" states from cloud-init
	// as well as any other state that does not match any of the other defined
	// states, as we are conservative in assuming that cloud-init is doing
	// something.
	CloudInitEnabled
	// CloudInitErrored is when cloud-init tried to run, but failed or had invalid
	// configuration.
	CloudInitErrored
)

// CloudInitStatus returns the current status of cloud-init. Note that it will
// first check for static file-based statuses first through the snapd
// restriction file and the disabled file before consulting
// cloud-init directly through the status command.
// Also note that in unknown situations we are conservative in assuming that
// cloud-init may be doing something and will return CloudInitEnabled when we
// do not recognize the state returned by the cloud-init status command.
func CloudInitStatus() (CloudInitState, error) {
	// if cloud-init has been restricted by snapd, check that first
	snapdRestrictingFile := filepath.Join(dirs.GlobalRootDir, cloudInitSnapdRestrictFile)
	if osutil.FileExists(snapdRestrictingFile) {
		return CloudInitRestrictedBySnapd, nil
	}

	// if it was explicitly disabled via the cloud-init disable file, then
	// return special status for that
	disabledFile := filepath.Join(dirs.GlobalRootDir, cloudInitDisabledFile)
	if osutil.FileExists(disabledFile) {
		return CloudInitDisabledPermanently, nil
	}

	out, err := exec.Command("cloud-init", "status").CombinedOutput()
	if err != nil {
		return CloudInitErrored, osutil.OutputErr(out, err)
	}
	// output should just be "status: <state>"
	match := cloudInitStatusRe.FindSubmatch(out)
	if len(match) != 2 {
		return CloudInitErrored, fmt.Errorf("invalid cloud-init output: %v", osutil.OutputErr(out, err))
	}
	switch string(match[1]) {
	case "disabled":
		// here since we weren't disabled by the file, we are in "disabled but
		// could be enabled" state - arguably this should be a different state
		// than "disabled", see
		// https://bugs.launchpad.net/cloud-init/+bug/1883124 and
		// https://bugs.launchpad.net/cloud-init/+bug/1883122
		return CloudInitUntriggered, nil
	case "error":
		return CloudInitErrored, nil
	case "done":
		return CloudInitDone, nil
	// "running" and "not run" are considered Enabled, see doc-comment
	case "running", "not run":
		fallthrough
	default:
		// these states are all
		return CloudInitEnabled, nil
	}
}

// these structs are externally defined by cloud-init
type v1Data struct {
	DataSource string `json:"datasource"`
}

type cloudInitStatus struct {
	V1 v1Data `json:"v1"`
}

// CloudInitRestrictionResult is the result of calling RestrictCloudInit. The
// values for Action are "disable" or "restrict", and the Datasource will be set
// to the restricted datasource if Action is "restrict".
type CloudInitRestrictionResult struct {
	Action     string
	DataSource string
}

// CloudInitRestrictOptions are options for how to restrict cloud-init with
// RestrictCloudInit. ForceDisable will force disabling cloud-init even if it is
// in an active/running or errored state.
type CloudInitRestrictOptions struct {
	ForceDisable bool
}

// RestrictCloudInit will limit the operations of cloud-init on subsequent boots
// by either disabling cloud-init in the untriggered state, or restrict
// cloud-init to only use a specific datasource (additionally if the currently
// detected datasource for this boot was NoCloud, it will disable the automatic
// import of filesystems with labels such as CIDATA (or cidata) as datasources).
// This is expected to be run when cloud-init is in a "steady" state such as
// done or disabled (untriggered). If called in other states such as errored, it
// will return an error, but it can be forced to disable cloud-init anyways in
// these states with the opts parameter and the ForceDisable field.
// This function is meant to protect against CVE-2020-11933.
func RestrictCloudInit(state CloudInitState, opts *CloudInitRestrictOptions) (CloudInitRestrictionResult, error) {
	res := CloudInitRestrictionResult{}

	if opts == nil {
		opts = &CloudInitRestrictOptions{}
	}

	switch state {
	case CloudInitDone:
		// handled below
		break
	case CloudInitRestrictedBySnapd:
		return res, fmt.Errorf("cannot restrict cloud-init: already restricted")
	case CloudInitDisabledPermanently:
		return res, fmt.Errorf("cannot restrict cloud-init: already disabled")
	case CloudInitErrored, CloudInitEnabled:
		// if we are not forcing a disable, return error as these states are
		// where cloud-init could still be running doing things
		if !opts.ForceDisable {
			return res, fmt.Errorf("cannot restrict cloud-init in error or enabled state")
		}
		fallthrough
	case CloudInitUntriggered:
		fallthrough
	default:
		res.Action = "disable"
		return res, DisableCloudInit(dirs.GlobalRootDir)
	}

	// from here on out, we are taking the "restrict" action
	res.Action = "restrict"

	// first get the cloud-init data-source that was used from /
	resultsFile := filepath.Join(dirs.GlobalRootDir, "/run/cloud-init/status.json")

	f, err := os.Open(resultsFile)
	if err != nil {
		return res, err
	}
	defer f.Close()

	var stat cloudInitStatus
	err = json.NewDecoder(f).Decode(&stat)
	if err != nil {
		return res, err
	}

	// if the datasource was empty then cloud-init did something wrong or
	// perhaps it incorrectly reported that it ran but something else deleted
	// the file
	datasourceRaw := stat.V1.DataSource
	if datasourceRaw == "" {
		return res, fmt.Errorf("cloud-init error: missing datasource from status.json")
	}

	// for some datasources there is additional data in this item, i.e. for
	// NoCloud we will also see:
	// "DataSourceNoCloud [seed=/dev/sr0][dsmode=net]"
	// so hence we use a regexp to parse out just the name of the datasource
	datasourceMatches := datasourceRe.FindStringSubmatch(datasourceRaw)
	if len(datasourceMatches) != 2 {
		return res, fmt.Errorf("cloud-init error: unexpected datasource format %q", datasourceRaw)
	}
	res.DataSource = datasourceMatches[1]

	cloudInitRestrictFile := filepath.Join(dirs.GlobalRootDir, cloudInitSnapdRestrictFile)

	switch res.DataSource {
	case "NoCloud":
		// With the NoCloud datasource, we also need to restrict/disable the
		// import of arbitrary filesystem labels to use as datasources, i.e. a
		// USB drive inserted by an attacker with label CIDATA will defeat
		// security measures on Ubuntu Core, so with the additional fs_label
		// spec, we disable that import.
		err := ioutil.WriteFile(cloudInitRestrictFile, nocloudRestrictYaml, 0644)
		if err != nil {
			return res, err
		}
	default:
		// all other datasources that are not NoCloud will be restricted to only
		// allow this specific datasource to prevent an attack via NoCloud for
		// example
		yaml := []byte(fmt.Sprintf(genericCloudRestrictYamlPattern, res.DataSource))
		err := ioutil.WriteFile(cloudInitRestrictFile, yaml, 0644)
		if err != nil {
			return res, err
		}
	}

	return res, nil
}
