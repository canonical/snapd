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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/release"
)

type errPolicyNotFound struct {
	// type of policy, e.g. template or cap
	PolType string
	// apparmor or seccomp
	PolKind *securityPolicyType
	// name of the policy
	PolName string
}

func (e *errPolicyNotFound) Error() string {
	return fmt.Sprintf("could not find specified %s: %s (%s)", e.PolType, e.PolName, e.PolKind)
}

var (
	// Note: these are true for ubuntu-core but perhaps not other flavors
	defaultTemplateName = "default"
	defaultPolicyGroups = []string{"network-client"}

	// AppArmor cache dir
	aaCacheDir = "/var/cache/apparmor"

	errInvalidAppID = errors.New("invalid APP_ID")
	errPolicyGen    = errors.New("errors found when generating policy")

	// snappyConfig is the default securityDefinition for a snappy
	// config fragment
	snappyConfig = &SecurityDefinitions{
		SecurityCaps: []string{},
	}

	// TODO This is not actually right. Even if there are interfaces,
	// we still want to give the snap a default set of allowances,
	// such as being able to read and write in its own directories
	// and perhaps network access (we're still deciding on that
	// one). So the real logic we want here is: give the snap a
	// default set of permissions, and then whatever else the
	// interfaces permit (migration or not). This is coming soon.
	defaultSecurityPolicy = &SecurityDefinitions{
		SecurityCaps: []string{},
	}

	runAppArmorParser = runAppArmorParserImpl
)

func runAppArmorParserImpl(argv ...string) ([]byte, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	return cmd.CombinedOutput()
}

// SecurityOverrideDefinition is used to override apparmor or seccomp
// security defaults
type SecurityOverrideDefinition struct {
	ReadPaths    []string `yaml:"read-paths,omitempty" json:"read-paths,omitempty"`
	WritePaths   []string `yaml:"write-paths,omitempty" json:"write-paths,omitempty"`
	Abstractions []string `yaml:"abstractions,omitempty" json:"abstractions,omitempty"`
	Syscalls     []string `yaml:"syscalls,omitempty" json:"syscalls,omitempty"`

	// deprecated keys, we warn when we see those
	DeprecatedAppArmor interface{} `yaml:"apparmor,omitempty" json:"apparmor,omitempty"`
	DeprecatedSeccomp  interface{} `yaml:"seccomp,omitempty" json:"seccomp,omitempty"`
}

// SecurityPolicyDefinition is used to provide hand-crafted policy
type SecurityPolicyDefinition struct {
	AppArmor string `yaml:"apparmor" json:"apparmor"`
	Seccomp  string `yaml:"seccomp" json:"seccomp"`
}

// SecurityDefinitions contains the common apparmor/seccomp definitions
type SecurityDefinitions struct {
	// SecurityTemplate is a template name like "default"
	SecurityTemplate string `yaml:"security-template,omitempty" json:"security-template,omitempty"`
	// SecurityOverride is a override for the high level security json
	SecurityOverride *SecurityOverrideDefinition `yaml:"security-override,omitempty" json:"security-override,omitempty"`
	// SecurityPolicy is a hand-crafted low-level policy
	SecurityPolicy *SecurityPolicyDefinition `yaml:"security-policy,omitempty" json:"security-policy,omitempty"`

	// SecurityCaps is are the apparmor/seccomp capabilities for an app
	SecurityCaps []string `yaml:"caps,omitempty" json:"caps,omitempty"`
}

// NeedsAppArmorUpdate checks whether the security definitions are impacted by
// changes to policies or templates.
func (sd *SecurityDefinitions) NeedsAppArmorUpdate(policies, templates map[string]bool) bool {
	if sd.SecurityPolicy != nil {
		return false
	}

	if sd.SecurityOverride != nil {
		// XXX: actually inspect the override to figure out in more detail
		return true
	}

	if templates[sd.SecurityTemplate] {
		return true
	}

	for _, cap := range sd.SecurityCaps {
		if policies[cap] {
			return true
		}
	}

	return false
}

// securityPolicyType is a kind of securityPolicy, we currently
// have "apparmor" and "seccomp"
type securityPolicyType struct {
	name          string
	basePolicyDir string
}

