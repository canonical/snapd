# Autopilot mode

Autopilot is a feature in development that will guarantee you are always up to
date. This initial implementation is based out of `systemd` units and disabled
by default.

The usage presented here will become part of the snappy command itself.

## Usage

To verify if the timer is active, run

    systemctl status -l snappy-autopilot.timer

Or for more details of when it is to be triggered can be seen as well by running

    systemctl list-timers snappy-autopilot.timer

If you want to enable it run

    sudo systemctl enable snappy-autopilot.timer
    sudo systemctl start snappy-autopilot.timer

And consequently, do disable it run

    sudo systemctl stop snappy-autopilot.timer
    sudo systemctl disable snappy-autopilot.timer

To only temporarily turn it off just issue `stop` (and omit `disable`).

Every time the timer triggers it will try to update; if an `ubuntu-core` update
is available the system won’t automatically reboot, but the update will take
place.

To inspect the update ran, run

    systemctl status -l snappy-autopilot.service

And to view any output from the command run

    sudo journalctl -u snappy-autopilot.service
