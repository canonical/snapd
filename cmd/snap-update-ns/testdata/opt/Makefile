# This makefiles uses grouped-target feature and relies on it for correctness.
ifeq (,$(findstring grouped-target,$(.FEATURES)))
$(error You need make with the grouped-taget feature to build this dataset)
endif

fstab_files = \
	      1-initially-connected.current.fstab \
	      1-initially-connected.desired.fstab \
	      2-after-disconnect.desired.fstab \
	      2-after-disconnect.current.fstab \
	      3-after-reconnect.desired.fstab \
	      3-after-reconnect.current.fstab \
	      4-initially-disconnected-then-connected.before.current.fstab \
	      4-initially-disconnected-then-connected.desired.fstab \
	      4-initially-disconnected-then-connected.current.fstab \
	      5-initially-connected-then-content-refreshed.before.current.fstab \
	      5-initially-connected-then-content-refreshed.desired.fstab \
	      5-initially-connected-then-content-refreshed.current.fstab \
	      6-initially-connected-then-app-refreshed.before.current.fstab \
	      6-initially-connected-then-app-refreshed.desired.fstab \
	      6-initially-connected-then-app-refreshed.current.fstab

# None of the fstab files can be built in parallel as the depend on global system state.
.NOTPARALLEL: $(fstab_files)
.PHONY: all
all: $(fstab_files)

.PHONY:
clean:
	rm -f $(fstab_files)
	rm -f *.snap
	rm -f *.mountinfo
	rm -f *.find

$(addprefix 1-initially-connected,.desired.fstab .current.fstab .mountinfo) &: PREFIX=1-initially-connected
$(addprefix 1-initially-connected,.desired.fstab .current.fstab .mountinfo) &: test-snapd-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 2,$^)
	sudo snap connect test-snapd-layout:content test-snapd-content:content
	snap run test-snapd-layout.sh -c true
	cp /var/lib/snapd/mount/snap.test-snapd-layout.fstab $(PREFIX).desired.fstab
	cp /run/snapd/ns/snap.test-snapd-layout.fstab $(PREFIX).current.fstab
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt cat /proc/self/mountinfo >$(PREFIX).mountinfo
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt find /opt >$(PREFIX).opt.find
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content

$(addprefix 2-after-disconnect,.desired.fstab .current.fstab .mountinfo) &: PREFIX=2-after-disconnect
$(addprefix 2-after-disconnect,.desired.fstab .current.fstab .mountinfo) &: test-snapd-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 2,$^)
	sudo snap connect test-snapd-layout:content test-snapd-content:content
	snap run test-snapd-layout.sh -c true
	sudo snap disconnect test-snapd-layout:content test-snapd-content:content
	cp /var/lib/snapd/mount/snap.test-snapd-layout.fstab $(PREFIX).desired.fstab
	cp /run/snapd/ns/snap.test-snapd-layout.fstab $(PREFIX).current.fstab
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt cat /proc/self/mountinfo >$(PREFIX).mountinfo
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt find /opt >$(PREFIX).opt.find
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content

$(addprefix 3-after-reconnect,.desired.fstab .current.fstab .mountinfo) &: PREFIX=3-after-reconnect
$(addprefix 3-after-reconnect,.desired.fstab .current.fstab .mountinfo) &: test-snapd-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 2,$^)
	sudo snap connect test-snapd-layout:content test-snapd-content:content
	snap run test-snapd-layout.sh -c true
	sudo snap disconnect test-snapd-layout:content test-snapd-content:content
	sudo snap connect test-snapd-layout:content test-snapd-content:content
	cp /var/lib/snapd/mount/snap.test-snapd-layout.fstab $(PREFIX).desired.fstab
	cp /run/snapd/ns/snap.test-snapd-layout.fstab $(PREFIX).current.fstab
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt cat /proc/self/mountinfo >$(PREFIX).mountinfo
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt find /opt >$(PREFIX).opt.find
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content

