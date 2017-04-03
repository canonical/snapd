SNAPD_ENVIRONMENT_FILE := /etc/environment
SNAP_MOUNTDIR := /snap
BINDIR := /usr/bin
LIBEXECDIR := /usr/lib
SYSTEMDSYSTEMUNITDIR := /lib/systemd/system

SYSTEMD_UNITS := $(patsubst %.service.in,%.service,$(wildcard *.service.in))

%.service: %.service.in
	cat $< | \
		sed s:@libexecdir@:${LIBEXECDIR}:g | \
		sed s:@SNAPD_ENVIRONMENT_FILE@:${SNAPD_ENVIRONMENT_FILE}:g | \
		sed s:@bindir@:${BINDIR}:g | \
		sed s:@SNAP_MOUNTDIR@:${SNAP_MOUNTDIR}:g  | \
		cat > $@
	install -m 0644

install: $(SYSTEMD_UNITS)
	install -d ${DESTDIR}/${SYSTEMDSYSTEMUNITDIR}
	install -m 0644 snapd.refresh.timer ${DESTDIR}/${SYSTEMDSYSTEMUNITDIR}
	install -m 0644 snapd.refresh.service ${DESTDIR}/${SYSTEMDSYSTEMUNITDIR}
	install -m 0644 snapd.autoimport.service ${DESTDIR}/${SYSTEMDSYSTEMUNITDIR}
	install -m 0644 *.socket ${DESTDIR}/${SYSTEMDSYSTEMUNITDIR}
	install -m 0644 snapd.service ${DESTDIR}/${SYSTEMDSYSTEMUNITDIR}
	install -m 0644 snapd.system-shutdown.service ${DESTDIR}/${SYSTEMDSYSTEMUNITDIR}
