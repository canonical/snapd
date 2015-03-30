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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"launchpad.net/snappy/clickdeb"
	"launchpad.net/snappy/helpers"

	"gopkg.in/yaml.v2"
)

// FIXME: this is lie we tell click to make it happy for now
const staticPreinst = `#! /bin/sh
echo "Click packages may not be installed directly using dpkg."
echo "Use 'click install' instead."
exit 1
`

// from click's click.build.ClickBuilderBase, and there from
// @Dpkg::Source::Package::tar_ignore_default_pattern;
// changed to regexps from globs for sanity (hah)
//
// Please resist the temptation of optimizing the regexp by grouping
// things by hand. People will find it unreadable enough as it is.
var shouldExclude = regexp.MustCompile(strings.Join([]string{
	`\.snap$`, // added
	`\.click$`,
	`^\..*\.sw.$`,
	`~$`,
	`^,,`,
	`^\.[#~]`,
	`^\.arch-ids$`,
	`^\.arch-inventory$`,
	`^\.bzr$`,
	`^\.bzr-builddeb$`,
	`^\.bzr\.backup$`,
	`^\.bzr\.tags$`,
	`^\.bzrignore$`,
	`^\.cvsignore$`,
	`^\.git$`,
	`^\.gitattributes$`,
	`^\.gitignore$`,
	`^\.gitmodules$`,
	`^\.hg$`,
	`^\.hgignore$`,
	`^\.hgsigs$`,
	`^\.hgtags$`,
	`^\.shelf$`,
	`^\.svn$`,
	`^CVS$`,
	`^DEADJOE$`,
	`^RCS$`,
	`^_MTN$`,
	`^_darcs$`,
	`^{arch}$`,
}, "|")).MatchString

const defaultApparmorJSON = `{
    "template": "default",
    "policy_groups": [
        "networking"
    ],
    "policy_vendor": "ubuntu-snappy",
    "policy_version": 1.3
}`

// small helper that return the architecture or "multi" if its multiple arches
func debArchitecture(m *packageYaml) string {
	if len(m.Architectures) > 1 {
		return "multi"
	}
	return m.Architectures[0]
}

func parseReadme(readme string) (title, description string, err error) {
	file, err := os.Open(readme)
	if err != nil {
		return "", "", err
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if title == "" {
			title = scanner.Text()
			continue
		}

		if description != "" && scanner.Text() == "" {
			break
		}
		description += scanner.Text()
	}
	if title == "" {
		return "", "", ErrReadmeInvalid
	}

	if strings.TrimSpace(description) == "" {
		description = "no description"
	}

	return title, description, nil
}

func handleBinaries(buildDir string, m *packageYaml) error {
	for _, v := range m.Binaries {
		hookName := filepath.Base(v.Name)

		if _, ok := m.Integration[hookName]; !ok {
			m.Integration[hookName] = make(map[string]string)
		}
		// legacy click hook
		m.Integration[hookName]["bin-path"] = v.Name

		// handle the apparmor stuff
		if err := handleApparmor(buildDir, m, hookName, &v.SecurityDefinitions); err != nil {
			return err
		}
	}

	return nil
}

func handleServices(buildDir string, m *packageYaml) error {
	_, description, err := parseReadme(filepath.Join(buildDir, "meta", "readme.md"))
	if err != nil {
		return err
	}

	for _, v := range m.Services {
		hookName := filepath.Base(v.Name)

		if _, ok := m.Integration[hookName]; !ok {
			m.Integration[hookName] = make(map[string]string)
		}

		// generate snappyd systemd unit json
		if v.Description == "" {
			v.Description = description
		}

		// omit the name from the json to make the
		// click-reviewers-tool happy
		v.Name = ""
		snappySystemdContent, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			return err
		}
		snappySystemdContentFile := filepath.Join("meta", hookName+".snappy-systemd")
		if err := ioutil.WriteFile(filepath.Join(buildDir, snappySystemdContentFile), []byte(snappySystemdContent), 0644); err != nil {
			return err
		}
		m.Integration[hookName]["snappy-systemd"] = snappySystemdContentFile

		// handle the apparmor stuff
		if err := handleApparmor(buildDir, m, hookName, &v.SecurityDefinitions); err != nil {
			return err
		}
	}

	return nil
}