$(addprefix 4-initially-disconnected-then-connected,.before.current.fstab .desired.fstab .current.fstab .mountinfo) &: PREFIX=4-initially-disconnected-then-connected
$(addprefix 4-initially-disconnected-then-connected,.before.current.fstab .desired.fstab .current.fstab .mountinfo) &: test-snapd-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 2,$^)
	snap connections test-snapd-layout | grep -xF 'content    test-snapd-layout:content  -     -'
	snap run test-snapd-layout.sh -c true
	# This file is used by unit tests model the changes needed after the refresh below.
	cp /run/snapd/ns/snap.test-snapd-layout.fstab $(PREFIX).before.current.fstab
	sudo snap connect test-snapd-layout:content test-snapd-content:content
	cp /var/lib/snapd/mount/snap.test-snapd-layout.fstab $(PREFIX).desired.fstab
	cp /run/snapd/ns/snap.test-snapd-layout.fstab $(PREFIX).current.fstab
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt cat /proc/self/mountinfo >$(PREFIX).mountinfo
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt find /opt >$(PREFIX).opt.find
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content

$(addprefix 5-initially-connected-then-content-refreshed,.before.current.fstab .desired.fstab .current.fstab .mountinfo) &: PREFIX=5-initially-connected-then-content-refreshed
$(addprefix 5-initially-connected-then-content-refreshed,.before.current.fstab .desired.fstab .current.fstab .mountinfo) &: test-snapd-layout_a_all.snap test-snapd-content_a_all.snap test-snapd-content_b_all.snap
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 2,$^)
	sudo snap connect test-snapd-layout:content test-snapd-content:content
	snap run test-snapd-layout.sh -c true
	# This file is used by unit tests model the changes needed after the refresh below.
	cp /run/snapd/ns/snap.test-snapd-layout.fstab $(PREFIX).before.current.fstab
	sudo snap install --dangerous $(word 3,$^) # Refresh the content.
	cp /var/lib/snapd/mount/snap.test-snapd-layout.fstab $(PREFIX).desired.fstab
	cp /run/snapd/ns/snap.test-snapd-layout.fstab $(PREFIX).current.fstab
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt cat /proc/self/mountinfo >$(PREFIX).mountinfo
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt find /snap/test-snapd-content/x1/ >$(PREFIX).content-x1.find
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt find /snap/test-snapd-content/x2/ >$(PREFIX).content-x2.find
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt find /opt >$(PREFIX).opt.find
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content

$(addprefix 6-initially-connected-then-app-refreshed,.before.current.fstab .desired.fstab .current.fstab .mountinfo) &: PREFIX=6-initially-connected-then-app-refreshed
$(addprefix 6-initially-connected-then-app-refreshed,.before.current.fstab .desired.fstab .current.fstab .mountinfo) &: test-snapd-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 2,$^)
	sudo snap connect test-snapd-layout:content test-snapd-content:content
	snap run test-snapd-layout.sh -c true
	# This file is used by unit tests model the changes needed after the refresh below.
	cp /run/snapd/ns/snap.test-snapd-layout.fstab $(PREFIX).before.current.fstab
	sudo snap install --dangerous $(word 1,$^) # Refresh the app.
	cp /var/lib/snapd/mount/snap.test-snapd-layout.fstab $(PREFIX).desired.fstab
	cp /run/snapd/ns/snap.test-snapd-layout.fstab $(PREFIX).current.fstab
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt cat /proc/self/mountinfo >$(PREFIX).mountinfo
	sudo nsenter -m/run/snapd/ns/test-snapd-layout.mnt find /opt >$(PREFIX).opt.find
	sudo snap remove --purge test-snapd-layout
	sudo snap remove --purge test-snapd-content

test-snapd-layout_a_all.snap: test-snapd-layout
	snap pack $<

test-snapd-content_a_all.snap: test-snapd-content-a
	snap pack $<

test-snapd-content_b_all.snap: test-snapd-content-b
	snap pack $<
