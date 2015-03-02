package snappy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

const staticPreinst = `#! /bin/sh
echo "Snap packages may not be installed directly using dpkg."
echo "Use 'snappy install' instead."
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

// Build the given sourceDirectory and return the generated snap file
func Build(sourceDir string) (string, error) {

	// ensure we have valid content
	m, err := readPackageYaml(filepath.Join(sourceDir, "meta", "package.yaml"))
	if err != nil {
		return "", err
	}

	// create build dir
	buildDir, err := ioutil.TempDir("", "snappy-build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(buildDir)

	// FIXME: too simplistic, we need a ignore pattern for stuff
	//        like "*~" etc
	os.Remove(buildDir)
	if err := exec.Command("cp", "-a", sourceDir, buildDir).Run(); err != nil {
		return "", err
	}
	debianDir := filepath.Join(buildDir, "DEBIAN")
	if err := os.MkdirAll(debianDir, 0755); err != nil {
		return "", err
	}

	// FIXME: get "du" output
	installedSize := "fixme-999"
	// FIXME: readme.md parsing
	title := "fixme-title"
	description := "fixme-description"

	controlContent := fmt.Sprintf(`Package: %s
Version: %s
Architecture: %s
Maintainer: %s
Installed-Size: %s
Description: %s
 %s
`, m.Name, m.Version, m.Architecture, m.Vendor, installedSize, title, description)
	if err := ioutil.WriteFile(filepath.Join(debianDir, "control"), []byte(controlContent), 0644); err != nil {
		return "", err
	}

	// defaults
	if m.Architecture == "" {
		m.Architecture = "all"
	}
	if m.Integration == nil {
		m.Integration = make(map[string]clickAppHook)
	}

	// generate compat hooks for binaries
	for k, v := range m.Binaries {
		if _, ok := m.Integration[k]; !ok {
			m.Integration[k] = make(map[string]string)
		}
		m.Integration[k]["bin-path"] = v["name"]
		hookName := filepath.Base(v["name"])

		_, hasApparmor := m.Integration[k]["apparmor"]
		_, hasApparmorProfile := m.Integration[k]["apparmor-profile"]
		if !hasApparmor && !hasApparmorProfile {
			defaultApparmorJSONFile := filepath.Join("meta", hookName+".apparmor")
			if err := ioutil.WriteFile(filepath.Join(sourceDir, defaultApparmorJSONFile), []byte(defaultApparmorJSON), 0644); err != nil {
				return "", err
			}
			m.Integration[k]["apparmor"] = defaultApparmorJSONFile
		}
	}

	// FIXME: auto-generate:
	// - framework "ubuntu-core-15.04-dev1 (store compat)
	// - systemd yaml files and hook entr (*or* native support
	//   for the snappy-systemd hook)
	//   plus: default security.json templates
	// - click-bin-path files and hook entry (*or* native support)
	//   plus:  default security.json templates
	// - snappy-config apparmor security.json & hook entry

	// manifest
	cm := clickManifest{
		Name:          m.Name,
		Version:       m.Version,
		Framework:     m.Framework,
		Icon:          m.Icon,
		InstalledSize: installedSize,
		Title:         title,
		Description:   description,
		Hooks:         m.Integration,
	}
	manifestContent, err := json.MarshalIndent(cm, "", " ")
	if err != nil {
		return "", err
	}
	if err := ioutil.WriteFile(filepath.Join(debianDir, "manifest"), []byte(manifestContent), 0644); err != nil {
		return "", err
	}

	// preinst
	if err := ioutil.WriteFile(filepath.Join(debianDir, "preinst"), []byte(staticPreinst), 0755); err != nil {
		return "", err
	}

	// build the package
	snapName := fmt.Sprintf("%s_%s_%s.snap", m.Name, m.Version, m.Architecture)
	// FIXME: we want a native build here without dpkg-deb to be
	//        about to build on non-ubuntu/debian systems
	cmd := exec.Command("fakeroot", "dpkg-deb", "--build", buildDir, snapName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		retCode, _ := exitCode(err)
		return "", fmt.Errorf("failed with %d: %s", retCode, output)
	}

	return snapName, nil
}