func handleConfigHookApparmor(buildDir string, m *packageYaml) error {
	configHookFile := filepath.Join(buildDir, "meta", "hooks", "config")
	if !helpers.FileExists(configHookFile) {
		return nil
	}

	hookName := "snappy-config"
	defaultApparmorJSONFile := filepath.Join("meta", hookName+".apparmor")
	if err := ioutil.WriteFile(filepath.Join(buildDir, defaultApparmorJSONFile), []byte(defaultApparmorJSON), 0644); err != nil {
		return err
	}
	m.Integration[hookName] = make(map[string]string)
	m.Integration[hookName]["apparmor"] = defaultApparmorJSONFile

	return nil
}

// the du(1) command, useful to override for testing
var duCmd = "du"

func dirSize(buildDir string) (string, error) {
	cmd := exec.Command(duCmd, "-s", "--apparent-size", buildDir)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.Fields(string(output))[0], nil
}

func writeHashes(buildDir, dataTar string) error {

	debianDir := filepath.Join(buildDir, "DEBIAN")
	os.MkdirAll(debianDir, 0755)

	hashes := hashesYaml{}
	sha512, err := helpers.Sha512sum(dataTar)
	if err != nil {
		return err
	}
	hashes.ArchiveSha512 = sha512

	err = filepath.Walk(buildDir, func(path string, info os.FileInfo, err error) error {
		if strings.HasPrefix(path[len(buildDir):], "/DEBIAN") {
			return nil
		}
		if path == buildDir {
			return nil
		}

		sha512sum := ""
		// pointer so that omitempty works (we don't want size for
		// directories or symlinks)
		var size *int64
		if info.Mode().IsRegular() {
			sha512sum, err = helpers.Sha512sum(path)
			if err != nil {
				return err
			}
			fsize := info.Size()
			size = &fsize
		}

		hashes.Files = append(hashes.Files, fileHash{
			Name:   path[len(buildDir)+1:],
			Size:   size,
			Sha512: sha512sum,
			// FIXME: not portable, this output is different on
			//        windows, macos
			Mode: newYamlFileMode(info.Mode()),
		})

		return nil
	})
	if err != nil {
		return err
	}

	content, err := yaml.Marshal(hashes)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filepath.Join(debianDir, "hashes.yaml"), []byte(content), 0644)
}

func writeDebianControl(buildDir string, m *packageYaml) error {
	debianDir := filepath.Join(buildDir, "DEBIAN")
	if err := os.MkdirAll(debianDir, 0755); err != nil {
		return err
	}

	// get "du" output, a deb needs the size in 1k blocks
	installedSize, err := dirSize(buildDir)
	if err != nil {
		return err
	}

	// title
	title, _, err := parseReadme(filepath.Join(buildDir, "meta", "readme.md"))
	if err != nil {
		return err
	}

	// create debian/control
	debianControlFile, err := os.OpenFile(filepath.Join(debianDir, "control"), os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer debianControlFile.Close()

	// generate debian/control content
	// FIXME: remove "Click-Version: 0.4" once we no longer need compat
	//        with snappy-python
	const debianControlTemplate = `Package: {{.Name}}
Version: {{.Version}}
Click-Version: 0.4
Architecture: {{.DebArchitecture}}
Maintainer: {{.Vendor}}
Installed-Size: {{.InstalledSize}}
Description: {{.Title}}
`
	t := template.Must(template.New("control").Parse(debianControlTemplate))
	debianControlData := struct {
		Name            string
		Version         string
		Vendor          string
		InstalledSize   string
		Title           string
		DebArchitecture string
	}{
		m.Name, m.Version, m.Vendor, installedSize, title, debArchitecture(m),
	}
	t.Execute(debianControlFile, debianControlData)

	// write static preinst
	return ioutil.WriteFile(filepath.Join(debianDir, "preinst"), []byte(staticPreinst), 0755)
}

func writeClickManifest(buildDir string, m *packageYaml) error {
	installedSize, err := dirSize(buildDir)
	if err != nil {
		return err
	}

	// title description
	title, description, err := parseReadme(filepath.Join(buildDir, "meta", "readme.md"))
	if err != nil {
		return err
	}

	cm := clickManifest{
		Name:          m.Name,
		Version:       m.Version,
		Framework:     m.Framework,
		Type:          m.Type,
		Icon:          m.Icon,
		InstalledSize: installedSize,
		Title:         title,
		Description:   description,
		Maintainer:    m.Vendor,
		Hooks:         m.Integration,
	}
	manifestContent, err := json.MarshalIndent(cm, "", " ")
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(filepath.Join(buildDir, "DEBIAN", "manifest"), []byte(manifestContent), 0644); err != nil {
		return err
	}

	return nil
}

func copyToBuildDir(sourceDir, buildDir string) error {
	sourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return err
	}

	err = os.Remove(buildDir)
	if err != nil && !os.IsNotExist(err) {
		// this shouldn't happen, but.
		return err
	}

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, errin error) (err error) {
		if errin != nil {
			return errin
		}

		if shouldExclude(filepath.Base(path)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dest := filepath.Join(buildDir, path[len(sourceDir):])
		if info.IsDir() {
			return os.Mkdir(dest, info.Mode())
		}

		// it's a file. Maybe we can link it?
		if os.Link(path, dest) == nil {
			// whee
			return nil
		}
		// sigh. ok, copy it is.
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode())
		if err != nil {
			return err
		}
		defer func() {
			// XXX: write a test for this. I dare you. I double-dare you.
			xerr := out.Close()
			if err == nil {
				err = xerr
			}
		}()
		_, err = io.Copy(out, in)
		// no need to sync, as it's a tempdir
		return err
	})
}

