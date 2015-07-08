# Integration testing for snappy

## Requirements

 *  autopkgtest (>= 3.15.1)

    Get the latest autopkgtest deb from
    https://packages.debian.org/sid/all/autopkgtest/download

 *  Internet access in the test bed.

## Testing a virtual machine

You can execute the full integration suite in a local virtual machine with:

    go run _integration-test/main.go

The test runner will create the snappy images with `ubuntu-device-flash`, so it
will ask for your password to run this command with `sudo`.

## Testing snappy from a branch

With the --snappy-from-branch flag, the snappy CLI command will be compiled
from the current branch, copied to the test bed and used during the integration
tests:

    go run _integration-tests/main.go --snappy-from-branch

You can use this flag to test in a remote machine too.

## Filtering the tests to run

With the --filter flag you can select the tests to run. For instance you can
pass MyTestSuite, MyTestSuite.FirstCustomTest or MyTestSuite.*CustomTest:

    go run _integration-tests/main.go --filter MyTestSuite.FirstCustomTest

## Testing a remote machine

You can execute the integration suite in a remote snappy machine with:

    go run _integration-test/main.go --ip {testbed-ip} --port {testbed-port} \
    --arch {testbed-arch}

The test runner will use `ssh-copy-id` to send your identity file to the
testbed, so it will ask for the password of the ubuntu user in the test bed.

When running in a remote machine, the test runner assumes the test bed is in
the latest rolling edge version, and it will skip all the tests that
require a different version. See the following section for instructions for
setting up a BeagleBone Black as the test bed.

## Testing a BeagleBone Black

First flash the latest rolling edge version into the sd card
(replacing /dev/sdX with the path to your card):

    sudo ubuntu-device-flash core rolling --channel edge --oem beagleblack \
    --developer-mode --enable-ssh -o ubuntu-rolling-edge-armhf-bbb.img

    sudo dd if=ubuntu-rolling-edge-armhf-bbb.img of=/dev/sdX bs=32M
    sync

Then boot the board with the sd card, make sure that it is connected to the
same network as the test runner host, and find the {beaglebone-ip}.

Run the tests with:

    go run _integration-tests/main.go --ip {beaglebone-ip} --arch arm
