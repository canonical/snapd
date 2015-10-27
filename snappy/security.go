// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v2"

	"launchpad.net/snappy/dirs"
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/pkg"
	"launchpad.net/snappy/policy"
)

type errPolicyNotFound struct {
	pol_type string
	pol string
}
func (e *errPolicyNotFound) Error() string {
	return fmt.Sprintf("could not find specified %s: %s", e.pol_type, e.pol)
}

var (
	// Generated policy
	aaProfilesDir = filepath.Join(policy.SecBase, "apparmor/profiles")
	scProfilesDir = filepath.Join(policy.SecBase, "seccomp/profiles")
	// Templates and policy groups (caps)
	aaPolicyDir = "/usr/share/apparmor/easyprof"
	scPolicyDir = "/usr/share/seccomp"
	// Framework policy
	aaFrameworkPolicyDir = filepath.Join(policy.SecBase, "apparmor")
	scFrameworkPolicyDir = filepath.Join(policy.SecBase, "seccomp")

	errOriginNotFound   = errors.New("could not detect origin")
	errPolicyTypeNotFound = errors.New("could not find specified policy type")
	errInvalidAppID     = errors.New("invalid APP_ID")
	errPolicyGen        = errors.New("errors found when generating policy")
)

type apparmorJSONTemplate struct {
	Template      string   `json:"template"`
	PolicyGroups  []string `json:"policy_groups"`
	PolicyVendor  string   `json:"policy_vendor"`
	PolicyVersion float64  `json:"policy_version"`
}

type securitySeccompOverride struct {
	Template      string   `yaml:"security-template,omitempty"`
	PolicyGroups  []string `yaml:"caps,omitempty"`
	Syscalls      []string `yaml:"syscalls,omitempty"`
	PolicyVendor  string   `yaml:"policy-vendor"`
	PolicyVersion float64  `yaml:"policy-version"`
}

type securityAppID struct {
	AppID   string
	Pkgname string
	Appname string
	Version string
}

const defaultTemplate = "default"

var defaultPolicyGroups = []string{"network-client"}

// TODO: autodetect, this won't work for personal
const defaultPolicyVendor = "ubuntu-core"
const defaultPolicyVersion = 15.04

// Generate a string suitable for use in a DBus object
func dbusPath(s string) (string) {
	dbus_s := ""
	ok := regexp.MustCompile(`^[a-zA-Z0-9]$`)

	for _, c := range strings.Split(s, "") {
		if ok.MatchString(c) {
			dbus_s += c
		} else {
			dbus_s += fmt.Sprintf("_%02x", c)
		}
	}

	return dbus_s
}

func (s *SecurityDefinitions) generateApparmorJSONContent() ([]byte, error) {
	t := apparmorJSONTemplate{
		Template:      s.SecurityTemplate,
		PolicyGroups:  s.SecurityCaps,
		PolicyVendor:  defaultPolicyVendor,
		PolicyVersion: defaultPolicyVersion,
	}

	// FIXME: this is snappy specific, on other systems like the
	//        phone we may want different defaults.
	if t.Template == "" && t.PolicyGroups == nil {
		t.PolicyGroups = defaultPolicyGroups
	}

	// never write a null value out into the json
	if t.PolicyGroups == nil {
		t.PolicyGroups = []string{}
	}

	if t.Template == "" {
		t.Template = defaultTemplate
	}

	outStr, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, err
	}

	return outStr, nil
}

func handleApparmor(buildDir string, m *packageYaml, hookName string, s *SecurityDefinitions) error {
	hasSecPol := s.SecurityPolicy != nil && s.SecurityPolicy.Apparmor != ""
	hasSecOvr := s.SecurityOverride != nil && s.SecurityOverride.Apparmor != ""

	if hasSecPol || hasSecOvr {
		return nil
	}

	// generate apparmor template
	apparmorJSONFile := m.Integration[hookName]["apparmor"]
	securityJSONContent, err := s.generateApparmorJSONContent()
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(buildDir, apparmorJSONFile), securityJSONContent, 0644); err != nil {
		return err
	}

	return nil
}

