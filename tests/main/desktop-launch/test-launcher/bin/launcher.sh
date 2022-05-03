#!/bin/sh

# Run the command from the .desktop file provided by our test app.
# This should automatically be run through the privileged desktop launcher.
desktopfile=/var/lib/snapd/desktop/applications/test-app_test-app.desktop
command=$(grep '^Exec=' $desktopfile | sed 's/^Exec=//g')
exec $command

