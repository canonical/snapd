# Writing integration tests for snappy

The integration tests for snappy are written using
[gocheck](https://labix.org/gocheck) and executed with `adt-run` through `ssh`.
The `integration-tests/main.go` file takes care of provisioning the testbed and
executing the tests. The `integration-tests/README.md` file explains the
different options to run the tests.

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

## Tests with reboots

`adt-run` supports reboots during tests. A test can request a reboot specifying
a mark to identify it; adt-run backs up all the test files and the results so
far, reboots the test bed, restores the files and results and reruns the test.
So a test that requires a reboot is run twice. The test needs to check the
reboot mark to know if it must run the steps that go before the reboot, or the
steps that go after the reboot.

We write the integration tests in go and compile a binary for the whole suite.
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
