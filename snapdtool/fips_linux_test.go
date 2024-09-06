package snapdtool_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

type fipsSuite struct {
	testutil.BaseTest

	logbuf logger.MockedLogger
}

var _ = Suite(&fipsSuite{})

func (s *fipsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	os.Setenv("SNAPD_DEBUG", "1")
	s.AddCleanup(func() { os.Unsetenv("SNAPD_DEBUG") })

	buf, restore := logger.MockLogger()
	s.AddCleanup(restore)
	s.logbuf = buf

	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.AddCleanup(snapdtool.MockSyscallExec(func(argv0 string, argv []string, envv []string) (err error) {
		c.Fatal("exec not mocked")
		return fmt.Errorf("exec not mocked")
	}))
}

func (s *fipsSuite) TearDownTest(c *C) {
	if c.Failed() {
		c.Logf("logs:\n%s", s.logbuf.String())
	}
	s.BaseTest.TearDownTest(c)
}

func mockFipsEnabledWithContent(c *C, root, content string) {
	f := filepath.Join(root, "/proc/sys/crypto/fips_enabled")
	err := os.MkdirAll(filepath.Dir(f), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(f, []byte(content), 0444)
	c.Assert(err, IsNil)
}

type fipsConf struct {
	fipsEnabledPresent bool
	fipsEnabledYes     bool
	moduleAvaialble    bool
	onCore             bool
}

func (s *fipsSuite) mockFIPSState(c *C, conf fipsConf) (selfExe string) {
	if conf.fipsEnabledPresent {
		content := "0\n"
		if conf.fipsEnabledYes {
			content = "1\n"
		}
		mockFipsEnabledWithContent(c, dirs.GlobalRootDir, content)
	}

	mockSelfExe := filepath.Join(dirs.SnapMountDir, "snapd/123/usr/lib/snapd/snapd")
	if conf.onCore {
		mockSelfExe = filepath.Join(dirs.DistroLibExecDir, "snapd")
	}
	restore := snapdtool.MockOsReadlink(func(p string) (string, error) {
		switch {
		case p == "/snap/snapd/current":
			return "123", nil
		case strings.HasSuffix(p, "/proc/self/exe"):
			return mockSelfExe, nil
		}
		return "", fmt.Errorf("unexpected path %q", p)
	})
	s.AddCleanup(restore)

	if conf.moduleAvaialble {
		// even on Core modules are still part of the snapd snap
		snaptest.PopulateDir(filepath.Join(dirs.SnapMountDir, "snapd/123"), [][]string{
			{"usr/lib/x86_64-linux-gnu/libcrypto.so.3", ""},
			{"usr/lib/x86_64-linux-gnu/ossl-modules-3/fips.so", ""},
		})
	}

	return mockSelfExe
}

func (s *fipsSuite) TestMaybeSetupFIPSFullWithReexecClassic(c *C) {
	// everything is set up correctly

	mockSelfExe := s.mockFIPSState(c, fipsConf{
		fipsEnabledPresent: true,
		fipsEnabledYes:     true,
		moduleAvaialble:    true,
		onCore:             false,
	})
	osArgs := os.Args
	s.AddCleanup(func() { os.Args = osArgs })
	os.Args = []string{"--arg"}

	var observedEnv []string
	var observedArgv []string
	var observedArg0 string

	restore := snapdtool.MockSyscallExec(func(argv0 string, argv []string, envv []string) (err error) {
		observedArg0 = argv0
		observedArgv = argv
		observedEnv = envv
		return fmt.Errorf("exec in tests on classic")
	})
	s.AddCleanup(restore)

	c.Check(snapdtool.MaybeSetupFIPS, PanicMatches, "exec in tests on classic")

	c.Check(observedArg0, Equals, mockSelfExe)
	c.Check(observedArgv, DeepEquals, []string{"--arg"})
	// FIPS mode is required
	c.Check(observedEnv, testutil.Contains, "GOFIPS=1")
	// module was found, and relevant env was added
	c.Check(observedEnv, testutil.Contains,
		"OPENSSL_MODULES="+filepath.Join(dirs.SnapMountDir, "snapd/123/usr/lib/x86_64-linux-gnu/ossl-modules-3"))
	c.Check(observedEnv, testutil.Contains, "GO_OPENSSL_VERSION_OVERRIDE=3")
	// bootstrap done
	c.Check(observedEnv, testutil.Contains, "SNAPD_FIPS_BOOTSTRAP=1")
}

func (s *fipsSuite) TestMaybeSetupFIPSFullWithReexecCore(c *C) {
	// everything is set up correctly

	s.AddCleanup(release.MockOnClassic(false))

	mockSelfExe := s.mockFIPSState(c, fipsConf{
		fipsEnabledPresent: true,
		fipsEnabledYes:     true,
		moduleAvaialble:    true,
		onCore:             true,
	})
	osArgs := os.Args
	s.AddCleanup(func() { os.Args = osArgs })
	os.Args = []string{"--arg"}

	var observedEnv []string
	var observedArgv []string
	var observedArg0 string

	restore := snapdtool.MockSyscallExec(func(argv0 string, argv []string, envv []string) (err error) {
		observedArg0 = argv0
		observedArgv = argv
		observedEnv = envv
		return fmt.Errorf("exec in tests on core")
	})
	s.AddCleanup(restore)

	c.Check(snapdtool.MaybeSetupFIPS, PanicMatches, "exec in tests on core")

	c.Check(observedArg0, Equals, mockSelfExe)
	c.Check(observedArgv, DeepEquals, []string{"--arg"})
	// FIPS mode is required
	c.Check(observedEnv, testutil.Contains, "GOFIPS=1")
	// module was found, and relevant env was added, and still points to the
	// snapd snap mount directory
	c.Check(observedEnv, testutil.Contains,
		"OPENSSL_MODULES="+filepath.Join(dirs.SnapMountDir, "snapd/123/usr/lib/x86_64-linux-gnu/ossl-modules-3"))
	c.Check(observedEnv, testutil.Contains, "GO_OPENSSL_VERSION_OVERRIDE=3")
	// bootstrap done
	c.Check(observedEnv, testutil.Contains, "SNAPD_FIPS_BOOTSTRAP=1")
}

func (s *fipsSuite) TestMaybeSetupFIPSNoModulesButStillReexec(c *C) {
	// FIPS is enabled, we do not find the module, but still reexec into
	// mandatory FIPS mode to obtain an predictable error from FIPS
	// initialization

	mockSelfExe := s.mockFIPSState(c, fipsConf{
		fipsEnabledPresent: true,
		fipsEnabledYes:     true,
		moduleAvaialble:    false,
	})

	var observedEnv []string
	var observedArgv []string
	var observedArg0 string

	osArgs := os.Args
	s.AddCleanup(func() { os.Args = osArgs })
	os.Args = []string{"--arg"}

	restore := snapdtool.MockSyscallExec(func(argv0 string, argv []string, envv []string) (err error) {
		observedArg0 = argv0
		observedArgv = argv
		observedEnv = envv
		return fmt.Errorf("exec in tests")
	})
	s.AddCleanup(restore)

	c.Check(snapdtool.MaybeSetupFIPS, PanicMatches, "exec in tests")

	c.Check(observedArg0, Equals, mockSelfExe)
	c.Check(observedArgv, DeepEquals, []string{"--arg"})
	// FIPS mode is erquired
	c.Check(observedEnv, testutil.Contains, "GOFIPS=1")
	// module was not found, so paths are not set
	for _, env := range observedEnv {
		if strings.HasPrefix(env, "OPENSSL_MODULES=") || strings.HasPrefix(env, "GO_OPENSSL_VERSION_OVERRIDE=") {
			c.Fatalf("found unexpected env %q", env)
		}
	}
	// bootstrap is done
	c.Check(observedEnv, testutil.Contains, "SNAPD_FIPS_BOOTSTRAP=1")
}

func (s *fipsSuite) TestMaybeSetupFIPSBootstrapAlreadyDone(c *C) {
	// bootstrap was already completed

	s.mockFIPSState(c, fipsConf{
		fipsEnabledPresent: true,
		fipsEnabledYes:     true,
	})

	defer func() {
		os.Unsetenv("GOFIPS")
		os.Unsetenv("SNAPD_FIPS_BOOSTRAP")
		os.Unsetenv("OPENSSL_MODULES")
		os.Unsetenv("GO_OPENSSL_VERSION_OVERRIDE")
	}()

	os.Setenv("SNAPD_FIPS_BOOTSTRAP", "1")
	os.Setenv("GOFIPS", "1")
	os.Setenv("OPENSSL_MODULES", "bogus-dir")
	os.Setenv("GO_OPENSSL_VERSION_OVERRIDE", "123-xyz")

	err := snapdtool.MaybeSetupFIPS()
	c.Assert(err, IsNil)

	c.Check(os.Getenv("SNAPD_FIPS_BOOTSTRAP"), Equals, "")
	c.Check(os.Getenv("GOFIPS"), Equals, "")
	c.Check(os.Getenv("OPENSSL_MODULES"), Equals, "")
	c.Check(os.Getenv("GO_OPENSSL_VERSION_OVERRIDE"), Equals, "")
}

func (s *fipsSuite) TestMaybeSetupFIPSSnapdNotFromSnapOnClassic(c *C) {
	// FIPS is enabled, but snapd is not running from the snapd snap

	restore := snapdtool.MockOsReadlink(func(p string) (string, error) {
		switch {
		case p == "/snap/snapd/current":
			return "123", nil
		case strings.HasSuffix(p, "/proc/self/exe"):
			return filepath.Join(dirs.DistroLibExecDir, "snapd"), nil
		}
		return "", fmt.Errorf("unexpected path %q", p)
	})
	s.AddCleanup(restore)

	restore = snapdtool.MockSyscallExec(func(argv0 string, argv []string, envv []string) (err error) {
		return fmt.Errorf("exec in tests")
	})
	s.AddCleanup(restore)

	err := snapdtool.MaybeSetupFIPS()
	c.Assert(err, IsNil)
}
