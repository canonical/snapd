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
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

// HasGadgetCloudConf takes a gadget directory and returns whether there is
// cloud-init config in the form of a cloud.conf file in the gadget.
func HasGadgetCloudConf(gadgetDir string) bool {
	return osutil.FileExists(filepath.Join(gadgetDir, "cloud.conf"))
}

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
	if err := os.WriteFile(filepath.Join(ubuntuDataCloud, "cloud-init.disabled"), nil, 0644); err != nil {
		return fmt.Errorf("cannot disable cloud-init: %v", err)
	}

	return nil
}

// supportedFilteredCloudConfig is a struct of the supported values for
// cloud-init configuration file.
type supportedFilteredCloudConfig struct {
	Datasource map[string]supportedFilteredDatasource `yaml:"datasource,omitempty"`
	Network    map[string]interface{}                 `yaml:"network,omitempty"`
	// DatasourceList is a pointer so we can distinguish between:
	// datasource_list: []
	// and not setting the datasource at all
	// for example there might be gadgets which don't want to use any
	// datasources, but still wants to set some networking config
	DatasourceList *[]string                             `yaml:"datasource_list,omitempty"`
	Reporting      map[string]supportedFilteredReporting `yaml:"reporting,omitempty"`
}

type supportedFilteredDatasource struct {
	// these are for MAAS
	ConsumerKey string `yaml:"consumer_key,omitempty"`
	MetadataURL string `yaml:"metadata_url,omitempty"`
	TokenKey    string `yaml:"token_key,omitempty"`
	TokenSecret string `yaml:"token_secret,omitempty"`
}

type supportedFilteredReporting struct {
	Type        string `yaml:"type,omitempty"`
	Endpoint    string `yaml:"endpoint,omitempty"`
	ConsumerKey string `yaml:"consumer_key,omitempty"`
	TokenKey    string `yaml:"token_key,omitempty"`
	TokenSecret string `yaml:"token_secret,omitempty"`
}

// supportedFilteredDatasources is the set of datasources we support filtering
// cloud-init config for. It is expected that this list grows as we support for
// more clouds.
var supportedFilteredDatasources = []string{
	"MAAS",
}

// filterCloudCfg filters a cloud-init configuration struct parsed from a single
// cloud-init configuration file. The config provided here may be a subset of
// the full cloud-init configuration from the file in that there may be
// top-level keys in the YAML file that we did not parse and as such they are
// dropped and filtered automatically. For other keys, we must parse part of the
// configuration struct and remove nested keys while keeping other parts of the
// same section.
func filterCloudCfg(cfg *supportedFilteredCloudConfig, allowedDatasources []string) error {
	// TODO: should we track modifications / filters applied to log/notify about
	//       what is dropped / not supported?

	// first filter out the disallowed datasources
	for dsName := range cfg.Datasource {
		// remove unsupported or unrecognized datasources
		if !strutil.ListContains(allowedDatasources, strings.ToUpper(dsName)) {
			delete(cfg.Datasource, dsName)
			continue
		}
	}

	// next handle the datasource list setting, if it was not empty, reset it to
	// the allowedDatasources we were provided
	if cfg.DatasourceList != nil {
		deepCpy := make([]string, 0, len(allowedDatasources))
		deepCpy = append(deepCpy, allowedDatasources...)
		cfg.DatasourceList = &deepCpy
	}

	// next handle the reporting setting
	for dsName := range cfg.Reporting {
		// remove unsupported or unrecognized datasources
		if !strutil.ListContains(allowedDatasources, strings.ToUpper(dsName)) {
			delete(cfg.Reporting, dsName)
			continue
		}
	}

	return nil
}