var securityPolicyTypeAppArmor = securityPolicyType{
	name:          "apparmor",
	basePolicyDir: "/usr/share/apparmor/easyprof",
}

var securityPolicyTypeSeccomp = securityPolicyType{
	name:          "seccomp",
	basePolicyDir: "/usr/share/seccomp",
}

func (sp *securityPolicyType) policyDir() string {
	return filepath.Join(dirs.GlobalRootDir, sp.basePolicyDir)
}

// findTemplate returns the security template content from the template name.
func (sp *securityPolicyType) findTemplate(templateName string) (string, error) {
	if templateName == "" {
		templateName = defaultTemplateName
	}

	subdir := filepath.Join("templates", defaultPolicyVendor(), defaultPolicyVersion())
	systemTemplateDir := filepath.Join(sp.policyDir(), subdir, templateName)

	// Read system and framwork policy, but always prefer system policy
	fns := []string{systemTemplateDir}
	for _, fn := range fns {
		content, err := ioutil.ReadFile(fn)
		// it is ok if the file does not exists
		if os.IsNotExist(err) {
			continue
		}
		// but any other error is a failure
		if err != nil {
			return "", err
		}

		return string(content), nil
	}

	return "", &errPolicyNotFound{"template", sp, templateName}
}

// helper for findSingleCap that implements readlines().
func readSingleCapFile(fn string) ([]string, error) {
	p := []string{}

	r, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	s := bufio.NewScanner(r)
	for s.Scan() {
		p = append(p, s.Text())
	}
	if err := s.Err(); err != nil {
		return nil, err
	}

	return p, nil
}

// findSingleCap returns the security template content for a single
// security-cap.
func (sp *securityPolicyType) findSingleCap(capName, systemPolicyDir string) ([]string, error) {
	found := false
	p := []string{}

	policyDirs := []string{systemPolicyDir}
	for _, dir := range policyDirs {
		fn := filepath.Join(dir, capName)
		newCaps, err := readSingleCapFile(fn)
		// its ok if the file does not exist
		if os.IsNotExist(err) {
			continue
		}
		// but any other error is not ok
		if err != nil {
			return nil, err
		}
		p = append(p, newCaps...)
		found = true
		break
	}

	if found == false {
		return nil, &errPolicyNotFound{"cap", sp, capName}
	}

	return p, nil
}

// findCaps returns the security template content for the given list
// of security-caps.
func (sp *securityPolicyType) findCaps(caps []string, templateName string) ([]string, error) {
	// XXX: this is snappy specific, on other systems like the phone we may
	// want different defaults.
	if templateName == "" && caps == nil {
		caps = defaultPolicyGroups
	}

	// Nothing to find if caps is empty
	if len(caps) == 0 {
		return nil, nil
	}

	subdir := filepath.Join("policygroups", defaultPolicyVendor(), defaultPolicyVersion())
	parentDir := filepath.Join(sp.policyDir(), subdir)

	var p []string
	for _, c := range caps {
		newCap, err := sp.findSingleCap(c, parentDir)
		if err != nil {
			return nil, err
		}
		p = append(p, newCap...)
	}

	return p, nil
}

func defaultPolicyVendor() string {
	// FIXME: slightly ugly that we have to give a prefix here
	return fmt.Sprintf("ubuntu-%s", release.Get().Flavor)
}

func defaultPolicyVersion() string {
	// note that we can not use release.Get().Series here
	// because that will return "rolling" for the development
	// version but apparmor stores its templates under the
	// version number (e.g. 16.04) instead
	ver, err := release.ReadLsb()
	if err != nil {
		// when this happens we are in trouble
		panic(err)
	}
	return ver.Release
}

const allowed = `abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`

// Generate a string suitable for use in a DBus object
func dbusPath(s string) string {
	buf := bytes.NewBuffer(make([]byte, 0, len(s)))

	for _, c := range []byte(s) {
		if strings.IndexByte(allowed, c) >= 0 {
			fmt.Fprintf(buf, "%c", c)
		} else {
			fmt.Fprintf(buf, "_%02x", c)
		}
	}

	return buf.String()
}