func getSecurityProfile(m *packageYaml, appName, baseDir string) (string, error) {
	cleanedName := strings.Replace(appName, "/", "-", -1)
	if m.Type == pkg.TypeFramework || m.Type == pkg.TypeOem {
		return fmt.Sprintf("%s_%s_%s", m.Name, cleanedName, m.Version), nil
	}

	origin, err := originFromYamlPath(filepath.Join(baseDir, "meta", "package.yaml"))

	return fmt.Sprintf("%s.%s_%s_%s", m.Name, origin, cleanedName, m.Version), err
}

var runScFilterGen = runScFilterGenImpl

func runScFilterGenImpl(argv ...string) ([]byte, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	return cmd.Output()
}

// seccomp specific
func generateSeccompPolicy(baseDir, appName string, sd SecurityDefinitions) ([]byte, error) {
	if sd.SecurityPolicy != nil && sd.SecurityPolicy.Seccomp != "" {
		fn := filepath.Join(baseDir, sd.SecurityPolicy.Seccomp)
		content, err := ioutil.ReadFile(fn)
		if err != nil {
			logger.Noticef("Failed to read %q: %v", fn, err)
		}
		return content, err
	}

	os.MkdirAll(dirs.SnapSeccompDir, 0755)

	// defaults
	policyVendor := defaultPolicyVendor
	policyVersion := defaultPolicyVersion
	template := defaultTemplate
	caps := []string{}
	for _, p := range defaultPolicyGroups {
		caps = append(caps, p)
	}
	syscalls := []string{}

	if sd.SecurityOverride != nil {
		if sd.SecurityOverride.Seccomp == "" {
			logger.Noticef("No seccomp policy found")
			return nil, ErrNoSeccompPolicy
		}

		fn := filepath.Join(baseDir, sd.SecurityOverride.Seccomp)
		var s securitySeccompOverride
		err := readSeccompOverride(fn, &s)
		if err != nil {
			logger.Noticef("Failed to read %q: %v", fn, err)
			return nil, err
		}

		if s.Template != "" {
			template = s.Template
		}
		if s.PolicyVendor != "" {
			policyVendor = s.PolicyVendor
		}
		if s.PolicyVersion != 0 {
			policyVersion = s.PolicyVersion
		}
		caps = s.PolicyGroups
		syscalls = s.Syscalls
	} else {
		if sd.SecurityTemplate != "" {
			template = sd.SecurityTemplate
		}
		if sd.SecurityCaps != nil {
			caps = sd.SecurityCaps
		}
	}

	// Build up the command line
	args := []string{
		"sc-filtergen",
		fmt.Sprintf("--include-policy-dir=%s", filepath.Dir(dirs.SnapSeccompDir)),
		fmt.Sprintf("--policy-vendor=%s", policyVendor),
		fmt.Sprintf("--policy-version=%.2f", policyVersion),
		fmt.Sprintf("--template=%s", template),
	}
	if len(caps) > 0 {
		args = append(args, fmt.Sprintf("--policy-groups=%s", strings.Join(caps, ",")))
	}
	if len(syscalls) > 0 {
		args = append(args, fmt.Sprintf("--syscalls=%s", strings.Join(syscalls, ",")))
	}

	content, err := runScFilterGen(args...)
	if err != nil {
		logger.Noticef("%v failed", args)
	}

	return content, err
}

func readSeccompOverride(yamlPath string, s *securitySeccompOverride) error {
	yamlData, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(yamlData, &s)
	if err != nil {
		return &ErrInvalidYaml{File: "package.yaml[seccomp override]", Err: err, Yaml: yamlData}
	}
	// These must always be specified together
	if (s.PolicyVersion == 0 && s.PolicyVendor != "") || (s.PolicyVersion != 0 && s.PolicyVendor == "") {
		return ErrInvalidSeccompPolicy
	}

	return nil
}

