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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"text/template"

	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/pkg/clickdeb"
	"github.com/ubuntu-core/snappy/pkg/squashfs"

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
var shouldExcludeDefault = regexp.MustCompile(strings.Join([]string{
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
	`^\.snapignore$`,
}, "|")).MatchString

// fake static function variables
type keep struct {
	basedir string
	exclude func(string) bool
}

func (k *keep) shouldExclude(basedir string, file string) bool {
	if basedir == k.basedir {
		if k.exclude == nil {
			return false
		}

		return k.exclude(file)
	}

	k.basedir = basedir
	k.exclude = nil

	snapignore, err := os.Open(filepath.Join(basedir, ".snapignore"))
	if err != nil {
		return false
	}

	scanner := bufio.NewScanner(snapignore)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if _, err := regexp.Compile(line); err != nil {
			// not a regexp
			line = regexp.QuoteMeta(line)
		}
		lines = append(lines, line)
	}

	fullRegex := strings.Join(lines, "|")
	exclude, err := regexp.Compile(fullRegex)
	if err == nil {
		k.exclude = exclude.MatchString

		return k.exclude(file)
	}

	// can't happen; can't even find a way to trigger it in testing.
	panic(fmt.Sprintf("|-composition of valid regexps is invalid?!? Please report this bug: %#v", fullRegex))
}

var shouldExcludeDynamic = new(keep).shouldExclude

func shouldExclude(basedir string, file string) bool {
	return shouldExcludeDefault(file) || shouldExcludeDynamic(basedir, file)
}

// small helper that return the architecture or "multi" if its multiple arches
func debArchitecture(m *packageYaml) string {
	switch len(m.Architectures) {
	case 0:
		return "unknown"
	case 1:
		return m.Architectures[0]
	default:
		return "multi"
	}
}

func parseReadme(readme string) (title, description string, err error) {
	file, err := os.Open(readme)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

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

func hashForFile(buildDir, path string, info os.FileInfo) (h *fileHash, err error) {
	sha512sum := ""
	// pointer so that omitempty works (we don't want size for
	// directories or symlinks)
	var size *int64
	if info.Mode().IsRegular() {
		sha512sum, err = helpers.Sha512sum(path)
		if err != nil {
			return nil, err
		}
		fsize := info.Size()
		size = &fsize
	}

	// major/minor handling
	device := ""
	major, minor, err := helpers.MajorMinor(info)
	if err == nil {
		device = fmt.Sprintf("%v,%v", major, minor)
	}

	if buildDir != "" {
		path = path[len(buildDir)+1:]
	}

	return &fileHash{
		Name:   path,
		Size:   size,
		Sha512: sha512sum,
		Device: device,
		// FIXME: not portable, this output is different on
		//        windows, macos
		Mode: newYamlFileMode(info.Mode()),
	}, nil
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
		// path will always start with buildDir...
		if path[len(buildDir):] == "/DEBIAN" {
			return filepath.SkipDir
		}
		// ...so if path's length is == buildDir, it's buildDir
		if len(path) == len(buildDir) {
			return nil
		}

		hash, err := hashForFile(buildDir, path, info)
		if err != nil {
			return err
		}
		hashes.Files = append(hashes.Files, hash)

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
Installed-Size: {{.InstalledSize}}
Description: {{.Title}}
`
	t := template.Must(template.New("control").Parse(debianControlTemplate))
	debianControlData := struct {
		Name            string
		Version         string
		InstalledSize   string
		Title           string
		DebArchitecture string
	}{
		m.Name, m.Version, installedSize, title, debArchitecture(m),
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
		Architecture:  m.Architectures,
		Framework:     m.FrameworksForClick(),
		Type:          m.Type,
		Icon:          m.Icon,
		InstalledSize: installedSize,
		Title:         title,
		Description:   description,
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

	// no umask here so that we get the permissions correct
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, errin error) (err error) {
		if errin != nil {
			return errin
		}

		relpath := path[len(sourceDir):]
		if relpath == "/DEBIAN" || shouldExclude(sourceDir, filepath.Base(path)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dest := filepath.Join(buildDir, relpath)

		// handle dirs
		if info.IsDir() {
			if err := os.Mkdir(dest, info.Mode()); err != nil {
				return err
			}
			// ensure that premissions are preserved
			uid := int(info.Sys().(*syscall.Stat_t).Uid)
			gid := int(info.Sys().(*syscall.Stat_t).Gid)
			return os.Chown(dest, uid, gid)
		}

		// handle char/block devices
		if helpers.IsDevice(info.Mode()) {
			return helpers.CopySpecialFile(path, dest)
		}

		if (info.Mode() & os.ModeSymlink) != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, dest)
		}

		// fail if its unsupported
		if !info.Mode().IsRegular() {
			return fmt.Errorf("can not handle type of file %s", path)
		}

		// it's a file. Maybe we can link it?
		if os.Link(path, dest) == nil {
			// whee
			return nil
		}
		// sigh. ok, copy it is.
		return helpers.CopyFile(path, dest, helpers.CopyFlagDefault)
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

func prepare(sourceDir, targetDir, buildDir string) (snapName string, err error) {
	// ensure we have valid content
	m, err := parsePackageYamlFile(filepath.Join(sourceDir, "meta", "package.yaml"))
	if err != nil {
		return "", err
	}

	if m.ExplicitLicenseAgreement {
		if err := licenseChecker(sourceDir); err != nil {
			return "", err
		}
	}

	if err := m.checkForNameClashes(); err != nil {
		return "", err
	}

	if err := copyToBuildDir(sourceDir, buildDir); err != nil {
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
	snapName = fmt.Sprintf("%s_%s_%v.snap", m.Name, m.Version, debArchitecture(m))

	if targetDir != "" {
		snapName = filepath.Join(targetDir, snapName)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return "", err
			}
		}
	}

	return snapName, nil
}

// BuildSquashfsSnap the given sourceDirectory and return the generated
// snap file
func BuildSquashfsSnap(sourceDir, targetDir string) (string, error) {
	// create build dir
	buildDir, err := ioutil.TempDir("", "snappy-build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(buildDir)

	snapName, err := prepare(sourceDir, targetDir, buildDir)
	if err != nil {
		return "", err
	}

	d := squashfs.New(snapName)
	if err = d.Build(buildDir); err != nil {
		return "", err
	}

	return snapName, nil
}

// BuildLegacySnap the given sourceDirectory and return the generated snap file
func BuildLegacySnap(sourceDir, targetDir string) (string, error) {
	// create build dir
	buildDir, err := ioutil.TempDir("", "snappy-build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(buildDir)

	snapName, err := prepare(sourceDir, targetDir, buildDir)
	if err != nil {
		return "", err
	}

	d, err := clickdeb.Create(snapName)
	if err != nil {
		return "", err
	}
	defer d.Close()

	err = d.Build(buildDir, func(dataTar string) error {
		// write hashes of the files plus the generated data tar
		return writeHashes(buildDir, dataTar)
	})
	if err != nil {
		return "", err
	}

	return snapName, nil
}
