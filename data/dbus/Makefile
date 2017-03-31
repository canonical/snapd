LIBEXECDIR := /usr/lib
DATADIR := /usr/share

all:
	cat com.canonical.SafeLauncher.service.in | \
		sed s:@libexecdir@:$(LIBEXECDIR):g > com.canonical.SafeLauncher.service

install:
	install -d $(DESTDIR)/$(DATADIR)/dbus-1/services
	install -m 0644 com.canonical.SafeLauncher.service \
		$(DESTDIR)/$(DATADIR)/dbus-1/services

clean:
	rm com.canonical.SafeLauncher.service