// Calculate whitespace prefix based on occurrence of s in t
func findWhitespacePrefix(t string, s string) string {
	subs := regexp.MustCompile(`(?m)^( *)` + regexp.QuoteMeta(s)).FindStringSubmatch(t)
	if subs == nil {
		return ""
	}

	return subs[1]
}

func getSecurityProfile(m *snapYaml, appName, baseDir string) string {
	cleanedName := strings.Replace(appName, "/", "-", -1)

	return fmt.Sprintf("%s_%s_%s", m.Name, cleanedName, m.Version)
}

type securityAppID struct {
	AppID    string
	SnapName string
	AppName  string
	Version  string
}

func newAppID(appID string) (*securityAppID, error) {
	tmp := strings.Split(appID, "_")
	if len(tmp) != 3 {
		return nil, errInvalidAppID
	}
	id := securityAppID{
		AppID:    appID,
		SnapName: tmp[0],
		AppName:  tmp[1],
		Version:  tmp[2],
	}
	return &id, nil
}

// TODO: once verified, reorganize all these
func (sa *securityAppID) appArmorVars() string {
	aavars := fmt.Sprintf(`
# Specified profile variables
@{APP_APPNAME}="%s"
@{APP_ID_DBUS}="%s"
@{APP_PKGNAME_DBUS}="%s"
@{APP_PKGNAME}="%s"
@{APP_VERSION}="%s"
@{INSTALL_DIR}="/snap"
# Deprecated:
@{CLICK_DIR}="/snap"`, sa.AppName, dbusPath(sa.AppID), dbusPath(sa.SnapName), sa.SnapName, sa.Version)
	return aavars
}

func genAppArmorPathRule(path string, access string) (string, error) {
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "@{") {
		logger.Noticef("Bad path: %s", path)
		return "", errPolicyGen
	}

	owner := ""
	if strings.HasPrefix(path, "/home") || strings.HasPrefix(path, "@{HOME") {
		owner = "owner "
	}

	rules := ""
	if strings.HasSuffix(path, "/") {
		rules += fmt.Sprintf("%s %s,\n", path, access)
		rules += fmt.Sprintf("%s%s** %s,\n", owner, path, access)
	} else if strings.HasSuffix(path, "/**") || strings.HasSuffix(path, "/*") {
		rules += fmt.Sprintf("%s/ %s,\n", filepath.Dir(path), access)
		rules += fmt.Sprintf("%s%s %s,\n", owner, path, access)
	} else {
		rules += fmt.Sprintf("%s%s %s,\n", owner, path, access)
	}

	return rules, nil
}

func mergeAppArmorTemplateAdditionalContent(appArmorTemplate, aaPolicy string, overrides *SecurityOverrideDefinition) (string, error) {
	// ensure we have
	if overrides == nil {
		overrides = &SecurityOverrideDefinition{}
	}

	if overrides.ReadPaths == nil {
		aaPolicy = strings.Replace(aaPolicy, "###READS###\n", "# No read paths specified\n", 1)
	} else {
		s := "# Additional read-paths from security-override\n"
		prefix := findWhitespacePrefix(appArmorTemplate, "###READS###")
		for _, readpath := range overrides.ReadPaths {
			rules, err := genAppArmorPathRule(strings.Trim(readpath, " "), "rk")
			if err != nil {
				return "", err
			}
			lines := strings.Split(rules, "\n")
			for _, rule := range lines {
				s += fmt.Sprintf("%s%s\n", prefix, rule)
			}
		}
		aaPolicy = strings.Replace(aaPolicy, "###READS###\n", s, 1)
	}

	if overrides.WritePaths == nil {
		aaPolicy = strings.Replace(aaPolicy, "###WRITES###\n", "# No write paths specified\n", 1)
	} else {
		s := "# Additional write-paths from security-override\n"
		prefix := findWhitespacePrefix(appArmorTemplate, "###WRITES###")
		for _, writepath := range overrides.WritePaths {
			rules, err := genAppArmorPathRule(strings.Trim(writepath, " "), "rwk")
			if err != nil {
				return "", err
			}
			lines := strings.Split(rules, "\n")
			for _, rule := range lines {
				s += fmt.Sprintf("%s%s\n", prefix, rule)
			}
		}
		aaPolicy = strings.Replace(aaPolicy, "###WRITES###\n", s, 1)
	}

	if overrides.Abstractions == nil {
		aaPolicy = strings.Replace(aaPolicy, "###ABSTRACTIONS###\n", "# No abstractions specified\n", 1)
	} else {
		s := "# Additional abstractions from security-override\n"
		prefix := findWhitespacePrefix(appArmorTemplate, "###ABSTRACTIONS###")
		for _, abs := range overrides.Abstractions {
			s += fmt.Sprintf("%s#include <abstractions/%s>\n", prefix, abs)
		}
		aaPolicy = strings.Replace(aaPolicy, "###ABSTRACTIONS###\n", s, 1)
	}

	return aaPolicy, nil
}

func getAppArmorTemplatedPolicy(m *snapYaml, appID *securityAppID, template string, caps []string, overrides *SecurityOverrideDefinition) (string, error) {
	t, err := securityPolicyTypeAppArmor.findTemplate(template)
	if err != nil {
		return "", err
	}
	p, err := securityPolicyTypeAppArmor.findCaps(caps, template)
	if err != nil {
		return "", err
	}

	aaPolicy := strings.Replace(t, "\n###VAR###\n", appID.appArmorVars()+"\n", 1)
	aaPolicy = strings.Replace(aaPolicy, "\n###PROFILEATTACH###", fmt.Sprintf("\nprofile \"%s\"", appID.AppID), 1)

	aacaps := ""
	if len(p) == 0 {
		aacaps += "# No caps (policy groups) specified\n"
	} else {
		aacaps += "# Rules specified via caps (policy groups)\n"
		prefix := findWhitespacePrefix(t, "###POLICYGROUPS###")
		for _, line := range p {
			if len(line) == 0 {
				aacaps += "\n"
			} else {
				aacaps += fmt.Sprintf("%s%s\n", prefix, line)
			}
		}
	}
	aaPolicy = strings.Replace(aaPolicy, "###POLICYGROUPS###\n", aacaps, 1)

	return mergeAppArmorTemplateAdditionalContent(t, aaPolicy, overrides)
}

func getSeccompTemplatedPolicy(m *snapYaml, appID *securityAppID, templateName string, caps []string, overrides *SecurityOverrideDefinition) (string, error) {
	t, err := securityPolicyTypeSeccomp.findTemplate(templateName)
	if err != nil {
		return "", err
	}
	p, err := securityPolicyTypeSeccomp.findCaps(caps, templateName)
	if err != nil {
		return "", err
	}

	scPolicy := t + "\n" + strings.Join(p, "\n")

	if overrides != nil && overrides.Syscalls != nil {
		scPolicy += "\n# Addtional syscalls from security-override\n"
		for _, syscall := range overrides.Syscalls {
			scPolicy += fmt.Sprintf("%s\n", syscall)
		}
	}

	scPolicy = strings.Replace(scPolicy, "\ndeny ", "\n# EXPLICITLY DENIED: ", -1)

	return scPolicy, nil
}

var finalCurtain = regexp.MustCompile(`}\s*$`)

func getAppArmorCustomPolicy(m *snapYaml, appID *securityAppID, fn string, overrides *SecurityOverrideDefinition) (string, error) {
	custom, err := ioutil.ReadFile(fn)
	if err != nil {
		return "", err
	}

	aaPolicy := strings.Replace(string(custom), "\n###VAR###\n", appID.appArmorVars()+"\n", 1)
	aaPolicy = strings.Replace(aaPolicy, "\n###PROFILEATTACH###", fmt.Sprintf("\nprofile \"%s\"", appID.AppID), 1)

	// a custom policy may not have the overrides defined that we
	// use for the hw-assign work. so we insert them here
	aaPolicy = finalCurtain.ReplaceAllString(aaPolicy, `
###READS###
###WRITES###
###ABSTRACTIONS###
}
`)

	return mergeAppArmorTemplateAdditionalContent("", aaPolicy, overrides)
}

