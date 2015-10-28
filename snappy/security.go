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
	polType string
	pol     string
}

func (e *errPolicyNotFound) Error() string {
	return fmt.Sprintf("could not find specified %s: %s", e.polType, e.pol)
}

var (
	// Note: these are true for ubuntu-core but perhaps not other flavors
	defaultTemplate     = "default"
	defaultPolicyGroups = []string{"network-client"}

	// These are set elsewhere
	defaultPolicyVendor  = ""
	defaultPolicyVersion = ""

	// Templates and policy groups (caps)
	aaPolicyDir = "/usr/share/apparmor/easyprof"
	scPolicyDir = "/usr/share/seccomp"
	// Framework policy
	aaFrameworkPolicyDir = filepath.Join(policy.SecBase, "apparmor")
	scFrameworkPolicyDir = filepath.Join(policy.SecBase, "seccomp")
	// AppArmor cache dir
	aaCacheDir = "/var/cache/apparmor"

	errOriginNotFound     = errors.New("could not detect origin")
	errPolicyTypeNotFound = errors.New("could not find specified policy type")
	errInvalidAppID       = errors.New("invalid APP_ID")
	errPolicyGen          = errors.New("errors found when generating policy")

	// ErrSystemVersionNotFound could not detect system version (eg, 15.04,
	// 15.10, etc)
	ErrSystemVersionNotFound = errors.New("could not detect system version")
	// ErrSystemFlavorNotFound could not detect system flavor (eg,
	// ubuntu-core, ubuntu-personal, etc)
	ErrSystemFlavorNotFound = errors.New("could not detect system flavor")
)

type securitySeccompOverride struct {
	Syscalls []string `yaml:"syscalls,omitempty"`
}

type securityAppID struct {
	AppID   string
	Pkgname string
	Appname string
	Version string
}

// FindUbuntuFlavor determines the flavor (eg, ubuntu-core, ubuntu-personal,
// etc) of the system, which is needed for determining the security policy
// policy-vendor
func FindUbuntuFlavor() (string, error) {
	// TODO: a downloaded snap targets a particular device. We need to map
	// that device type (flavor) to installed system security policy (eg
	// ubuntu-core, ubuntu-personal, etc). As of 2015-10-28,
	// ubuntu-personal images are no longer generated, and there is no
	// mechanism to know what the snap targets.
	return "ubuntu-core", nil
}

// FindUbuntuVersion determines the version (eg, 15.04, 15.10, etc) of the
// system, which is needed for determining the security policy
// policy-version
func FindUbuntuVersion() (string, error) {
	var buffer bytes.Buffer
	fn := "/etc/lsb-release"
	content, err := ioutil.ReadFile(fn)
	if err != nil {
		logger.Noticef("Failed to read %q: %v", fn, err)
		return "", err
	}
	buffer.Write(content)

	v := ""
	for _, line := range strings.Split(buffer.String(), "\n") {
		if strings.HasPrefix(line, "DISTRIB_RELEASE=") {
			tmp := strings.Split(line, "=")
			if len(tmp) != 2 {
				return v, ErrSystemVersionNotFound
			}
			v = tmp[1]
		}
	}
	if v == "" {
		err = ErrSystemVersionNotFound
	}
	return v, err
}

// Generate a string suitable for use in a DBus object
func dbusPath(s string) string {
	dbusStr := ""
	ok := regexp.MustCompile(`^[a-zA-Z0-9]$`)

	for _, c := range strings.SplitAfter(s, "") {
		if ok.MatchString(c) {
			dbusStr += c
		} else {
			dbusStr += fmt.Sprintf("_%02x", c)
		}
	}

	return dbusStr
}