// filterCloudCfgFile takes a cloud config file as input and filters out unknown
// and unsupported keys from the config, returning a new file. It also will
// filter out configuration that is specific to a datasource if that datasource
// is not specified in the allowedDatasources argument. The empty string will be
// returned if the input file was entirely filtered out and there is nothing
// left.
func filterCloudCfgFile(in string, allowedDatasources []string) (string, error) {
	// we don't allow any files to be installed/filtered from ubuntu-seed if
	// there are no datasources at all
	if len(allowedDatasources) == 0 {
		return "", nil
	}

	// otherwise if there are datasources that are allowed, then we perform
	// filtering on the file
	// note that this logic means that "generic" cloud-init config which is not
	// specific to a datasource will not get installed unless either:
	// * there is another file specifying a datasource that intersects with the
	//   set of datasources mentioned in the gadget and intersects with what we
	//   support
	// * there are no datasources mentioned in the gadget and there are other
	//   cloud-init files on ubuntu-seed which specify a datasource and
	//   intersect with what we support

	dstFileName := filepath.Base(in)
	filteredFile, err := ioutil.TempFile("", dstFileName)
	if err != nil {
		return "", err
	}
	defer filteredFile.Close()

	// open the source and unmarshal it as yaml
	unfilteredFileBytes, err := ioutil.ReadFile(in)
	if err != nil {
		return "", err
	}

	var cfg supportedFilteredCloudConfig
	if err := yaml.Unmarshal(unfilteredFileBytes, &cfg); err != nil {
		return "", err
	}

	if err := filterCloudCfg(&cfg, allowedDatasources); err != nil {
		return "", err
	}

	// write out cfg to the filtered file now
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}

	// check if we need to write a file at all, if the yaml serialization was
	// entirely filtered out, then we don't need to write anything
	if strings.TrimSpace(string(b)) == "{}" {
		return "", nil
	}

	// add the #cloud-config prefix to all files we write
	if _, err := filteredFile.Write([]byte("#cloud-config\n")); err != nil {
		return "", err
	}

	if _, err := filteredFile.Write(b); err != nil {
		return "", err
	}

	// use the newly filtered temp file as the source to copy
	return filteredFile.Name(), nil
}

type cloudDatasourcesInUseResult struct {
	// ExplicitlyAllowed is the value of datasource_list. If this is empty,
	// consult ExplicitlyNoneAllowed to tell if it was specified as empty in the
	// config or if it was just absent from the config
	ExplicitlyAllowed []string
	// ExplicitlyNoneAllowed is true when datasource_list was set to
	// specifically the empty list, thus disallowing use of any datasource
	ExplicitlyNoneAllowed bool
	// Mentioned is the full set of datasources mentioned in the yaml config,
	// both sources from ExplicitlyAllowed and from implicitly mentioned in the
	// config.
	Mentioned []string
}

// cloudDatasourcesInUse returns the datasources in use by the specified config
// file. All datasource names are made upper case to be comparable. This is an
// arbitrary choice between making them upper case or making them lower case,
// but cloud-init treats "maas" the same as "MAAS", so we need to treat them the
// same too.
func cloudDatasourcesInUse(configFile string) (*cloudDatasourcesInUseResult, error) {
	// TODO: are there other keys in addition to those that we support in
	// filtering that might mention datasources ?

	b, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var cfg supportedFilteredCloudConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}

	res := &cloudDatasourcesInUseResult{}

	sourcesMentionedInCfg := map[string]bool{}

	// datasource key is a map with the datasource name as a key
	for ds := range cfg.Datasource {
		sourcesMentionedInCfg[strings.ToUpper(ds)] = true
	}

	// same for reporting
	for ds := range cfg.Reporting {
		sourcesMentionedInCfg[strings.ToUpper(ds)] = true
	}

	// we can also have datasources mentioned in the datasource list config
	if cfg.DatasourceList != nil {
		if len(*cfg.DatasourceList) == 0 {
			res.ExplicitlyNoneAllowed = true
		} else {
			explicitlyAllowed := map[string]bool{}
			for _, ds := range *cfg.DatasourceList {
				dsName := strings.ToUpper(ds)
				sourcesMentionedInCfg[dsName] = true
				explicitlyAllowed[dsName] = true
			}
			res.ExplicitlyAllowed = make([]string, 0, len(explicitlyAllowed))
			for ds := range explicitlyAllowed {
				res.ExplicitlyAllowed = append(res.ExplicitlyAllowed, ds)
			}
			sort.Strings(res.ExplicitlyAllowed)
		}
	}

	for ds := range sourcesMentionedInCfg {
		res.Mentioned = append(res.Mentioned, strings.ToUpper(ds))
	}
	sort.Strings(res.Mentioned)

	return res, nil
}

