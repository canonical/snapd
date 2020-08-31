package devicestate_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
)

type cloudInitSuite struct {
	deviceMgrBaseSuite
	mockLogger *bytes.Buffer
}

var _ = Suite(&cloudInitSuite{})

func (s *cloudInitSuite) SetUpTest(c *C) {
	s.deviceMgrBaseSuite.SetUpTest(c)

	// undo the cloud-init mocking from deviceMgrBaseSuite, since here we
	// actually want the default function used to be the real one
	s.restoreCloudInitStatusRestore()

	r := release.MockOnClassic(false)
	defer r()

	st := s.o.State()
	st.Lock()
	st.Set("seeded", true)
	st.Unlock()

	logbuf, r := logger.MockLogger()
	s.mockLogger = logbuf
	s.AddCleanup(r)

	// mock /etc/cloud on writable
	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc", "cloud"), 0755)
	c.Assert(err, IsNil)
}

func (s *cloudInitSuite) TestClassicCloudInitDoesNothing(c *C) {
	r := release.MockOnClassic(true)
	defer r()

	r = devicestate.MockCloudInitStatus(func() (sysconfig.CloudInitState, error) {
		c.Error("EnsureCloudInitRestricted should not have checked cloud-init status when on classic")
		return 0, fmt.Errorf("broken")
	})
	defer r()

	r = devicestate.MockRestrictCloudInit(func(sysconfig.CloudInitState, *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		c.Error("EnsureCloudInitRestricted should not have restricted cloud-init when on classic")
		return sysconfig.CloudInitRestrictionResult{}, fmt.Errorf("broken")
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)
}

func (s *cloudInitSuite) TestCloudInitEnsureBeforeSeededDoesNothing(c *C) {
	st := s.o.State()
	st.Lock()
	st.Set("seeded", false)
	st.Unlock()

	r := devicestate.MockCloudInitStatus(func() (sysconfig.CloudInitState, error) {
		c.Error("EnsureCloudInitRestricted should not have checked cloud-init status when not seeded")
		return 0, fmt.Errorf("broken")
	})
	defer r()

	r = devicestate.MockRestrictCloudInit(func(sysconfig.CloudInitState, *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		c.Error("EnsureCloudInitRestricted should not have restricted cloud-init when not seeded")
		return sysconfig.CloudInitRestrictionResult{}, fmt.Errorf("broken")
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)
}

func (s *cloudInitSuite) TestCloudInitAlreadyEnsuredRestrictedDoesNothing(c *C) {
	n := 0

	// mock that it was restricted so that we set the internal bool to say it
	// already ran
	r := devicestate.MockCloudInitStatus(func() (sysconfig.CloudInitState, error) {
		n++
		switch n {
		case 1:
			return sysconfig.CloudInitRestrictedBySnapd, nil
		default:
			c.Error("EnsureCloudInitRestricted should not have checked cloud-init status again")
			return sysconfig.CloudInitRestrictedBySnapd, fmt.Errorf("test broken")
		}
	})
	defer r()

	// run it once to set the internal bool
	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	c.Assert(n, Equals, 1)

	// it should run again without checking anything
	err = devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	c.Assert(n, Equals, 1)
}

func (s *cloudInitSuite) TestCloudInitDeviceManagerEnsureRestrictsCloudInit(c *C) {
	st := s.o.State()
	st.Lock()
	// avoid device registration
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Serial: "123",
	})
	st.Unlock()
	n := 0

	// mock that it was restricted so that we set the internal bool to say it
	// already ran
	r := devicestate.MockCloudInitStatus(func() (sysconfig.CloudInitState, error) {
		n++
		switch n {
		case 1:
			return sysconfig.CloudInitRestrictedBySnapd, nil
		default:
			c.Error("EnsureCloudInitRestricted should not have checked cloud-init status again")
			return sysconfig.CloudInitRestrictedBySnapd, fmt.Errorf("test broken")
		}
	})
	defer r()

	// run it once to set the internal bool
	err := s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)

	// running again is still okay and won't call CloudInitStatus again
	err = s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
}