func findTemplate(template string, policyType string) (string, error) {
	if template == "" {
		template = defaultTemplate
	}

	system_template := ""
	fw_template := ""
	subdir := fmt.Sprintf("templates/%s/%0.2f", defaultPolicyVendor, defaultPolicyVersion)
	if policyType == "apparmor" {
		system_template = filepath.Join(aaPolicyDir, subdir, template)
		fw_template = filepath.Join(aaFrameworkPolicyDir, "templates", template)
	} else if policyType == "seccomp" {
		system_template = filepath.Join(scPolicyDir, subdir, template)
		fw_template = filepath.Join(scFrameworkPolicyDir, "templates", template)
	} else {
		return "", errPolicyTypeNotFound
	}

	// Always prefer system policy
	found := false
	fns := []string{system_template, fw_template}
	var t bytes.Buffer
	for _, fn := range fns {
		tmp, err := ioutil.ReadFile(fn)
		if err == nil {
			t.Write(tmp)
			found = true
			break
		}
	}

	if found == false {
		return "", &errPolicyNotFound{"template", template}
	}

	return t.String(), nil
}

func findCaps(caps []string, template string, policyType string) (string, error) {
	// FIXME: this is snappy specific, on other systems like the
	//        phone we may want different defaults.
	if template == "" && caps == nil {
		caps = defaultPolicyGroups
	} else if caps == nil {
		caps = []string{}
	}

	subdir := fmt.Sprintf("policygroups/%s/%0.2f", defaultPolicyVendor, defaultPolicyVersion)
	parent := ""
	fw_parent := ""
	if policyType == "apparmor" {
		parent = filepath.Join(aaPolicyDir, subdir)
		fw_parent = filepath.Join(aaFrameworkPolicyDir, "policygroups")
	} else if policyType == "seccomp" {
		parent = filepath.Join(scPolicyDir, subdir)
		fw_parent = filepath.Join(scFrameworkPolicyDir, "policygroups")
	} else {
		return "", errPolicyTypeNotFound
	}

	// Nothing to find if caps is empty
	found := len(caps) == 0
	bad_cap := ""
	var p bytes.Buffer
	for _, c := range caps {
		// Always prefer system policy
		dirs := []string{parent, fw_parent}
		for _, dir := range dirs {
			fn := filepath.Join(dir, c)
			tmp, err := ioutil.ReadFile(fn)
			if err == nil {
				p.Write(tmp)
				found = true
				break
			}
		}
		if found == false {
			bad_cap = c
			break
		}
	}

	if found == false {
		return "", &errPolicyNotFound{"cap", bad_cap}
	}

	return p.String(), nil
}

func getAppArmorVars(appID *securityAppID) string {
	aavars := "\n# Specified profile variables\n"
	aavars += fmt.Sprintf("@{APP_APPNAME}=\"%s\"\n", appID.Appname)
	aavars += fmt.Sprintf("@{APP_ID_DBUS}=\"%s\"\n", dbusPath(appID.AppID))
	aavars += fmt.Sprintf("@{APP_PKGNAME_DBUS}=\"%s\"\n", dbusPath(appID.Pkgname))
	aavars += fmt.Sprintf("@{APP_PKGNAME}=\"%s\"\n", appID.Pkgname)
	aavars += fmt.Sprintf("@{APP_VERSION}=\"%s\"\n", appID.Version)
	aavars += "@{INSTALL_DIR}=\"{/apps,/oem}\"\n"
	aavars += "# Deprecated:\n"
	aavars += "@{CLICK_DIR}=\"{/apps,/oem}\""

	return aavars
}

func getAppArmorTemplatedPolicy(m *packageYaml, appID *securityAppID, template string, caps []string) (string, error) {
	t, err := findTemplate(template, "apparmor")
	if err != nil {
		return "", err
	}
	p, err := findCaps(caps, template, "apparmor")
	if err != nil {
		return "", err
	}

	aaPolicy := strings.Replace(t, "\n###VAR###\n", getAppArmorVars(appID)+"\n", 1)
	aaPolicy = strings.Replace(aaPolicy, "\n###PROFILEATTACH###", fmt.Sprintf("\nprofile \"%s\"", appID.AppID), 1)

	// FIXME: indentation
	aacap := "# No caps (policy groups) specified\n"
	if p != "" {
		aacap = "# Rules specified via caps (policy groups)\n"
		aacap += p + "\n"
	}
	aaPolicy = strings.Replace(aaPolicy, "###POLICYGROUPS###\n", aacap, 1)

	aaPolicy = strings.Replace(aaPolicy, "###ABSTRACTIONS###\n", "# No abstractions specified\n", 1)
	aaPolicy = strings.Replace(aaPolicy, "###READS###\n", "# No read paths specified\n", 1)
	aaPolicy = strings.Replace(aaPolicy, "###WRITES###\n", "# No write paths specified\n", 1)

	return aaPolicy, nil
}