func getSeccompCustomPolicy(m *snapYaml, appID *securityAppID, fn string) (string, error) {
	custom, err := ioutil.ReadFile(fn)
	if err != nil {
		return "", err
	}

	return string(custom), nil
}

var loadAppArmorPolicy = func(fn string) ([]byte, error) {
	args := []string{
		"/sbin/apparmor_parser",
		"-r",
		"--write-cache",
		"-L", aaCacheDir,
		fn,
	}
	content, err := runAppArmorParser(args...)
	if err != nil {
		logger.Noticef("%v failed", args)
	}
	return content, err
}

func removeOneSecurityPolicy(m *snapYaml, name, baseDir string) error {
	profileName := getSecurityProfile(m, filepath.Base(name), baseDir)

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

func RemoveGeneratedSnapSecurity(s *Snap) error {
	m := s.m
	baseDir := s.Info().MountDir()
	for _, app := range m.Apps {
		if app.Daemon == "" {
			continue
		}
		if err := removeOneSecurityPolicy(m, app.Name, baseDir); err != nil {
			return err
		}
	}

	for _, app := range m.Apps {
		if app.Daemon != "" {
			continue
		}
		if err := removeOneSecurityPolicy(m, app.Name, baseDir); err != nil {
			return err
		}
	}

	if err := removeOneSecurityPolicy(m, "snappy-config", baseDir); err != nil {
		return err
	}

	return nil
}

func (sd *SecurityDefinitions) mergeAppArmorSecurityOverrides(new *SecurityOverrideDefinition) {
	// nothing to do
	if new == nil {
		return
	}

	// ensure we have valid structs to work with
	if sd.SecurityOverride == nil {
		sd.SecurityOverride = &SecurityOverrideDefinition{}
	}

	sd.SecurityOverride.ReadPaths = append(sd.SecurityOverride.ReadPaths, new.ReadPaths...)
	sd.SecurityOverride.WritePaths = append(sd.SecurityOverride.WritePaths, new.WritePaths...)
	sd.SecurityOverride.Abstractions = append(sd.SecurityOverride.Abstractions, new.Abstractions...)
}

type securityPolicyResult struct {
	id *securityAppID

	aaPolicy string
	aaFn     string

	scPolicy string
	scFn     string
}

func (sd *SecurityDefinitions) warnDeprecatedKeys() {
	if sd.SecurityOverride != nil && sd.SecurityOverride.DeprecatedAppArmor != nil {
		logger.Noticef("The security-override.apparmor key is no longer supported, please use use security-override directly")
	}
	if sd.SecurityOverride != nil && sd.SecurityOverride.DeprecatedSeccomp != nil {
		logger.Noticef("The security-override.seccomp key is no longer supported, please use use security-override directly")
	}
}

func (sd *SecurityDefinitions) generatePolicyForServiceBinaryResult(m *snapYaml, appName string, baseDir string) (*securityPolicyResult, error) {
	res := &securityPolicyResult{}
	appID := getSecurityProfile(m, appName, baseDir)

	resID, err := newAppID(appID)
	if err != nil {
		logger.Noticef("Failed to obtain APP_ID for %s: %v", appName, err)
		return nil, err
	}
	res.id = resID

	// warn about deprecated
	sd.warnDeprecatedKeys()

	// add the hw-override parts and merge with the other overrides
	hwaccessOverrides, err := readHWAccessYamlFile(m.Name)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	sd.mergeAppArmorSecurityOverrides(&hwaccessOverrides)
	if sd.SecurityPolicy != nil {
		res.aaPolicy, err = getAppArmorCustomPolicy(m, res.id, filepath.Join(baseDir, sd.SecurityPolicy.AppArmor), sd.SecurityOverride)
		if err != nil {
			logger.Noticef("Failed to generate custom AppArmor policy for %s: %v", m.Name, err)
			return nil, err
		}
		res.scPolicy, err = getSeccompCustomPolicy(m, res.id, filepath.Join(baseDir, sd.SecurityPolicy.Seccomp))
		if err != nil {
			logger.Noticef("Failed to generate custom seccomp policy for %s: %v", m.Name, err)
			return nil, err
		}

	} else {
		res.aaPolicy, err = getAppArmorTemplatedPolicy(m, res.id, sd.SecurityTemplate, sd.SecurityCaps, sd.SecurityOverride)
		if err != nil {
			logger.Noticef("Failed to generate AppArmor policy for %s: %v", appName, err)
			return nil, err
		}

		res.scPolicy, err = getSeccompTemplatedPolicy(m, res.id, sd.SecurityTemplate, sd.SecurityCaps, sd.SecurityOverride)
		if err != nil {
			logger.Noticef("Failed to generate seccomp policy for %s: %v", appName, err)
			return nil, err
		}
	}
	res.scFn = filepath.Join(dirs.SnapSeccompDir, res.id.AppID)
	res.aaFn = filepath.Join(dirs.SnapAppArmorDir, res.id.AppID)

	return res, nil
}

func (sd *SecurityDefinitions) generatePolicyForServiceBinary(m *snapYaml, appName string, baseDir string) error {
	p, err := sd.generatePolicyForServiceBinaryResult(m, appName, baseDir)
	if err != nil {
		return err
	}

	os.MkdirAll(filepath.Dir(p.scFn), 0755)
	err = osutil.AtomicWriteFile(p.scFn, []byte(p.scPolicy), 0644, 0)
	if err != nil {
		logger.Noticef("Failed to write seccomp policy for %s: %v", appName, err)
		return err
	}

	os.MkdirAll(filepath.Dir(p.aaFn), 0755)
	err = osutil.AtomicWriteFile(p.aaFn, []byte(p.aaPolicy), 0644, 0)
	if err != nil {
		logger.Noticef("Failed to write AppArmor policy for %s: %v", appName, err)
		return err
	}
	out, err := loadAppArmorPolicy(p.aaFn)
	if err != nil {
		logger.Noticef("Failed to load AppArmor policy for %s: %v\n:%s", appName, err, out)
		return err
	}

	return nil
}

// FIXME: move into something more generic - Snap.HasConfig?
func hasConfig(baseDir string) bool {
	return osutil.FileExists(filepath.Join(baseDir, "meta", "hooks", "config"))
}

func findPlugForApp(m *snapYaml, app *AppYaml) (*plugYaml, error) {
	if len(app.PlugsRef) == 0 {
		return nil, nil
	}
	if len(app.PlugsRef) != 1 {
		return nil, fmt.Errorf("only a single plug is supported, %d found", len(app.PlugsRef))
	}

	plug, ok := m.Plugs[app.PlugsRef[0]]
	if !ok {
		return nil, fmt.Errorf("can not find plug %q", app.PlugsRef[0])
	}
	return plug, nil
}

func SetupSnapSecurity(s *Snap) error {
	var foundError error

	m := s.m
	baseDir := s.Info().MountDir()

	// generate default security config for snappy-config
	if hasConfig(baseDir) {
		if err := snappyConfig.generatePolicyForServiceBinary(m, "snappy-config", baseDir); err != nil {
			foundError = err
			logger.Noticef("Failed to obtain APP_ID for %s: %v", "snappy-config", err)
		}
	}

	for _, app := range m.Apps {
		plug, err := findPlugForApp(m, app)
		if err != nil {
			return err
		}

		// if no plug is specified, use the defaultSecurityPolicy
		if plug == nil {
			if err = defaultSecurityPolicy.generatePolicyForServiceBinary(m, app.Name, baseDir); err != nil {
				logger.Noticef("Failed to generate policy for app %s: %v", app.Name, err)
			}
			continue
		}

		err = plug.generatePolicyForServiceBinary(m, app.Name, baseDir)
		if err != nil {
			foundError = err
			logger.Noticef("Failed to generate policy for service %s: %v", app.Name, err)
			continue
		}
	}

	// FIXME: if there are multiple errors only the last one
	//        will be preserved
	if foundError != nil {
		return foundError
	}

	return nil
}

// regeneratePolicyForSnap is used to regenerate all security policy for a
// given snap
func regeneratePolicyForSnap(snapname string) error {
	globExpr := filepath.Join(dirs.SnapAppArmorDir, fmt.Sprintf("%s_*", snapname))
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		// Nothing to regenerate is not an error
		return nil
	}

	appliedVersion := ""
	for _, profile := range matches {
		appID, err := newAppID(filepath.Base(profile))
		if err != nil {
			return err
		}
		if appID.Version != appliedVersion {
			// FIXME: dirs.SnapSnapsDir is too simple, gadget
			fn := filepath.Join(dirs.SnapSnapsDir, appID.SnapName, appID.Version, "meta", "snap.yaml")
			if !osutil.FileExists(fn) {
				continue
			}
			err := GeneratePolicyFromFile(fn, true)
			if err != nil {
				return err
			}
			appliedVersion = appID.Version
		}
	}

	return nil
}

// compare if the given policy matches the current system policy
// return an error if not
func comparePolicyToCurrent(p *securityPolicyResult) error {
	if err := compareSinglePolicyToCurrent(p.aaFn, p.aaPolicy); err != nil {
		return err
	}
	if err := compareSinglePolicyToCurrent(p.scFn, p.scPolicy); err != nil {
		return err
	}

	return nil
}

// helper for comparePolicyToCurrent that takes a single apparmor or seccomp
// policy and compares it to the system version
func compareSinglePolicyToCurrent(oldPolicyFn, newPolicy string) error {
	oldPolicy, err := ioutil.ReadFile(oldPolicyFn)
	if err != nil {
		return err
	}
	if string(oldPolicy) != newPolicy {
		return fmt.Errorf("policy differs %s", oldPolicyFn)
	}
	return nil
}

// CompareGeneratePolicyFromFile is used to simulate security policy
// generation and returns if the policy would have changed
func CompareGeneratePolicyFromFile(fn string) error {
	m, err := parseSnapYamlFileWithVersion(fn)
	if err != nil {
		return err
	}

	baseDir := filepath.Dir(filepath.Dir(fn))

	for _, app := range m.Apps {
		plug, err := findPlugForApp(m, app)
		if err != nil {
			return err
		}
		if plug == nil {
			continue
		}

		p, err := plug.generatePolicyForServiceBinaryResult(m, app.Name, baseDir)
		// FIXME: use apparmor_profile -p on both AppArmor profiles
		if err != nil {
			// FIXME: what to do here?
			return err
		}
		if err := comparePolicyToCurrent(p); err != nil {
			return err
		}
	}

	// now compare the snappy-config profile
	if hasConfig(baseDir) {
		p, err := snappyConfig.generatePolicyForServiceBinaryResult(m, "snappy-config", baseDir)
		if err != nil {
			return nil
		}
		if err := comparePolicyToCurrent(p); err != nil {
			return err
		}
	}

	return nil
}

