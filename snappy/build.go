package snappy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"launchpad.net/snappy/helpers"
)

// FIXME: this is lie we tell click to make it happy for now
const staticPreinst = `#! /bin/sh
echo "Click packages may not be installed directly using dpkg."
echo "Use 'click install' instead."
exit 1
`

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
	if description == "" {
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
		m.Integration[hookName]["bin-path"] = v.Name

		_, hasApparmor := m.Integration[hookName]["apparmor"]
		_, hasApparmorProfile := m.Integration[hookName]["apparmor-profile"]
		if !hasApparmor && !hasApparmorProfile {
			defaultApparmorJSONFile := filepath.Join("meta", hookName+".apparmor")
			if err := ioutil.WriteFile(filepath.Join(buildDir, defaultApparmorJSONFile), []byte(defaultApparmorJSON), 0644); err != nil {
				return err
			}
			m.Integration[hookName]["apparmor"] = defaultApparmorJSONFile
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
		snappySystemdContent, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			return err
		}
		snappySystemdContentFile := filepath.Join("meta", hookName+".snappy-systemd")
		if err := ioutil.WriteFile(filepath.Join(buildDir, snappySystemdContentFile), []byte(snappySystemdContent), 0644); err != nil {
			return err
		}
		m.Integration[hookName]["snappy-systemd"] = snappySystemdContentFile

		// generate apparmor
		_, hasApparmor := m.Integration[hookName]["apparmor"]
		_, hasApparmorProfile := m.Integration[hookName]["apparmor-profile"]
		if !hasApparmor && !hasApparmorProfile {
			defaultApparmorJSONFile := filepath.Join("meta", hookName+".apparmor")
			if err := ioutil.WriteFile(filepath.Join(buildDir, defaultApparmorJSONFile), []byte(defaultApparmorJSON), 0644); err != nil {
				return err
			}
			m.Integration[hookName]["apparmor"] = defaultApparmorJSONFile
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

	// title description
	title, description, err := parseReadme(filepath.Join(buildDir, "meta", "readme.md"))
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
 {{.Descripton}}
`
	t := template.Must(template.New("control").Parse(debianControlTemplate))
	debianControlData := struct {
		Name            string
		Version         string
		Vendor          string
		InstalledSize   string
		Title           string
		Description     string
		DebArchitecture string
	}{
		m.Name, m.Version, m.Vendor, installedSize, title, description, debArchitecture(m),
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
	// FIXME: too simplistic, we need a ignore pattern for stuff
	//        like "*~" etc
	os.Remove(buildDir)

	return exec.Command("cp", "-a", sourceDir, buildDir).Run()
}

// Build the given sourceDirectory and return the generated snap file
func Build(sourceDir string) (string, error) {

	// ensure we have valid content
	m, err := parsePackageYamlFile(filepath.Join(sourceDir, "meta", "package.yaml"))
	if err != nil {
		return "", err
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

	// FIXME: we want a native build here without dpkg-deb to be
	//        about to build on non-ubuntu/debian systems
	cmd := exec.Command("fakeroot", "dpkg-deb", "-Zgzip", "--build", buildDir, snapName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		retCode, _ := helpers.ExitCode(err)
		return "", fmt.Errorf("failed with %d: %s", retCode, output)
	}

	return snapName, nil
}