// cloudDatasourcesInUseForDir considers all files in a directory as individual
// cloud-init config files, and analyzes all datasources in use for each file
// and returns their union. It does not distinguish between mentioned,
// explicitly allowed, or explicitly disallowed, but it does follow cloud-init's
// logic for determining the overwriting of properties. So, for example, if a
// file sets datasource_list: [] and no other file processed later (files are
// processed in lexical order) sets this property to another value, it will be
// treated as if the config explicitly disallows no datasources. If, on the
// other hand, a file processed later sets datasource_list: [foo], then foo is
// used instead and the explicit disallowing is ignored/overwritten.
func cloudDatasourcesInUseForDir(dir string) (*cloudDatasourcesInUseResult, error) {
	// cloud-init only considers files with file extension .cfg so we do too.
	files, err := filepath.Glob(filepath.Join(dir, "*.cfg"))
	if err != nil {
		return nil, err
	}

	// sort the filenames so they are in lexographical order - this is the same
	// order that cloud-init processes them
	sort.Strings(files)

	res := &cloudDatasourcesInUseResult{}

	resMentionedMap := map[string]bool{}

	for _, f := range files {
		fRes, err := cloudDatasourcesInUse(f)
		// TODO: or should we fail on broken individual files? probably?
		if err != nil {
			logger.Noticef("error analyzing cloud-init datasources in use for file %s: %v", f, err)
			continue
		}

		// if we have an explicit setting for what is allowed, then that always
		// overwrites previous settings of ExplicitlyAllowed
		if len(fRes.ExplicitlyAllowed) != 0 {
			res.ExplicitlyNoneAllowed = false
			res.ExplicitlyAllowed = fRes.ExplicitlyAllowed
		} else if fRes.ExplicitlyNoneAllowed {
			// if we are now explicitly disallowing datasources, then overwrite that
			// setting - this is mutually exclusive with ExplicitlyAllowed
			// having a non-zero length
			res.ExplicitlyNoneAllowed = true
			res.ExplicitlyAllowed = nil
		}

		// we always keep track of the mentioned datasources, it's not an issue
		// to mention datasources and also have datasources disallowed, the
		// higher level logic is expected to handle this properly
		for _, ds := range fRes.Mentioned {
			if !resMentionedMap[ds] {
				res.Mentioned = append(res.Mentioned, ds)
				resMentionedMap[ds] = true
			}
		}
	}

	sort.Strings(res.Mentioned)
	sort.Strings(res.ExplicitlyAllowed)

	return res, nil
}

type cloudInitConfigInstallOptions struct {
	// Prefix is the prefix to add to files when installing them.
	Prefix string
	// Filter is whether to filter the config files when installing them.
	Filter bool
	// AllowedDatasources is the set of datasources to allow config that is
	// specific to a datasource in when filtering. An empty list and setting
	// Filter to false is equivalent to allowing any datasource to be installed,
	// while an empty list and setting Filter to true means that no config that
	// is specific to a datasource should be installed, but config that is not
	// specific to a datasource (such as networking config) is allowed to be
	// installed.
	AllowedDatasources []string
}

// installCloudInitCfgDir installs glob cfg files from the source directory to
// the cloud config dir, optionally filtering the files for safe and supported
// keys in the configuration before installing them.
func installCloudInitCfgDir(src, targetdir string, opts *cloudInitConfigInstallOptions) (installedFiles []string, err error) {
	if opts == nil {
		opts = &cloudInitConfigInstallOptions{}
	}

	// TODO:UC20: enforce patterns on the glob files and their suffix ranges
	ccl, err := filepath.Glob(filepath.Join(src, "*.cfg"))
	if err != nil {
		return nil, err
	}
	if len(ccl) == 0 {
		return nil, nil
	}

	ubuntuDataCloudCfgDir := filepath.Join(ubuntuDataCloudDir(targetdir), "cloud.cfg.d/")
	if err := os.MkdirAll(ubuntuDataCloudCfgDir, 0755); err != nil {
		return nil, fmt.Errorf("cannot make cloud config dir: %v", err)
	}

	for _, cc := range ccl {
		src := cc
		baseName := filepath.Base(cc)
		dst := filepath.Join(ubuntuDataCloudCfgDir, opts.Prefix+baseName)

		if opts.Filter {
			filteredFile, err := filterCloudCfgFile(cc, opts.AllowedDatasources)
			if err != nil {
				return nil, fmt.Errorf("error while filtering cloud-config file %s: %v", baseName, err)
			}
			src = filteredFile
		}

		// src may be the empty string if we were copying a file that got
		// entirely emptied, in which case we shouldn't copy anything since
		// there's nothing to install from this config file
		if src == "" {
			logger.Noticef("cloud-init config file %s was filtered out", baseName)
			continue
		}

		if err := osutil.CopyFile(src, dst, 0); err != nil {
			return nil, err
		}

		// make sure that the new file is world readable, since cloud-init does
		// not run as root (somehow?)
		if err := os.Chmod(dst, 0644); err != nil {
			return nil, err
		}

		installedFiles = append(installedFiles, dst)
	}

	return installedFiles, nil
}