// Calculate whitespace prefix based on occurrence of s in t
func findWhitespacePrefix(t string, s string) string {
	pat := regexp.MustCompile(`^ *` + s)
	p := ""
	for _, line := range strings.Split(t, "\n") {
		if pat.MatchString(line) {
			for i := 0; i < len(line)-len(strings.TrimLeft(line, " ")); i++ {
				p += " "
			}
			break
		}
	}
	return p
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

func readSeccompOverride(yamlPath string, s *securitySeccompOverride) error {
	yamlData, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(yamlData, &s)
	if err != nil {
		return &ErrInvalidYaml{File: "package.yaml[seccomp override]", Err: err, Yaml: yamlData}
	}

	return nil
}

func findTemplate(template string, policyType string) (string, error) {
	if template == "" {
		template = defaultTemplate
	}

	systemTemplate := ""
	fwTemplate := ""
	subdir := fmt.Sprintf("templates/%s/%s", defaultPolicyVendor, defaultPolicyVersion)
	if policyType == "apparmor" {
		systemTemplate = filepath.Join(aaPolicyDir, subdir, template)
		fwTemplate = filepath.Join(aaFrameworkPolicyDir, "templates", template)
	} else if policyType == "seccomp" {
		systemTemplate = filepath.Join(scPolicyDir, subdir, template)
		fwTemplate = filepath.Join(scFrameworkPolicyDir, "templates", template)
	} else {
		return "", errPolicyTypeNotFound
	}

	// Always prefer system policy
	found := false
	fns := []string{systemTemplate, fwTemplate}
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

	subdir := fmt.Sprintf("policygroups/%s/%s", defaultPolicyVendor, defaultPolicyVersion)
	parent := ""
	fwParent := ""
	if policyType == "apparmor" {
		parent = filepath.Join(aaPolicyDir, subdir)
		fwParent = filepath.Join(aaFrameworkPolicyDir, "policygroups")
	} else if policyType == "seccomp" {
		parent = filepath.Join(scPolicyDir, subdir)
		fwParent = filepath.Join(scFrameworkPolicyDir, "policygroups")
	} else {
		return "", errPolicyTypeNotFound
	}

	// Nothing to find if caps is empty
	found := len(caps) == 0
	badCap := ""
	var p bytes.Buffer
	for _, c := range caps {
		// Always prefer system policy
		policyDirs := []string{parent, fwParent}
		for _, dir := range policyDirs {
			fn := filepath.Join(dir, c)
			tmp, err := ioutil.ReadFile(fn)
			if err == nil {
				p.Write(tmp)
				if c != caps[len(caps)-1] {
					p.Write([]byte("\n"))
				}
				found = true
				break
			}
		}
		if found == false {
			badCap = c
			break
		}
	}

	if found == false {
		return "", &errPolicyNotFound{"cap", badCap}
	}

	return p.String(), nil
}

// TODO: once verified, reorganize all these
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

	aacaps := ""
	if p == "" {
		aacaps += "# No caps (policy groups) specified\n"
	} else {
		aacaps += "# Rules specified via caps (policy groups)\n"
		prefix := findWhitespacePrefix(t, "###POLICYGROUPS###")
		for _, line := range strings.Split(p, "\n") {
			if len(line) == 0 {
				aacaps += line + "\n"
			} else {
				aacaps += fmt.Sprintf("%s%s\n", prefix, line)
			}
		}
	}
	aaPolicy = strings.Replace(aaPolicy, "###POLICYGROUPS###\n", aacaps, 1)

	// Only used with security-override
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
	cmd := exec.Command("/sbin/apparmor_parser", "-r", "--write-cache", "-L", aaCacheDir, fn)
	return cmd.CombinedOutput()
}

