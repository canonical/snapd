package snappy

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	. "gopkg.in/check.v1"
)

func (s *SnapTestSuite) makeTestSnap(c *C) (snapFile string) {
	tmpdir, err := ioutil.TempDir(s.tempdir, "make-snap")
	c.Assert(err, IsNil)
	// content
	os.MkdirAll(path.Join(tmpdir, "bin"), 0755)
	content := `#!/bin/sh
echo "hello"`
	exampleBinary := path.Join(tmpdir, "bin", "foo")
	ioutil.WriteFile(exampleBinary, []byte(content), 0755)
	// meta
	os.MkdirAll(path.Join(tmpdir, "meta"), 0755)
	packageYaml := path.Join(tmpdir, "meta", "package.yaml")
	content = `
name: foo
version: 1.0
vendor: Foo Bar <foo@example.com>
`
	ioutil.WriteFile(packageYaml, []byte(content), 0644)
	readmeMd := path.Join(tmpdir, "meta", "readme.md")
	content = "Random\nExample"
	ioutil.WriteFile(readmeMd, []byte(content), 0644)
	// build it
	err = chDir(tmpdir, func() {
		cmd := exec.Command("snappy", "build", tmpdir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Println(string(output))
		}
		c.Assert(err, IsNil)
		allSnapFiles, err := filepath.Glob("*.snap")
		c.Assert(err, IsNil)
		c.Assert(len(allSnapFiles), Equals, 1)
		snapFile = allSnapFiles[0]
	})
	c.Assert(err, IsNil)
	return path.Join(tmpdir, snapFile)
}

func (s *SnapTestSuite) TestReadManifest(c *C) {
	manifestData := []byte(`{
   "description": "This is a simple hello world example.",
    "framework": "ubuntu-core-15.04-dev1",
    "hooks": {
        "echo": {
            "apparmor": "meta/echo.apparmor",
            "bin-path": "bin/echo"
        },
        "env": {
            "apparmor": "meta/env.apparmor",
            "bin-path": "bin/env"
        },
        "evil": {
            "apparmor": "meta/evil.apparmor",
            "bin-path": "bin/evil"
        }
    },
    "icon": "meta/hello.svg",
    "installed-size": "59",
    "maintainer": "Michael Vogt <mvo@ubuntu.com>",
    "name": "hello-world",
    "title": "Hello world example",
    "version": "1.0.5"
}`)
	manifest, err := readClickManifest(manifestData)
	c.Assert(err, IsNil)
	c.Assert(manifest.Name, Equals, "hello-world")
	c.Assert(manifest.Version, Equals, "1.0.5")
	c.Assert(manifest.Hooks["evil"]["bin-path"], Equals, "bin/evil")
}

func makeClickHook(c *C, hooksDir, hookName, hookContent string) {
	if _, err := os.Stat(hooksDir); err != nil {
		os.MkdirAll(hooksDir, 0755)
	}
	ioutil.WriteFile(path.Join(hooksDir, hookName+".hook"), []byte(hookContent), 0644)
}

func (s *SnapTestSuite) TestReadClickHookFile(c *C) {
	mockHooksDir := path.Join(s.tempdir, "hooks")
	makeClickHook(c, mockHooksDir, "snappy-systemd", `Hook-Name: systemd
User: root
Exec: /usr/lib/click-systemd/systemd-clickhook
Pattern: /var/lib/systemd/click/${id}`)
	hook, err := readClickHookFile(path.Join(mockHooksDir, "snappy-systemd.hook"))
	c.Assert(err, IsNil)
	c.Assert(hook.name, Equals, "systemd")
	c.Assert(hook.user, Equals, "root")
	c.Assert(hook.exec, Equals, "/usr/lib/click-systemd/systemd-clickhook")
	c.Assert(hook.pattern, Equals, "/var/lib/systemd/click/${id}")
}

func (s *SnapTestSuite) TestReadClickHooksDir(c *C) {
	mockHooksDir := path.Join(s.tempdir, "hooks")
	makeClickHook(c, mockHooksDir, "snappy-systemd", `Hook-Name: systemd
User: root
Exec: /usr/lib/click-systemd/systemd-clickhook
Pattern: /var/lib/systemd/click/${id}`)
	hooks, err := systemClickHooks(mockHooksDir)
	c.Assert(err, IsNil)
	c.Assert(len(hooks), Equals, 1)
	c.Assert(hooks["systemd"].name, Equals, "systemd")
}