// installGadgetCloudInitCfg installs a single cloud-init config file from the
// gadget snap to the /etc/cloud config dir as "80_device_gadget.cfg". It also
// parses and returns what datasources are detected to be in use for the gadget
// cloud-config.
func installGadgetCloudInitCfg(src, targetdir string) (*cloudDatasourcesInUseResult, error) {
	ubuntuDataCloudCfgDir := filepath.Join(ubuntuDataCloudDir(targetdir), "cloud.cfg.d/")
	if err := os.MkdirAll(ubuntuDataCloudCfgDir, 0755); err != nil {
		return nil, fmt.Errorf("cannot make cloud config dir: %v", err)
	}

	datasourcesRes, err := cloudDatasourcesInUse(src)
	if err != nil {
		return nil, err
	}

	configFile := filepath.Join(ubuntuDataCloudCfgDir, "80_device_gadget.cfg")
	if err := osutil.CopyFile(src, configFile, 0); err != nil {
		return nil, err
	}
	return datasourcesRes, nil
}

func configureCloudInit(model *asserts.Model, opts *Options) (err error) {
	if opts.TargetRootDir == "" {
		return fmt.Errorf("unable to configure cloud-init, missing target dir")
	}

	// first check if cloud-init should be disallowed entirely
	if !opts.AllowCloudInit {
		return DisableCloudInit(WritableDefaultsDir(opts.TargetRootDir))
	}

	// otherwise cloud-init is allowed to run, we need to decide where to
	// permit configuration to come from, if opts.CloudInitSrcDir is non-empty
	// there is at least a cloud-config dir on ubuntu-seed we could install
	// config from

	// check if we should filter cloud-init config on ubuntu-seed, we do this
	// for grade signed only (we don't allow any config for grade secured, and we
	// allow any config on grade dangerous)

	grade := model.Grade()

	gadgetDatasourcesRes := &cloudDatasourcesInUseResult{}

	// we always allow gadget cloud config, so install that first
	if HasGadgetCloudConf(opts.GadgetDir) {
		// then copy / install the gadget config first
		gadgetCloudConf := filepath.Join(opts.GadgetDir, "cloud.conf")

		datasourcesRes, err := installGadgetCloudInitCfg(gadgetCloudConf, WritableDefaultsDir(opts.TargetRootDir))
		if err != nil {
			return err
		}

		gadgetDatasourcesRes = datasourcesRes

		// we don't return here to enable also copying any cloud-init config
		// from ubuntu-seed in order for both to be used simultaneously for
		// example on test devices where the gadget has a gadget.yaml, but for
		// testing purposes you also want to provision another user with
		// ubuntu-seed cloud-init config
	}

	// after installing gadget config, check if we have to consider ubuntu-seed
	// at all, if a source dir wasn't provided to us we can just exit early
	// here, note that it's valid to allow cloud-init, but not set
	// CloudInitSrcDir and not have a gadget cloud.conf, in this case cloud-init
	// may pick up dynamic metadata and userdata from NoCloud sources such as a
	// USB or CD-ROM drive with label CIDATA, etc. during first-boot
	if opts.CloudInitSrcDir == "" {
		return nil
	}

	// otherwise there is most likely something on ubuntu-seed

	installOpts := &cloudInitConfigInstallOptions{
		// set the prefix such that any ubuntu-seed config that ends up getting
		// installed takes precedence over the gadget config
		Prefix: "90_",
	}

	switch grade {
	case asserts.ModelSecured:
		// for secured we are done, we only allow gadget cloud-config on secured
		return nil
	case asserts.ModelSigned:
		// for grade signed, we filter config coming from ubuntu-seed
		installOpts.Filter = true

		// in order to decide what to allow through the filter, we need to
		// consider the whole set of config files on ubuntu-seed as a single
		// bundle of files and determine the datasource(s) in use there, and
		// compare this with the datasource(s) we support through the gadget and
		// in supportedFilteredDatasources

		ubuntuSeedDatasourceRes, err := cloudDatasourcesInUseForDir(opts.CloudInitSrcDir)
		if err != nil {
			return err
		}

		// handle the various permutations for the datasources mentioned in the
		// gadget
		switch {
		case gadgetDatasourcesRes.ExplicitlyNoneAllowed:
			// no datasources were allowed, so set it to the empty list to
			// disallow anything being installed
			installOpts.AllowedDatasources = nil

		// consider the case where the gadget explicitly allows specific
		// datasources before considering any of the implicit mentions

		case len(gadgetDatasourcesRes.ExplicitlyAllowed) != 0:
			// allow the intersection of what the gadget explicitly allows, what
			// ubuntu-seed either explicitly allows (or what it mentions), and
			// what we statically support

			if len(ubuntuSeedDatasourceRes.ExplicitlyAllowed) != 0 {
				// use ubuntu-seed explicitly allowed in the intersection computation
				installOpts.AllowedDatasources = strutil.Intersection(
					supportedFilteredDatasources,
					ubuntuSeedDatasourceRes.ExplicitlyAllowed,
					gadgetDatasourcesRes.ExplicitlyAllowed,
				)
			} else if len(ubuntuSeedDatasourceRes.Mentioned) != 0 && !ubuntuSeedDatasourceRes.ExplicitlyNoneAllowed {
				// use ubuntu-seed mentioned in the intersection computation
				installOpts.AllowedDatasources = strutil.Intersection(
					supportedFilteredDatasources,
					ubuntuSeedDatasourceRes.Mentioned,
					gadgetDatasourcesRes.ExplicitlyAllowed,
				)
			} else {
				// then the ubuntu-seed datasources didn't either mention any
				// datasources, or it explicitly disallowed any datasources (
				// which would be weird to have config on ubuntu-seed which says
				// "please ignore this other config on ubuntu-seed")
				// but in any case we know a priori that the intersection will
				// be empty
				installOpts.AllowedDatasources = nil
			}

		case len(gadgetDatasourcesRes.Mentioned) != 0:
			// allow the intersection of what the gadget mentions, what
			// ubuntu-seed either explicitly allows (or what it mentions), and
			// what we statically support

			if len(ubuntuSeedDatasourceRes.ExplicitlyAllowed) != 0 {
				// use ubuntu-seed explicitly allowed in the intersection computation
				installOpts.AllowedDatasources = strutil.Intersection(
					supportedFilteredDatasources,
					ubuntuSeedDatasourceRes.ExplicitlyAllowed,
					gadgetDatasourcesRes.Mentioned,
				)
			} else if len(ubuntuSeedDatasourceRes.Mentioned) != 0 && !ubuntuSeedDatasourceRes.ExplicitlyNoneAllowed {
				// use ubuntu-seed mentioned in the intersection computation
				installOpts.AllowedDatasources = strutil.Intersection(
					supportedFilteredDatasources,
					ubuntuSeedDatasourceRes.Mentioned,
					gadgetDatasourcesRes.Mentioned,
				)
			} else {
				// then the ubuntu-seed datasources didn't either mention any
				// datasources, or it explicitly disallowed any datasources (
				// which would be weird to have config on ubuntu-seed which says
				// "please ignore this other config on ubuntu-seed")
				// but in any case we know a priori that the intersection will
				// be empty
				installOpts.AllowedDatasources = nil
			}

		default:
			// gadget had no opinion on the datasources used, so we allow the
			// intersection of what ubuntu-seed explicitly allowed (or
			// mentioned) with what we statically allow
			if len(ubuntuSeedDatasourceRes.ExplicitlyAllowed) != 0 {
				// use ubuntu-seed explicitly allowed in the intersection computation
				installOpts.AllowedDatasources = strutil.Intersection(
					supportedFilteredDatasources,
					ubuntuSeedDatasourceRes.ExplicitlyAllowed,
				)
			} else if len(ubuntuSeedDatasourceRes.Mentioned) != 0 && !ubuntuSeedDatasourceRes.ExplicitlyNoneAllowed {
				// use ubuntu-seed mentioned in the intersection computation
				installOpts.AllowedDatasources = strutil.Intersection(
					supportedFilteredDatasources,
					ubuntuSeedDatasourceRes.Mentioned,
				)
			} else {
				// then the ubuntu-seed datasources didn't either mention any
				// datasources, or it explicitly disallowed any datasources (
				// which would be weird to have config on ubuntu-seed which says
				// "please ignore this other config on ubuntu-seed")
				// but in any case we know a priori that the intersection will
				// be empty
				installOpts.AllowedDatasources = nil
			}
		}

	case asserts.ModelDangerous:
		// for grade dangerous we just install all the config from ubuntu-seed
		installOpts.Filter = false
	default:
		return fmt.Errorf("internal error: unknown model assertion grade %s", grade)
	}

	// check if we will actually be able to install anything
	if installOpts.Filter && len(installOpts.AllowedDatasources) == 0 {
		return nil
	}

	// try installing the files, this is the case either where we are filtering
	// and there are some files that will be filtered, or where we are not
	// filtering and thus don't know anything about what files we might install,
	// but we will install them all because we are in grade dangerous
	installedFiles, err := installCloudInitCfgDir(opts.CloudInitSrcDir, WritableDefaultsDir(opts.TargetRootDir), installOpts)
	if err != nil {
		return err
	}

	if installOpts.Filter && len(installedFiles) != 0 {
		// we are filtering files and we installed some, so we also need to
		// install a datasource restriction file at the end just as a paranoia
		// measure
		yaml := []byte(fmt.Sprintf(genericCloudRestrictYamlPattern, strings.Join(installOpts.AllowedDatasources, ",")))
		restrictFile := filepath.Join(ubuntuDataCloudDir(WritableDefaultsDir(opts.TargetRootDir)), "cloud.cfg.d/99_snapd_datasource.cfg")
		return os.WriteFile(restrictFile, yaml, 0644)
	}

	return nil
}

// CloudInitState represents the various cloud-init states
type CloudInitState int

var (
	// the (?m) is needed since cloud-init output will have newlines
	cloudInitStatusRe = regexp.MustCompile(`(?m)^status: (.*)$`)
	datasourceRe      = regexp.MustCompile(`DataSource([a-zA-Z0-9]+).*`)

	cloudInitSnapdRestrictFile = "/etc/cloud/cloud.cfg.d/zzzz_snapd.cfg"
	cloudInitDisabledFile      = "/etc/cloud/cloud-init.disabled"

	// for NoCloud datasource, we need to specify "manual_cache_clean: true"
	// because the default is false, and this key being true essentially informs
	// cloud-init that it should always trust the instance-id it has cached in
	// the image, and shouldn't assume that there is a new one on every boot, as
	// otherwise we have bugs like https://bugs.launchpad.net/snapd/+bug/1905983
	// where subsequent boots after cloud-init runs and gets restricted it will
	// try to detect the instance_id by reading from the NoCloud datasource
	// fs_label, but we set that to "null" so it fails to read anything and thus
	// can't detect the effective instance_id and assumes it is different and
	// applies default config which can overwrite valid config from the initial
	// boot if that is not the default config
	// see also https://cloudinit.readthedocs.io/en/latest/topics/boot.html?highlight=manual_cache_clean#first-boot-determination
	nocloudRestrictYaml = []byte(`datasource_list: [NoCloud]
datasource:
  NoCloud:
    fs_label: null
manual_cache_clean: true
`)

	// don't use manual_cache_clean for real cloud datasources, the setting is
	// used with ubuntu core only for sources where we can only get the
	// instance_id through the fs_label for NoCloud and None (since we disable
	// importing using the fs_label after the initial run).
	genericCloudRestrictYamlPattern = `datasource_list: [%s]
`

	localDatasources = []string{"NoCloud", "None"}
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
	// CloudInitNotFound is when there is no cloud-init executable on the
	// device.
	CloudInitNotFound
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

	ciBinary, err := exec.LookPath("cloud-init")
	if err != nil {
		logger.Noticef("cannot locate cloud-init executable: %v", err)
		return CloudInitNotFound, nil
	}

	out, stderr, err := osutil.RunSplitOutput(ciBinary, "status")

	// in the case where cloud-init is actually in an error condition, like
	// where MAAS is the datasource but there is no MAAS server for example,
	// then cloud-init will exit with status 1 and output `status: error`
	// we want to handle that case specially below by returning non-nil error,
	// but also CloudInitErrored, so first inspect the output to see if it
	// matches
	// output should just be "status: <state>"
	match := cloudInitStatusRe.FindSubmatch(out)
	if len(match) != 2 {
		// check if running the command had an error, if it did then return that
		if err != nil {
			return CloudInitErrored, osutil.OutputErrCombine(out, stderr, err)
		}
		// otherwise we had some sort of malformed output
		return CloudInitErrored, fmt.Errorf("invalid cloud-init output: %v", osutil.OutputErrCombine(out, stderr, err))
	}

	// otherwise we had a successful match, but we need to check if the status
	// command errored itself
	if err != nil {
		if string(match[1]) == "error" {
			// then the status was indeed error and we should treat this as the
			// "positively identified" error case
			return CloudInitErrored, nil
		}
		// otherwise just ignore the parsing of the output and just return the
		// error normally
		return CloudInitErrored, fmt.Errorf("cloud-init errored: %v", osutil.OutputErrCombine(out, stderr, err))
	}

	// otherwise no error from cloud-init

	switch string(match[1]) {
	case "disabled":
		// here since we weren't disabled by the file, we are in "disabled but
		// could be enabled" state - arguably this should be a different state
		// than "disabled", see
		// https://bugs.launchpad.net/cloud-init/+bug/1883124 and
		// https://bugs.launchpad.net/cloud-init/+bug/1883122
		return CloudInitUntriggered, nil
	case "error":
		// this shouldn't happen in practice, but handle it here anyways in case
		// cloud-init ever changes it's mind and starts reporting error state
		// with a 0 exit code
		return CloudInitErrored, nil
	case "done":
		return CloudInitDone, nil
	// "running" and "not run" are considered Enabled, see doc-comment
	case "running", "not run":
		fallthrough
	default:
		// these states are all the generic "enabled" state
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
// RestrictCloudInit.
type CloudInitRestrictOptions struct {
	// ForceDisable will force disabling cloud-init even if it is
	// in an active/running or errored state.
	ForceDisable bool

	// DisableAfterLocalDatasourcesRun modifies RestrictCloudInit to disable
	// cloud-init after it has run on first-boot if the datasource detected is
	// a local source such as NoCloud or None. If the datasource detected is not
	// a local source, such as GCE or AWS EC2 it is merely restricted as
	// described in the doc-comment on RestrictCloudInit.
	DisableAfterLocalDatasourcesRun bool
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
	case CloudInitUntriggered, CloudInitNotFound:
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

	switch {
	case opts.DisableAfterLocalDatasourcesRun && strutil.ListContains(localDatasources, res.DataSource):
		// On UC20, DisableAfterLocalDatasourcesRun will be set, where we want
		// to disable local sources like NoCloud and None after first-boot
		// instead of just restricting them like we do below for UC16 and UC18.

		// as such, change the action taken to disable and disable cloud-init
		res.Action = "disable"
		err = DisableCloudInit(dirs.GlobalRootDir)
	case res.DataSource == "NoCloud":
		// With the NoCloud datasource (which is one of the local datasources),
		// we also need to restrict/disable the import of arbitrary filesystem
		// labels to use as datasources, i.e. a USB drive inserted by an
		// attacker with label CIDATA will defeat security measures on Ubuntu
		// Core, so with the additional fs_label spec, we disable that import.
		err = os.WriteFile(cloudInitRestrictFile, nocloudRestrictYaml, 0644)
	default:
		// all other cases are either not local on UC20, or not NoCloud and as
		// such we simply restrict cloud-init to the specific datasource used so
		// that an attack via NoCloud is protected against
		yaml := []byte(fmt.Sprintf(genericCloudRestrictYamlPattern, res.DataSource))
		err = os.WriteFile(cloudInitRestrictFile, yaml, 0644)
	}

	return res, err
}