func (m *packageYaml) removeOneSecurityPolicy(name, baseDir string) error {
	profileName, err := getSecurityProfile(m, filepath.Base(name), baseDir)
	if err != nil {
		return err
	}

	// seccomp profile
	fn := filepath.Join(dirs.SnapSeccompDir, profileName)
	if err := os.Remove(fn); err != nil && !os.IsNotExist(err) {
		return err
	}

	// apparmor cache
	fn = filepath.Join(aaCacheDir, profileName)
	if err := os.Remove(fn); err != nil && !os.IsNotExist(err) {
		return err
	}

	// apparmor profile
	fn = filepath.Join(dirs.SnapAppArmorDir, profileName)
	if err := os.Remove(fn); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func removePolicy(m *packageYaml, baseDir string) error {
	for _, service := range m.ServiceYamls {
		if err := m.removeOneSecurityPolicy(service.Name, baseDir); err != nil {
			return err
		}
	}

	for _, binary := range m.Binaries {
		if err := m.removeOneSecurityPolicy(binary.Name, baseDir); err != nil {
			return err
		}
	}

	return nil
}

func (sd *SecurityDefinitions) generatePolicyForServiceBinary(m *packageYaml, name string, baseDir string) error {
	appID, err := getSecurityProfile(m, name, baseDir)
	if err != nil {
		logger.Noticef("Failed to obtain APP_ID for %s: %v", name, err)
		return err
	}

	id, err := getAppID(appID)
	if err != nil {
		logger.Noticef("Failed to obtain APP_ID for %s: %v", name, err)
		return err
	}

	aaPolicy := ""
	scPolicy := ""
	if sd.SecurityPolicy != nil {
		aaPolicy, err = getAppArmorCustomPolicy(m, id, filepath.Join(baseDir, sd.SecurityPolicy.Apparmor))
		if err != nil {
			logger.Noticef("Failed to generate custom AppArmor policy for %s: %v", name, err)
			return err
		}
		scPolicy, err = getSeccompCustomPolicy(m, id, filepath.Join(baseDir, sd.SecurityPolicy.Seccomp))
		if err != nil {
			logger.Noticef("Failed to generate custom seccomp policy for %s: %v", name, err)
			return err
		}
	} else if sd.SecurityOverride != nil {
		aaPolicy = "TODO: security-override"
		scPolicy = "TODO: security-override"
	} else {
		aaPolicy, err = getAppArmorTemplatedPolicy(m, id, sd.SecurityTemplate, sd.SecurityCaps)
		if err != nil {
			logger.Noticef("Failed to generate AppArmor policy for %s: %v", name, err)
			return err
		}
		scPolicy, err = getSeccompTemplatedPolicy(m, id, sd.SecurityTemplate, sd.SecurityCaps)
		if err != nil {
			logger.Noticef("Failed to generate seccomp policy for %s: %v", name, err)
			return err
		}
	}

	scFn := filepath.Join(dirs.SnapSeccompDir, id.AppID)
	os.MkdirAll(filepath.Dir(scFn), 0755)
	err = ioutil.WriteFile(scFn, []byte(scPolicy), 0644)
	if err != nil {
		logger.Noticef("Failed to write seccomp policy for %s: %v", name, err)
		return err
	}

	aaFn := filepath.Join(dirs.SnapAppArmorDir, id.AppID)
	os.MkdirAll(filepath.Dir(aaFn), 0755)
	err = ioutil.WriteFile(aaFn, []byte(aaPolicy), 0644)
	if err != nil {
		logger.Noticef("Failed to write AppArmor policy for %s: %v", name, err)
		return err
	}
	out, err := loadAppArmorPolicy(aaFn)
	if err != nil {
		logger.Noticef("Failed to load AppArmor policy for %s: %v\n:%s", name, err, out)
		return err
	}

	return nil
}

func generatePolicy(m *packageYaml, baseDir string) error {
	var err error
	defaultPolicyVendor, err = FindUbuntuFlavor()
	if err != nil {
		return err
	}
	defaultPolicyVersion, err = FindUbuntuVersion()
	if err != nil {
		return err
	}

	foundError := false

	for _, service := range m.ServiceYamls {
		err := service.generatePolicyForServiceBinary(m, service.Name, baseDir)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to obtain APP_ID for %s: %v", service.Name, err)
			continue
		}
	}

	for _, binary := range m.Binaries {
		err := binary.generatePolicyForServiceBinary(m, binary.Name, baseDir)
		if err != nil {
			foundError = true
			logger.Noticef("Failed to obtain APP_ID for %s: %v", binary.Name, err)
			continue
		}
	}

	if foundError {
		return errPolicyGen
	}

	return nil
}

// GeneratePolicyFromFile is used to generate security policy on the system
// from the specified manifest file name
func GeneratePolicyFromFile(fn string, force bool) error {
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