func (s *SnapTestSuite) TestHandleClickHooks(c *C) {
	mockHooksDir := path.Join(s.tempdir, "hooks")

	// two hooks to ensure iterating works correct
	os.MkdirAll(path.Join(s.tempdir, "/var/lib/systemd/click/"), 0755)
	testSymlinkDir := path.Join(s.tempdir, "/var/lib/systemd/click/")
	content := fmt.Sprintf(`Hook-Name: systemd
Pattern: %s/${id}`, testSymlinkDir)
	makeClickHook(c, mockHooksDir, "snappy-systemd", content)
	
	os.MkdirAll(path.Join(s.tempdir, "/var/lib/apparmor/click/"), 0755)
	testSymlinkDir2 := path.Join(s.tempdir, "/var/lib/apparmor/click/")
	content = fmt.Sprintf(`Hook-Name: apparmor
Pattern: %s/${id}`, testSymlinkDir2)
	makeClickHook(c, mockHooksDir, "click-apparmor", content)

	instDir := path.Join(s.tempdir, "apps", "foo", "1.0")
	os.MkdirAll(instDir, 0755)
	ioutil.WriteFile(path.Join(instDir, "path-to-systemd-file"), []byte(""), 0644)
	ioutil.WriteFile(path.Join(instDir, "path-to-apparmor-file"), []byte(""), 0644)
	manifest := clickManifest{
		Name:    "foo",
		Version: "1.0",
		Hooks: map[string]clickAppHook{
			"app": clickAppHook{
				"systemd":  "path-to-systemd-file",
				"apparmor": "path-to-apparmor-file",
			},
		},
	}
	err := installClickHooks(mockHooksDir, instDir, manifest)
	c.Assert(err, IsNil)
	p := fmt.Sprintf("%s/%s_%s_%s", testSymlinkDir, manifest.Name, "app", manifest.Version)
	_, err = os.Stat(p)
	c.Assert(err, IsNil)
	symlinkTarget, err := filepath.EvalSymlinks(p)
	c.Assert(err, IsNil)
	c.Assert(symlinkTarget, Equals, path.Join(instDir, "path-to-systemd-file"))

	p = fmt.Sprintf("%s/%s_%s_%s", testSymlinkDir2, manifest.Name, "app", manifest.Version)
	_, err = os.Stat(p)
	c.Assert(err, IsNil)
	symlinkTarget, err = filepath.EvalSymlinks(p)
	c.Assert(err, IsNil)
	c.Assert(symlinkTarget, Equals, path.Join(instDir, "path-to-apparmor-file"))

	// now ensure we can remove
	err = removeClickHooks(mockHooksDir, manifest)
	c.Assert(err, IsNil)
	_, err = os.Stat(fmt.Sprintf("%s/%s_%s_%s", testSymlinkDir, manifest.Name, "app", manifest.Version))
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestLocalSnapInstall(c *C) {
	runDebsigVerify = func(snapFile string, allowUnauth bool) (err error) {
		return nil
	}

	snapFile := s.makeTestSnap(c)
	targetDir := path.Join(s.tempdir, "apps")
	err := installClick(snapFile, targetDir, false)
	c.Assert(err, IsNil)

	contentFile := path.Join(s.tempdir, "apps", "foo", "1.0", "bin", "foo")
	content, err := ioutil.ReadFile(contentFile)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "#!/bin/sh\necho \"hello\"")

	// ensure we have the manifest too
	_, err = os.Stat(path.Join(s.tempdir, "apps", "foo", "1.0", ".click", "info", "foo.manifest"))
	c.Assert(err, IsNil)
}

func (s *SnapTestSuite) TestLocalSnapInstallDebsigVerifyFails(c *C) {
	runDebsigVerify = func(snapFile string, allowUnauth bool) (err error) {
		return errors.New("something went wrong")
	}

	snapFile := s.makeTestSnap(c)
	targetDir := path.Join(s.tempdir, "apps")
	err := installClick(snapFile, targetDir, false)
	c.Assert(err, NotNil)

	contentFile := path.Join(s.tempdir, "apps", "foo", "1.0", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSnapRemove(c *C) {
	runDebsigVerify = func(snapFile string, allowUnauth bool) (err error) {
		return nil
	}

	targetDir := path.Join(s.tempdir, "apps")
	err := installClick(s.makeTestSnap(c), targetDir, false)
	c.Assert(err, IsNil)

	instDir := path.Join(targetDir, "foo", "1.0")
	_, err = os.Stat(instDir)
	c.Assert(err, IsNil)

	err = removeClick(instDir)
	c.Assert(err, IsNil)

	_, err = os.Stat(instDir)
	c.Assert(err, NotNil)
}
