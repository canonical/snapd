SNAPD_ENVIRONMENT_FILE := /etc/environment
SNAP_MOUNTDIR := /snap
BINDIR := /usr/bin
LIBEXECDIR := /usr/lib

all:
	# Generate the real systemd units out of the available templates
	cat snapd.service.in | \
		sed s:@libexecdir@:${LIBEXECDIR}:g | \
		sed s:@SNAPD_ENVIRONMENT_FILE@:${SNAPD_ENVIRONMENT_FILE}:g > snapd.service
	cat 	snapd.refresh.service.in | \
		sed s:@bindir@:${BINDIR}:g | \
		sed s:@SNAP_MOUNTDIR@:${SNAP_MOUNTDIR}:g > snapd.refresh.service
	cat snapd.autoimport.service.in | \
		sed s:@bindir@:${BINDIR}:g > snapd.autoimport.service