var nonEmptyLicense = regexp.MustCompile(`(?s)\S+`).Match

func checkLicenseExists(sourceDir string) error {
	lic := filepath.Join(sourceDir, "meta", "license.txt")
	if _, err := os.Stat(lic); err != nil {
		return err
	}
	buf, err := ioutil.ReadFile(lic)
	if err != nil {
		return err
	}
	if !nonEmptyLicense(buf) {
		return ErrLicenseBlank
	}
	return nil
}

var licenseChecker = checkLicenseExists

// Build the given sourceDirectory and return the generated snap file
func Build(sourceDir string) (string, error) {

	// ensure we have valid content
	m, err := parsePackageYamlFile(filepath.Join(sourceDir, "meta", "package.yaml"))
	if err != nil {
		return "", err
	}

	if m.ExplicitLicenseAgreement {
		err = licenseChecker(sourceDir)
		if err != nil {
			return "", err
		}
	}

	// create build dir
	buildDir, err := ioutil.TempDir("", "snappy-build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(buildDir)

	if err := copyToBuildDir(sourceDir, buildDir); err != nil {
		return "", err
	}

	// FIXME: the store needs this right now
	if !strings.Contains(m.Framework, "ubuntu-core-15.04-dev1") {
		l := strings.Split(m.Framework, ",")
		if l[0] == "" {
			m.Framework = "ubuntu-core-15.04-dev1"
		} else {
			l = append(l, "ubuntu-core-15.04-dev1")
			m.Framework = strings.Join(l, ", ")
		}
	}

	// defaults, mangling
	if m.Integration == nil {
		m.Integration = make(map[string]clickAppHook)
	}

	// generate compat hooks for binaries
	if err := handleBinaries(buildDir, m); err != nil {
		return "", err
	}

	// generate compat hooks for services
	if err := handleServices(buildDir, m); err != nil {
		return "", err
	}

	// generate config hook apparmor
	if err := handleConfigHookApparmor(buildDir, m); err != nil {
		return "", err
	}

	if err := writeDebianControl(buildDir, m); err != nil {
		return "", err
	}

	// manifest
	if err := writeClickManifest(buildDir, m); err != nil {
		return "", err
	}

	// build the package
	snapName := fmt.Sprintf("%s_%s_%v.snap", m.Name, m.Version, debArchitecture(m))

	// build it
	d := clickdeb.ClickDeb{Path: snapName}
	err = d.Build(buildDir, func(dataTar string) error {
		// write hashes of the files plus the generated data tar
		return writeHashes(buildDir, dataTar)
	})
	if err != nil {
		return "", err
	}

	return snapName, nil
}