func (s *cloudInitSuite) TestCloudInitAlreadyRestrictedDoesNothing(c *C) {
	r := devicestate.MockCloudInitStatus(func() (sysconfig.CloudInitState, error) {
		return sysconfig.CloudInitRestrictedBySnapd, nil
	})
	defer r()

	r = devicestate.MockRestrictCloudInit(func(sysconfig.CloudInitState, *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		c.Error("EnsureCloudInitRestricted should not have restricted cloud-init when already restricted")
		return sysconfig.CloudInitRestrictionResult{}, fmt.Errorf("broken")
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)
}

func (s *cloudInitSuite) TestCloudInitAlreadyRestrictedFileDoesNothing(c *C) {
	// write a cloud-init restriction file
	disableFile := filepath.Join(dirs.GlobalRootDir, "/etc/cloud/cloud.cfg.d/zzzz_snapd.cfg")
	err := os.MkdirAll(filepath.Dir(disableFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(disableFile, nil, 0644)
	c.Assert(err, IsNil)

	// mock cloud-init command, but make it always fail, it shouldn't be called
	// as cloud-init.disabled should tell sysconfig to never consult cloud-init
	// directly
	cmd := testutil.MockCommand(c, "cloud-init", `
echo "unexpected call to cloud-init with args $*"
exit 1`)
	defer cmd.Restore()

	r := devicestate.MockRestrictCloudInit(func(sysconfig.CloudInitState, *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		c.Error("EnsureCloudInitRestricted should not have restricted cloud-init when already disabled")
		return sysconfig.CloudInitRestrictionResult{}, fmt.Errorf("broken")
	})
	defer r()

	err = devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	c.Assert(s.mockLogger.String(), Equals, "")

	c.Assert(cmd.Calls(), HasLen, 0)
}

func (s *cloudInitSuite) TestCloudInitAlreadyDisabledDoesNothing(c *C) {
	// the absence of a zzzz_snapd.cfg file will indicate that it has not been
	// restricted yet and thus it should then check to see if it was manually
	// disabled

	// write a cloud-init disabled file
	disableFile := filepath.Join(dirs.GlobalRootDir, "/etc/cloud/cloud-init.disabled")
	err := os.MkdirAll(filepath.Dir(disableFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(disableFile, nil, 0644)
	c.Assert(err, IsNil)

	// mock cloud-init command, but make it always fail, it shouldn't be called
	// as cloud-init.disabled should tell sysconfig to never consult cloud-init
	// directly
	cmd := testutil.MockCommand(c, "cloud-init", `
echo "unexpected call to cloud-init with args $*"
exit 1`)
	defer cmd.Restore()

	r := devicestate.MockRestrictCloudInit(func(sysconfig.CloudInitState, *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		c.Error("EnsureCloudInitRestricted should not have restricted cloud-init when already disabled")
		return sysconfig.CloudInitRestrictionResult{}, fmt.Errorf("broken")
	})
	defer r()

	err = devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	c.Assert(s.mockLogger.String(), Equals, "")

	c.Assert(cmd.Calls(), HasLen, 0)
}

func (s *cloudInitSuite) TestCloudInitUntriggeredDisables(c *C) {
	// the absence of a zzzz_snapd.cfg file will indicate that it has not been
	// restricted yet and thus it should then check to see if it was manually
	// disabled

	// the absence of a cloud-init.disabled file indicates that cloud-init is
	// "untriggered", i.e. not active/running but could still be triggered

	cmd := testutil.MockCommand(c, "cloud-init", `
if [ "$1" = "status" ]; then
	echo "status: disabled"
else
	echo "unexpected args $*"
	exit 1
fi`)
	defer cmd.Restore()

	restrictCalls := 0

	r := devicestate.MockRestrictCloudInit(func(state sysconfig.CloudInitState, opts *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		restrictCalls++
		c.Assert(state, Equals, sysconfig.CloudInitUntriggered)
		c.Assert(opts, Not(IsNil))
		c.Assert(opts.ForceDisable, Equals, false)
		// we would have disabled it
		return sysconfig.CloudInitRestrictionResult{Action: "disable"}, nil
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
	})

	// a message about cloud-init done and being restricted
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init reported to be in disabled state, disabled permanently.*`)

	c.Assert(restrictCalls, Equals, 1)
}

func (s *cloudInitSuite) TestCloudInitDoneRestricts(c *C) {
	// the absence of a zzzz_snapd.cfg file will indicate that it has not been
	// restricted yet and thus it should then check to see if it was manually
	// disabled

	// the absence of a cloud-init.disabled file indicates that cloud-init is
	// "untriggered", i.e. not active/running but could still be triggered

	cmd := testutil.MockCommand(c, "cloud-init", `
if [ "$1" = "status" ]; then
	echo "status: done"
else
	echo "unexpected args $*"
	exit 1
fi`)
	defer cmd.Restore()

	restrictCalls := 0

	r := devicestate.MockRestrictCloudInit(func(state sysconfig.CloudInitState, opts *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		restrictCalls++
		c.Assert(state, Equals, sysconfig.CloudInitDone)
		c.Assert(opts, Not(IsNil))
		c.Assert(opts.ForceDisable, Equals, false)
		// we would have restricted it since it ran
		return sysconfig.CloudInitRestrictionResult{
			// pretend it was NoCloud
			DataSource: "NoCloud",
			Action:     "restrict",
		}, nil
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
	})

	// a message about cloud-init done and being restricted
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init reported to be done, set datasource_list to \[ NoCloud \] and disabled auto-import by filesystem label.*`)

	// and 1 call to restrict
	c.Assert(restrictCalls, Equals, 1)
}

func (s *cloudInitSuite) TestCloudInitDoneProperCloudRestricts(c *C) {
	// the absence of a zzzz_snapd.cfg file will indicate that it has not been
	// restricted yet and thus it should then check to see if it was manually
	// disabled

	// the absence of a cloud-init.disabled file indicates that cloud-init is
	// "untriggered", i.e. not active/running but could still be triggered

	cmd := testutil.MockCommand(c, "cloud-init", `
if [ "$1" = "status" ]; then
	echo "status: done"
else
	echo "unexpected args $*"
	exit 1
fi`)
	defer cmd.Restore()

	restrictCalls := 0

	r := devicestate.MockRestrictCloudInit(func(state sysconfig.CloudInitState, opts *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		restrictCalls++
		c.Assert(state, Equals, sysconfig.CloudInitDone)
		c.Assert(opts, Not(IsNil))
		c.Assert(opts.ForceDisable, Equals, false)
		// we would have restricted it since it ran
		return sysconfig.CloudInitRestrictionResult{
			// pretend it was GCE
			DataSource: "GCE",
			Action:     "restrict",
		}, nil
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
	})

	// a message about cloud-init done and being restricted
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init reported to be done, set datasource_list to \[ GCE \].*`)

	// only called restrict once
	c.Assert(restrictCalls, Equals, 1)
}

func (s *cloudInitSuite) TestCloudInitRunningEnsuresUntilNotRunning(c *C) {
	// the absence of a zzzz_snapd.cfg file will indicate that it has not been
	// restricted yet and thus it should then check to see if it was manually
	// disabled

	// the absence of a cloud-init.disabled file indicates that cloud-init is
	// "untriggered", i.e. not active/running but could still be triggered

	// we use a file to make the mocked cloud-init act differently depending on
	// how many times it is called
	// this is because we want to test settle()/EnsureBefore() automatically
	// re-triggering the EnsureCloudInitRestricted() w/o changing the script
	// mid-way through the test while settle() is running
	cloudInitScriptStateFile := filepath.Join(c.MkDir(), "cloud-init-state")

	cmd := testutil.MockCommand(c, "cloud-init", fmt.Sprintf(`
# the first time the script is called the file shouldn't exist, so return 
# running
# next time when the file exists, return done
if [ -f %[1]s ]; then
	status="done"
else
	status="running"
	touch %[1]s
fi
if [ "$1" = "status" ]; then
	echo "status: $status"
else
	echo "unexpected args $*"
	exit 1
fi`, cloudInitScriptStateFile))
	defer cmd.Restore()

	restrictCalls := 0

	r := devicestate.MockRestrictCloudInit(func(state sysconfig.CloudInitState, opts *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		restrictCalls++
		c.Assert(state, Equals, sysconfig.CloudInitDone)
		c.Assert(opts, Not(IsNil))
		c.Assert(opts.ForceDisable, Equals, false)
		// we would have restricted it
		return sysconfig.CloudInitRestrictionResult{
			// pretend it was NoCloud
			DataSource: "NoCloud",
			Action:     "restrict",
		}, nil
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	// no log messages while we wait for the transition
	c.Assert(s.mockLogger.String(), Equals, "")

	// should not have called to restrict
	c.Assert(restrictCalls, Equals, 0)

	// only one call to cloud-init status
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
	})

	// we should have had a call to EnsureBefore, so if we now settle, we will
	// see an additional call to cloud-init status, which now returns done and
	// progresses
	s.settle(c)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
		{"cloud-init", "status"},
	})

	// now restrict should have been called
	c.Assert(restrictCalls, Equals, 1)

	// now a message that it was disabled
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init reported to be done, set datasource_list to \[ NoCloud \] and disabled auto-import by filesystem label.*`)
}

func (s *cloudInitSuite) TestCloudInitSteadyErrorDisables(c *C) {
	// the absence of a zzzz_snapd.cfg file will indicate that it has not been
	// restricted yet and thus it should then check to see if it was manually
	// disabled

	// the absence of a cloud-init.disabled file indicates that cloud-init is
	// "untriggered", i.e. not active/running but could still be triggered

	cmd := testutil.MockCommand(c, "cloud-init", `
if [ "$1" = "status" ]; then
	echo "status: error"
else
	echo "unexpected args $*"
	exit 1
fi`)
	defer cmd.Restore()

	restrictCalls := 0

	r := devicestate.MockRestrictCloudInit(func(state sysconfig.CloudInitState, opts *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		restrictCalls++
		c.Assert(state, Equals, sysconfig.CloudInitErrored)
		c.Assert(opts, Not(IsNil))
		c.Assert(opts.ForceDisable, Equals, true)
		// we would have disabled it
		return sysconfig.CloudInitRestrictionResult{
			Action: "disable",
		}, nil
	})
	defer r()

	timeCalls := 0
	testStart := time.Now()

	r = devicestate.MockTimeNow(func() time.Time {
		// we will only call time.Now() three times, first to initialize/set the
		// that we saw cloud-init in error, and another immediately after to
		// check if the 3 minute timeout has elapsed, and then finally after the
		// ensure() call happened 3 minutes later
		timeCalls++
		switch timeCalls {
		case 1, 2:
			// we have 2 calls that happen at first, the first one initializes
			// the time we checked it at, and for code simplicity, another one
			// right after to check if the time elapsed
			// both of these should have the same time for the first call to
			// EnsureCloudInitRestricted
			return testStart
		case 3:
			return testStart.Add(3*time.Minute + 1*time.Second)
		default:
			c.Errorf("unexpected additional call (number %d) to time.Now()", timeCalls)
			return time.Time{}
		}
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	// should not have called restrict
	c.Assert(restrictCalls, Equals, 0)

	// only one call to cloud-init status
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
	})

	// a message about error state for the operator to try to fix
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init reported to be in error state, will disable in 3 minutes.*`)
	s.mockLogger.Reset()

	// make sure the time accounting is correct
	c.Assert(timeCalls, Equals, 2)

	// we should have had a call to EnsureBefore, so if we now settle, we will
	// see an additional call to cloud-init status, which continues to return
	// error and then disables cloud-init
	s.settle(c)

	// make sure the time accounting is correct after the ensure - one more
	// check which was simulated to be 3 minutes later
	c.Assert(timeCalls, Equals, 3)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
		{"cloud-init", "status"},
	})

	// now restrict should have been called
	c.Assert(restrictCalls, Equals, 1)

	// and a new message about being disabled permanently
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init reported to be in error state after 3 minutes, disabled permanently.*`)
}

func (s *cloudInitSuite) TestCloudInitSteadyErrorDisablesFasterEnsure(c *C) {
	// the absence of a zzzz_snapd.cfg file will indicate that it has not been
	// restricted yet and thus it should then check to see if it was manually
	// disabled

	// the absence of a cloud-init.disabled file indicates that cloud-init is
	// "untriggered", i.e. not active/running but could still be triggered

	cmd := testutil.MockCommand(c, "cloud-init", `
if [ "$1" = "status" ]; then
	echo "status: error"
else
	echo "unexpected args $*"
	exit 1
fi`)
	defer cmd.Restore()

	restrictCalls := 0

	r := devicestate.MockRestrictCloudInit(func(state sysconfig.CloudInitState, opts *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		restrictCalls++
		c.Assert(state, Equals, sysconfig.CloudInitErrored)
		c.Assert(opts, Not(IsNil))
		c.Assert(opts.ForceDisable, Equals, true)
		// we would have disabled it
		return sysconfig.CloudInitRestrictionResult{
			Action: "disable",
		}, nil
	})
	defer r()

	timeCalls := 0
	testStart := time.Now()

	r = devicestate.MockTimeNow(func() time.Time {
		// we will only call time.Now() three times, first to initialize/set the
		// that we saw cloud-init in error, and another immediately after to
		// check if the 3 minute timeout has elapsed, and then a few odd times
		// before hitting the timeout to ensure we don't print the log message
		// unnecessarily and that the timeout logic works
		timeCalls++
		switch timeCalls {
		case 1, 2:
			// we have 2 calls that happen at first, the first one initializes
			// the time we checked it at, and for code simplicity, another one
			// right after to check if the time elapsed
			// both of these should have the same time for the first call to
			// EnsureCloudInitRestricted
			return testStart
		case 3:
			// only 1 minute elapsed
			return testStart.Add(1 * time.Minute)
		case 4:
			// only 1 minute elapsed
			return testStart.Add(1*time.Minute + 30*time.Second)
		case 5:
			// now we hit the timeout
			return testStart.Add(3*time.Minute + 1*time.Second)
		default:
			c.Errorf("unexpected additional call (number %d) to time.Now()", timeCalls)
			return time.Time{}
		}
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	// should not have called restrict
	c.Assert(restrictCalls, Equals, 0)

	// only one call to cloud-init status
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
	})

	// a message about error state for the operator to try to fix
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init reported to be in error state, will disable in 3 minutes.*`)
	s.mockLogger.Reset()

	// make sure the time accounting is correct
	c.Assert(timeCalls, Equals, 2)

	// we should have had a call to EnsureBefore, so if we now settle, we will
	// see an additional call to cloud-init status, which continues to return
	// error and then disables cloud-init
	s.settle(c)

	// make sure the time accounting is correct after the ensure - one more
	// check which was simulated to be 3 minutes later
	c.Assert(timeCalls, Equals, 5)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
		{"cloud-init", "status"},
		{"cloud-init", "status"},
		{"cloud-init", "status"},
	})

	// now restrict should have been called
	c.Assert(restrictCalls, Equals, 1)

	// and a new message about being disabled permanently
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init reported to be in error state after 3 minutes, disabled permanently.*`)
}

func (s *cloudInitSuite) TestCloudInitTakingTooLongDisables(c *C) {
	// the absence of a zzzz_snapd.cfg file will indicate that it has not been
	// restricted yet and thus it should then check to see if it was manually
	// disabled

	// the absence of a cloud-init.disabled file indicates that cloud-init is
	// "untriggered", i.e. not active/running but could still be triggered

	cmd := testutil.MockCommand(c, "cloud-init", `
if [ "$1" = "status" ]; then
	echo "status: running"
else
	echo "unexpected args $*"
	exit 1
fi`)
	defer cmd.Restore()

	restrictCalls := 0

	r := devicestate.MockRestrictCloudInit(func(state sysconfig.CloudInitState, opts *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		restrictCalls++
		c.Assert(state, Equals, sysconfig.CloudInitEnabled)
		c.Assert(opts, Not(IsNil))
		c.Assert(opts.ForceDisable, Equals, true)
		// we would have disabled it
		return sysconfig.CloudInitRestrictionResult{
			Action: "disable",
		}, nil
	})
	defer r()

	timeCalls := 0
	testStart := time.Now()

	r = devicestate.MockTimeNow(func() time.Time {
		timeCalls++
		switch {
		case timeCalls == 1 || timeCalls == 2:
			// we have 2 calls that happen at first, the first one initializes
			// the time we checked it at, and for code simplicity, another one
			// right after to check if the time elapsed
			// both of these should have the same time for the first call to
			// EnsureCloudInitRestricted
			return testStart
		case timeCalls > 2 && timeCalls <= 31:
			// 31 here because we should do 30 checks plus one initially
			return testStart.Add(time.Duration(timeCalls*10) * time.Second)
		default:
			c.Errorf("unexpected additional call (number %d) to time.Now()", timeCalls)
			return time.Time{}
		}
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	// should not have called to disable
	c.Assert(restrictCalls, Equals, 0)

	// only one call to cloud-init status
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
	})

	// make sure our time accounting is still correct
	c.Assert(timeCalls, Equals, 2)

	// no messages while it waits until the timeout
	c.Assert(s.mockLogger.String(), Equals, ``)

	// we should have had a call to EnsureBefore, so if we now settle, we will
	// see additional calls to cloud-init status, which continues to always
	// return an error and so we eventually give up and disable it anyways
	s.settle(c)

	// make sure our time accounting is still correct
	c.Assert(timeCalls, Equals, 31)

	// should have called cloud-init status 30 times
	calls := make([][]string, 30)
	for i := 0; i < 30; i++ {
		calls[i] = []string{"cloud-init", "status"}
	}

	c.Assert(cmd.Calls(), DeepEquals, calls)

	// now disable should have been called
	c.Assert(restrictCalls, Equals, 1)

	// now a message after we timeout waiting for the transition
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init failed to transition to done or error state after 5 minutes, disabled permanently.*`)
}

func (s *cloudInitSuite) TestCloudInitTakingTooLongDisablesFasterEnsures(c *C) {
	// same test as TestCloudInitTakingTooLongDisables, but with a faster
	// re-ensure cycle to ensure that if we get scheduled to run Ensure() sooner
	// than expected everything still works

	// the absence of a zzzz_snapd.cfg file will indicate that it has not been
	// restricted yet and thus it should then check to see if it was manually
	// disabled

	// the absence of a cloud-init.disabled file indicates that cloud-init is
	// "untriggered", i.e. not active/running but could still be triggered

	cmd := testutil.MockCommand(c, "cloud-init", `
if [ "$1" = "status" ]; then
	echo "status: running"
else
	echo "unexpected args $*"
	exit 1
fi`)
	defer cmd.Restore()

	restrictCalls := 0

	r := devicestate.MockRestrictCloudInit(func(state sysconfig.CloudInitState, opts *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		restrictCalls++
		c.Assert(state, Equals, sysconfig.CloudInitEnabled)
		c.Assert(opts, Not(IsNil))
		c.Assert(opts.ForceDisable, Equals, true)
		// we would have disabled it
		return sysconfig.CloudInitRestrictionResult{
			Action: "disable",
		}, nil
	})
	defer r()

	timeCalls := 0
	testStart := time.Now()

	r = devicestate.MockTimeNow(func() time.Time {
		timeCalls++
		switch {
		case timeCalls == 1 || timeCalls == 2:
			// we have 2 calls that happen at first, the first one initializes
			// the time we checked it at, and for code simplicity, another one
			// right after to check if the time elapsed
			// both of these should have the same time for the first call to
			// EnsureCloudInitRestricted
			return testStart
		case timeCalls > 2 && timeCalls <= 61:
			// 31 here because we should do 60 checks plus one initially
			return testStart.Add(time.Duration(timeCalls*5) * time.Second)
		default:
			c.Errorf("unexpected additional call (number %d) to time.Now()", timeCalls)
			return time.Time{}
		}
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	// should not have called to disable
	c.Assert(restrictCalls, Equals, 0)

	// only one call to cloud-init status
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
	})

	// make sure our time accounting is still correct
	c.Assert(timeCalls, Equals, 2)

	// no messages while it waits until the timeout
	c.Assert(s.mockLogger.String(), Equals, ``)

	// we should have had a call to EnsureBefore, so if we now settle, we will
	// see additional calls to cloud-init status, which continues to always
	// return an error and so we eventually give up and disable it anyways
	s.settle(c)

	// make sure our time accounting is still correct
	c.Assert(timeCalls, Equals, 61)

	// should have called cloud-init status 60 times
	calls := make([][]string, 60)
	for i := 0; i < 60; i++ {
		calls[i] = []string{"cloud-init", "status"}
	}

	c.Assert(cmd.Calls(), DeepEquals, calls)

	// now disable should have been called
	c.Assert(restrictCalls, Equals, 1)

	// now a message after we timeout waiting for the transition
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init failed to transition to done or error state after 5 minutes, disabled permanently.*`)
}

