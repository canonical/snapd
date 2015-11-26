# Autopilot

*Autopilot* is a feature that will guarantee you are always up to
date. It is enabled by default, and can be disabled via `snappy config`.

## Usage

To check whether the feature is active, run

    snappy config ubuntu-core | grep autopilot

If you want to disable it run

    echo 'config: {ubuntu-core: {autopilot: off}}' | sudo snappy config ubuntu-core -

and you then re-enable it via

    echo 'config: {ubuntu-core: {autopilot: on}}' | sudo snappy config ubuntu-core -

Every time autopilot triggers it will try to update the whole system;
if an `ubuntu-core` update is available the system will automatically
reboot, although a message is printed to console with instructions on
how to abort the reboot, in case you are logged in at the time.

> *Autopilot* is going to be renamed to *autoupdate* after `15.04`, as the name
> *autopilot* can be confusing.

## Implementation details

For more details of when it is to be triggered you could dig into the
implementation, via

    systemctl list-timers snappy-autopilot.timer

To check whether the update ran, run

    systemctl status -l snappy-autopilot.service

and to view any output from the command run

    sudo journalctl -u snappy-autopilot.service
