# Integration testing for snappy

The integration tests for snappy are written using
[gocheck](https://labix.org/gocheck) and executed with `adt-run` through `ssh`.

The `integration-tests/main.go` file takes care of executing the tests. By
default, it will also provision a snappy virtual machine with `ssh` enabled,
and it will pass the IP of this testbed to `adt-run`. If you want to provision
the testbed on your own or need to run the tests in a remote machine already
provisioned, you can specify the IP address and port as explained in the
*Testing a remote machine* section.

## Running the tests

### Requirements

 *  autopkgtest (>= 3.15.1)

    Get the latest autopkgtest deb from
    https://packages.debian.org/sid/all/autopkgtest/download

 *  Internet access in the test bed.

 *  (Optional) subunit, to display nice test results in the terminal:

        sudo apt-get install subunit

### Setting up the project

First you need to set up the `GOPATH`, get the snappy sources and the
dependencies as explained in the `README.md` that is located at the root of the
branch.

### Testing a virtual machine

You can execute the full integration suite in a local virtual machine with:

    go run integration-tests/main.go

The test runner will create the snappy images with `ubuntu-device-flash`, so it
will ask for your password to run this command with `sudo`.

You can also especify more options to customize the image being created, including
the `release`, the `channel` and the `revision` to use. This parameters will be passed
to `ubuntu-device-flash`:

    go run integration-tests/main.go -release 15.04 -channel stable -revision 3

The default values are suited to testing the most recent version, *rolling* for
`release`, *edge* for `channel` and an empty `revision`, which picks the latest
available.

### Testing snappy from a branch

With the `--snappy-from-branch` flag, the snappy CLI command will be compiled
from the current branch, copied to the test bed and used during the integration
tests:

    go run integration-tests/main.go --snappy-from-branch

You can use this flag to test in a remote machine too.

### Filtering the tests to run

With the `--filter` flag you can select the tests to run. For instance you can
pass *MyTestSuite*, *MyTestSuite.FirstCustomTest* or *MyTestSuite.\*CustomTest*:

    go run integration-tests/main.go --filter MyTestSuite.FirstCustomTest

### Testing a remote machine

You can execute the integration suite in a remote snappy machine with:

    go run integration-tests/main.go --ip {testbed-ip} --port {testbed-port} \
    --arch {testbed-arch}

When running in a remote machine, the test runner assumes the test bed is in
the latest *rolling edge* version, and it will skip all the tests that
require a different version. See the following section for instructions for
setting up a BeagleBone Black as the test bed.

### Testing a BeagleBone Black

First flash the latest *rolling edge* version into the sd card
(replacing `/dev/sdX` with the path to your card):

    sudo ubuntu-device-flash core rolling --channel edge --gadget beagleblack \
    --developer-mode --enable-ssh -o ubuntu-rolling-edge-armhf-bbb.img

    sudo dd if=ubuntu-rolling-edge-armhf-bbb.img of=/dev/sdX bs=32M
    sync

Then boot the board with the sd card, make sure that it is connected to the
same network as the test runner host, and find the *{beaglebone-ip}*.

Run the tests with:

    go run integration-tests/main.go --ip {beaglebone-ip} --arch arm

### Testing an update

With the `--update` flag you can flash an old image, update to the latest and
then run the whole suite on the updated system. The `release`, the `channel` and
the `revision` flags specify the image that will be flashed. There must be an
update available for the flashed image.

For example, to update from *rolling edge -1* to the latest and then run the
integration tests:

    go run integration-tests/main.go --snappy-from-branch \
    --revision=-1 --update

### Testing a rollback

With the `--rollback` flag you can flash an old image, update to the latest,
rollback again to the old image and then run the whole suite on the rolled
back system. You should use the `release`, `channel` and `revision` flags as
when testing an update.

For example, to test a rollback from latest *rolling edge* to *rolling edge -1*:

    go run integration-tests/main.go \
    --revision=-1 --rollback

## Writing a new test

To write a new test, create a file named `{something}_test.go` in the
`integration-tests/tests` directory. Replace `{something}` with a useful
identifier of what you want to test.

On this file make sure to use the build tag:

    // +build !excludeintegration

We use this tag to exclude the integration tests from the unit test runs.

The test suite must extend the common integration suite:

    var _ = check.Suite(&somethingSuite{})

    type somethingSuite struct {
	    common.SnappySuite
    }

If for some reason you need to do something on the set up or tear down of the
test, *always* call the methods of the common suite:

    func (s *somethingSuite) SetUpTest(c *check.C) {
	    s.SnappySuite.SetUpTest(c)
        ...
    }

    func (s *somethingSuite) TearDownTest(c *check.C) {
        ...
	    s.SnappySuite.TearDownTest(c)
    }

### Tests with reboots

`adt-run` supports reboots during tests. A test can request a reboot specifying
a mark to identify it; adt-run backs up all the test files and the results so
far, reboots the test bed, restores the files and results and reruns the test.
So a test that requires a reboot is run twice. The test needs to check the
reboot mark to know if it must run the steps that go before the reboot, or the
steps that go after the reboot.

We write the integration tests in Go and compile a binary for the whole suite.
The downside of this is that the whole suite is run before a reboot, and then it
is run again after the reboot. We rely on the order of the suite between reboots
being always the same, and use the adt reboot mark to identify which test
requested the reboot. With this we can know which tests already ran, which is
the test that needs to be resumed after the reboot, and which tests are still
pending to run. We then use *subunit* to merge the results of all the executions
of the suite.

All of this is implemented in the `SetUpTest` method of the common Suite, so as
long as you are not overwriting it or you are calling it from the overwritten
methods (as explained in a previous section), the suite should properly handle
the reboots.

In order to tell your test to run a different set of steps before and after the
reboot, write it like this:

    func (s *somethingSuite) TestSomething(c *check.C) {
        if common.BeforeReboot() {
            ...
            common.Reboot(c)
        } else if common.AfterReboot(c) {
            common.RemoveRebootMark(c)
            ...
        }
    }

*Always* make sure that your test is removing the mark once it handled the
reboot.

Some test beds don't support reboots, so the tests that require a reboot
must use the build tag:

    // +build !excludeintegration,!excludereboots

This allows to exclude the tests that require a reboot by passing `excludereboots`
to the `test-build-tags` flag:

    go run integration-tests/main.go -test-build-tags=excludereboots

### Update-rollback stress test

While validating a new image before release one of the key aspects tested is the ability
to upgrade from a previous version and later rollback to the original state, this way we
can assure that existing users' systems won't break after the new image is out and they
try the upgrade. It is also useful to do the complete upgrade-rollback cycle more than once
so that we can be sure that after any rollback the system is still able to update to the
latest version.

Because of the nature of this kind of tests, they need some initial state to be present in the
system before executing, the image must be updatable. For that reason, they can not be executed
with the complete suite, the other tests don't make any assumption, and this test is guarded
by the `rollbackstress` build tag. Taking into account both requirements and assuming that you
have an updatable system running on ip `<updatable_ip>` with ssh listening on port `<updatable_port>`,
this tests can be executed with:

    go run integration-tests/main.go -ip <updatable_ip> -port <updatable_port> \
        -test-build-tags=rollbackstress

### Excluding flaky tests on low-performance systems

We have found that some tests give random results on low-performance systems. You can
exclude these tests by passing `lowperformance` to the `test-build-tags` flag:

    go run integration-tests/main.go -test-build-tags=lowperformance

### Classic-only tests

There are certain integration tests which at the moment only work in classic ubuntu systems, for
instance the unity suite, which checks features that currently are only available in desktop systems.

These tests are guarded by the `classiconly` build tag, the autopkgtest runner is configured to
include it when building the tests' binary. If you develop a test of this type remember to add it at
the top of the file:

```
// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,classiconly
...
```