// FIXME: refactor so that we don't need this
func parseSnapYamlFileWithVersion(fn string) (*snapYaml, error) {
	m, err := parseSnapYamlFile(fn)

	// FIXME: duplicated code from snapp.go:NewSnapFromYaml,
	//        version is overriden by sideloaded versions

	// use EvalSymlinks to resolve 'current' to the correct version
	dir, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(fn)))
	if err != nil {
		return nil, err
	}
	m.Version = filepath.Base(dir)

	return m, err
}

// GeneratePolicyFromFile is used to generate security policy on the system
// from the specified manifest file name
func GeneratePolicyFromFile(fn string, force bool) error {
	s, err := NewInstalledSnap(fn)
	if err != nil {
		return err
	}

	return SetupSnapSecurity(s)
}

// RegenerateAllPolicy will re-generate all policy that needs re-generating
func RegenerateAllPolicy(force bool) error {
	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return err
	}

	for _, snap := range installed {
		basedir := snap.Info().MountDir()
		yFn := filepath.Join(basedir, "meta", "snap.yaml")

		// FIXME: use ErrPolicyNeedsRegenerating here to check if
		//        re-generation is needed
		if err := CompareGeneratePolicyFromFile(yFn); err == nil {
			continue
		}

		// re-generate!
		logger.Noticef("re-generating security policy for %s", yFn)
		if err := GeneratePolicyFromFile(yFn, force); err != nil {
			return err
		}
	}

	return nil
}