func (s *cloudInitSuite) TestCloudInitErrorOnceAllowsFixing(c *C) {
	// the absence of a zzzz_snapd.cfg file will indicate that it has not been
	// restricted yet and thus it should then check to see if it was manually
	// disabled

	// the absence of a cloud-init.disabled file indicates that cloud-init is
	// "untriggered", i.e. not active/running but could still be triggered

	// we use a file to make the mocked cloud-init act differently depending on
	// how many times it is called
	// this is because we want to test settle()/EnsureBefore() automatically
	// re-triggering the EnsureCloudInitRestricted() w/o changing the script
	// mid-way through the test while settle() is running
	cloudInitScriptStateFile := filepath.Join(c.MkDir(), "cloud-init-state")

	cmd := testutil.MockCommand(c, "cloud-init", fmt.Sprintf(`
# the first time the script is called the file shouldn't exist, so return error
# next time when the file exists, return done
if [ -f %[1]s ]; then
	status="done"
else
	status="error"
	touch %[1]s
fi
if [ "$1" = "status" ]; then
	echo "status: $status"
else
	echo "unexpected args $*"
	exit 1
fi`, cloudInitScriptStateFile))
	defer cmd.Restore()

	restrictCalls := 0

	r := devicestate.MockRestrictCloudInit(func(state sysconfig.CloudInitState, opts *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error) {
		restrictCalls++
		c.Assert(state, Equals, sysconfig.CloudInitDone)
		c.Assert(opts, Not(IsNil))
		c.Assert(opts.ForceDisable, Equals, false)
		// we would have restricted it
		return sysconfig.CloudInitRestrictionResult{
			Action: "restrict",
			// pretend it was NoCloud
			DataSource: "NoCloud",
		}, nil
	})
	defer r()

	timeCalls := 0
	testStart := time.Now()
	r = devicestate.MockTimeNow(func() time.Time {
		// we should only call time.Now() twice, first to initialize/set the
		// that we saw cloud-init in error, and another immediately after to
		// check if the 3 minute timeout has elapsed
		timeCalls++
		switch timeCalls {
		case 1, 2:
			return testStart
		default:
			c.Errorf("unexpected additional call (number %d) to time.Now()", timeCalls)
			return time.Time{}
		}
	})
	defer r()

	err := devicestate.EnsureCloudInitRestricted(s.mgr)
	c.Assert(err, IsNil)

	// should not have called to restrict
	c.Assert(restrictCalls, Equals, 0)

	// make sure our time accounting is still correct
	c.Assert(timeCalls, Equals, 2)

	// only one call to cloud-init status
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
	})

	// a message about being in error
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init reported to be in error state, will disable in 3 minutes`)
	s.mockLogger.Reset()

	// we should have had a call to EnsureBefore, so if we now settle, we will
	// see an additional call to cloud-init status, which now returns done and
	// progresses
	s.settle(c)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
		{"cloud-init", "status"},
	})

	// make sure our time accounting is still correct
	c.Assert(timeCalls, Equals, 2)

	// now restrict should have been called
	c.Assert(restrictCalls, Equals, 1)

	// we now have a message about restricting
	c.Assert(strings.TrimSpace(s.mockLogger.String()), Matches, `.*System initialized, cloud-init reported to be done, set datasource_list to \[ NoCloud \] and disabled auto-import by filesystem label`)
}