func getSeccompTemplatedPolicy(m *packageYaml, appID *securityAppID, template string, caps []string) (string, error) {
	t, err := findTemplate(template, "seccomp")
	if err != nil {
		return "", err
	}
	p, err := findCaps(caps, template, "seccomp")
	if err != nil {
		return "", err
	}

	scPolicy := t + "\n" + p
	scPolicy = strings.Replace(scPolicy, "\ndeny ", "\n# EXPLICITLY DENIED: ", -1)

	return scPolicy, nil
}

func getAppArmorCustomPolicy(m *packageYaml, appID *securityAppID, fn string) (string, error) {
	var custom bytes.Buffer
	tmp, err := ioutil.ReadFile(fn)
	if err != nil {
		return "", err
	}
	custom.Write(tmp)

	aaPolicy := strings.Replace(custom.String(), "\n###VAR###\n", getAppArmorVars(appID)+"\n", 1)
	aaPolicy = strings.Replace(aaPolicy, "\n###PROFILEATTACH###", fmt.Sprintf("\nprofile \"%s\"", appID.AppID), 1)

	return aaPolicy, nil
}

func getSeccompCustomPolicy(m *packageYaml, appID *securityAppID, fn string) (string, error) {
	var custom bytes.Buffer
	tmp, err := ioutil.ReadFile(fn)
	if err != nil {
		return "", err
	}
	custom.Write(tmp)
	return custom.String(), nil
}

func getAppID(appID string) (*securityAppID, error) {
	tmp := strings.Split(appID, "_")
	if len(tmp) != 3 {
		return nil, errInvalidAppID
	}
	id := securityAppID{
		AppID:   appID,
		Pkgname: tmp[0],
		Appname: tmp[1],
		Version: tmp[2],
	}
	return &id, nil
}

func loadAppArmorPolicy(fn string) ([]byte, error) {
	cmd := exec.Command("/sbin/apparmor_parser", "-r", "--write-cache", fn)
	return cmd.CombinedOutput()
}

