UDEVDIR := /lib/udev/rules.d

.PHONY: all
all:

.PHONY: install
install:
	install -Dm0644 -t $(DESTDIR)$(UDEVDIR) rules.d/50-snapd-loop-devices.rules

.PHONY: clean
clean:
