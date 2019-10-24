#!/bin/sh -ex
echo "Install a test service"
snap pack test-snapd-service

# Because this service is of type "simple" it is considered "ready" instantly.
# In reality the process needs to go through snap "run" chain to be really
# ready. As a workaround, touch a "remove-me" file that is removed by the
# service on startup, restart the service and the wait for the file to
# disappear.
mkdir -p /var/snap/test-snapd-service/common
touch /var/snap/test-snapd-service/common/remove-me
snap install --dangerous ./test-snapd-service_1.0_all.snap
# Wait for the service to really be alive and running. Otherwise the "main pid"
# will be still tracking snap-run-confine-exec chain and be unreliable.
for _ in $(seq 5); do
	if [ ! -e /var/snap/test-snapd-service/common/remove-me ]; then
		break
	fi
	sleep 1
done

echo "Extract the PID of the main process tracked by systemd"
# It would be nicer to use "systemctl show --property=... --value" but it doesn't work on older systemd.
pid=$(systemctl show snap.test-snapd-service.test-snapd-service.service --property=ExecMainPID | cut -d = -f 2)

echo "Extract the device cgroup of the main process"
initial_device_cgroup=$(grep devices < "/proc/$pid/cgroup" | cut -d : -f 3)

# Initially, because there are no udev tags corresponding to this application,
# snap-confine is not moving the process to a new cgroup. As such the service
# runs in the cgroup created by systemd, which varies across version of systemd.
case "$initial_device_cgroup" in
    /)
        ;;
    /system.slice)
        ;;
    /system.slice/snap.test-snapd-service.test-snapd-service.service)
        ;;
    *)
        echo "Unexpected initial device cgroup: $initial_device_cgroup"
        exit 1
esac

echo "Ensure that the claim of the process is consistent with the claim of the cgroup"
# This is just a sanity check.
MATCH "$pid" < "/sys/fs/cgroup/devices/$initial_device_cgroup/cgroup.procs"

echo "Verify the constraints imposed by the device cgroup made by systemd"
# This may change over time as it is governed by systemd.
test 'a *:* rwm' = "$(cat "/sys/fs/cgroup/devices/$initial_device_cgroup/devices.list")"

echo "Connect the joystick interface"
snap connect test-snapd-service:joystick

echo "Refresh the value of the main pid and the effective device cgroup after snap connect"
# NOTE: As of snapd 2.40 the PID and cgroup are expected to be the same as before.
pid_check=$(systemctl show snap.test-snapd-service.test-snapd-service.service --property=ExecMainPID | cut -d = -f 2)
test "$pid" -eq "$pid_check"
updated_device_cgroup=$(grep devices < "/proc/$pid/cgroup" | cut -d : -f 3)

echo "Verify that the main process is still in the systemd-made cgroup"
test "$updated_device_cgroup" = "$initial_device_cgroup"

echo "Verify the constraints imposed by the device cgroup made by systemd"
test 'a *:* rwm' = "$(cat "/sys/fs/cgroup/devices/$updated_device_cgroup/devices.list")"

echo "Run /bin/true via snap-confine, so that we create the device cgroup made by snapd"
snap run --shell test-snapd-service -c /bin/true

echo "Verify the constraints imposed by the device cgroup made by snapd"
# NOTE: the actual permissions may drift over time. We just care about the fact
# that there *are* some constraints here now and there were none before.
test 'c 1:3 rwm' = "$(head -n 1 "/sys/fs/cgroup/devices/snap.test-snapd-service.test-snapd-service/devices.list")"
# The device cgroup made by snapd is, currently, still empty.
test -z "$(cat "/sys/fs/cgroup/devices/snap.test-snapd-service.test-snapd-service/cgroup.procs")"

echo "Restart the test service"
# See the comment for the similar code above.
touch /var/snap/test-snapd-service/common/remove-me
systemctl restart snap.test-snapd-service.test-snapd-service.service
for _ in $(seq 5); do
	if [ ! -e /var/snap/test-snapd-service/common/remove-me ]; then
		break
	fi
	sleep 1
done

echo "Refresh the value of the main pid and the effective device cgroup after service restart"
pid=$(systemctl show snap.test-snapd-service.test-snapd-service.service --property=ExecMainPID | cut -d = -f 2)
final_device_cgroup=$(grep devices < "/proc/$pid/cgroup" | cut -d : -f 3)

echo "Verify that the main process is now in the snapd-made cgroup"
test "$final_device_cgroup" = /snap.test-snapd-service.test-snapd-service