func generatePolicy(m *packageYaml, baseDir string) error {
	foundError := false

	for _, service := range m.ServiceYamls {
		appID, err := getSecurityProfile(m, service.Name, baseDir)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to obtain APP_ID for %s: %v", service.Name, err)
			continue
		}

		id, err := getAppID(appID)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to obtain APP_ID for %s: %v", service.Name, err)
			continue
		}

		aaPolicy := ""
		scPolicy := ""
		if service.SecurityPolicy != nil {
			aaPolicy, err = getAppArmorCustomPolicy(m, id, filepath.Join(baseDir, service.SecurityPolicy.Apparmor))
			if err != nil {
				foundError = true
				logger.Noticef("Failed to generate custom AppArmor policy for %s: %v", service.Name, err)
				continue
			}
			scPolicy, err = getSeccompCustomPolicy(m, id, filepath.Join(baseDir, service.SecurityPolicy.Seccomp))
			if err != nil {
				foundError = true
				logger.Noticef("Failed to generate custom seccomp policy for %s: %v", service.Name, err)
				continue
			}
		} else if service.SecurityOverride != nil {
			aaPolicy = "TODO: security-override"
			scPolicy = "TODO: security-override"
		} else {
			aaPolicy, err = getAppArmorTemplatedPolicy(m, id, service.SecurityTemplate, service.SecurityCaps)
			if err != nil {
				foundError = true
				logger.Noticef("Failed to generate AppArmor policy for %s: %v", service.Name, err)
				continue
			}
			scPolicy, err = getSeccompTemplatedPolicy(m, id, service.SecurityTemplate, service.SecurityCaps)
			if err != nil {
				foundError = true
				logger.Noticef("Failed to generate seccomp policy for %s: %v", service.Name, err)
				continue
			}
		}
		aaFn := filepath.Join(aaProfilesDir, id.AppID)
		os.MkdirAll(filepath.Dir(aaFn), 0755)
		err = ioutil.WriteFile(aaFn, []byte(aaPolicy), 0644)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to write AppArmor policy for %s: %v", service.Name, err)
		}
		out, err := loadAppArmorPolicy(aaFn)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to load AppArmor policy for %s: %v\n:%s", service.Name, err, out)
		}

		scFn := filepath.Join(scProfilesDir, id.AppID)
		os.MkdirAll(filepath.Dir(scFn), 0755)
		err = ioutil.WriteFile(scFn, []byte(scPolicy), 0644)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to write seccomp policy for %s: %v", service.Name, err)
		}
	}

	// TODO: is there any way to combine this with the above?
	for _, binary := range m.Binaries {
		appID, err := getSecurityProfile(m, binary.Name, baseDir)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to obtain APP_ID for %s: %v", binary.Name, err)
			continue
		}

		id, err := getAppID(appID)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to obtain APP_ID for %s: %v", binary.Name, err)
			continue
		}

		aaPolicy := ""
		scPolicy := ""
		if binary.SecurityPolicy != nil {
			aaPolicy, err = getAppArmorCustomPolicy(m, id, filepath.Join(baseDir, binary.SecurityPolicy.Apparmor))
			if err != nil {
				foundError = true
				logger.Noticef("Failed to generate custom AppArmor policy for %s: %v", binary.Name, err)
				continue
			}
			scPolicy, err = getSeccompCustomPolicy(m, id, filepath.Join(baseDir, binary.SecurityPolicy.Seccomp))
			if err != nil {
				foundError = true
				logger.Noticef("Failed to generate custom seccomp policy for %s: %v", binary.Name, err)
				continue
			}
		} else if binary.SecurityOverride != nil {
			aaPolicy = "TODO: security-override"
			scPolicy = "TODO: security-override"
		} else {
			aaPolicy, err = getAppArmorTemplatedPolicy(m, id, binary.SecurityTemplate, binary.SecurityCaps)
			if err != nil {
				foundError = true
				logger.Noticef("Failed to generate AppArmor policy for %s: %v", binary.Name, err)
				continue
			}
			scPolicy, err = getSeccompTemplatedPolicy(m, id, binary.SecurityTemplate, binary.SecurityCaps)
			if err != nil {
				foundError = true
				logger.Noticef("Failed to generate seccomp policy for %s: %v", binary.Name, err)
				continue
			}
		}

		aaFn := filepath.Join(aaProfilesDir, id.AppID)
		os.MkdirAll(filepath.Dir(aaFn), 0755)
		err = ioutil.WriteFile(aaFn, []byte(aaPolicy), 0644)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to write AppArmor policy for %s: %v", binary.Name, err)
		}
		out, err := loadAppArmorPolicy(aaFn)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to load AppArmor policy for %s: %v\n:%s", binary.Name, err, out)
		}

		scFn := filepath.Join(scProfilesDir, id.AppID)
		os.MkdirAll(filepath.Dir(scFn), 0755)
		err = ioutil.WriteFile(scFn, []byte(scPolicy), 0644)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to write seccomp policy for %s: %v", binary.Name, err)
		}
	}

	if foundError {
		return errPolicyGen
	}

	return nil
}

// GeneratePolicyFromFile is used to generate security policy on the system
// from the specified manifest file name
func GeneratePolicyFromFile(fn string, force []bool) error {
	m, err := parsePackageYamlFile(fn)
	if err != nil {
		return err
	}

	if m.Type == "" || m.Type == pkg.TypeApp {
		_, err = originFromYamlPath(fn)
		if err != nil {
			if err == ErrInvalidPart {
				err = errOriginNotFound
			}
			return err
		}
	}

	// TODO: verify cache files here

	baseDir := strings.Replace(fn, "/meta/package.yaml", "", 1)
	err = generatePolicy(m, baseDir)
	if err != nil {
		return err
	}

	return err
}
