define AMAZONLINUX_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
endef

define AMAZONLINUX_2_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
# Amazon 2 does not implement the power_state cloud-init plugin.
- shutdown --poweroff now
# Pre-install Python3 that is used by our scripts.
packages:
- python3
endef

define ARCHLINUX_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
# enable AppArmor
- sed -i -e 's/GRUB_CMDLINE_LINUX_DEFAULT="\(.*\)"/GRUB_CMDLINE_LINUX_DEFAULT="\1 lsm=landlock,lockdown,yama,integrity,apparmor,bpf"/' /etc/default/grub
- grub-mkconfig -o /boot/grub/grub.cfg
- systemctl enable --now apparmor.service
# Pre-install apparmor on ArchLinux systems.
packages:
- apparmor
endef

define CENTOS_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
endef

define DEBIAN_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
endef

define FEDORA_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
# set selinux to permissive mode
- sed -i 's/SELINUX=.*/SELINUX=permissive/g' /etc/selinux/config
# pre-install wget so that prepare with snapd built from master works
packages:
- wget
endef

define OPENSUSE_tumbleweed_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
# Switch the primary LSM to AppArmor on openSUSE systems.
- sed -i -e 's/security=selinux/security=apparmor/g' /etc/default/grub
- sed -i -e 's/selinux=1//g' /etc/default/grub
- sed -i -e 's/^SELINUX=enforcing/SELINUX=disabled/g' /etc/selinux/config
- update-bootloader
packages:
- apparmor-utils
endef

define OPENSUSE_tumbleweed@selinux_CLOUD_INIT_USER_DATA_TEMPLATE
$(BASE_CLOUD_INIT_USER_DATA_TEMPLATE)
- sed -i -e 's/^SELINUX=enforcing/SELINUX=permissive/g' /etc/selinux/config
endef

# Create an instance of opensuse-cloud-tumbleweed with a SELinux cloud-init
# profile. We provide the OPENSUSE_tumbleweed@selinux_CLOUD_INIT_USER_DATA_TEMPLATE
# above.
$(eval $(call define-instance,opensuse-cloud-tumbleweed.x86_64,selinux))

define UBUNTU_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
# ESM archive doesn't work over plain http and this is not compatible with the
# proxy that image-garden snap is providing for faster testing. The ESM archive
# is not used so this is not a problem.
- rm -f /etc/apt/sources.list.d/ubuntu-esm-infra.list
# Some systems do not have persistent journal, let's fix that.
- mkdir -p /var/log/journal
# Disable upgrades that can lock apt
- sed -i -e 's|^Prompt=.*|Prompt=never|' /etc/update-manager/release-upgrades
- systemctl disable --now apt-daily.timer apt-daily.service apt-daily-upgrade.service apt-daily-upgrade.timer
- systemctl disable --now unattended-upgrades.service
endef

# This is somewhat dense so let's break it down into steps:
#
# - Source the pkgdb.sh script and call the pkg_dependencies function. Along
#   with SPREAD_SYSTEM and TESTSLIB this prints the list of packages to install
#   on a given system. This is normally done in prepare-restore.sh, but by
#   putting it here we avoid constant cost on each iteration, because the
#   booted test image has all of those packages pre-installed.
# - Remove empty lines and leading indentation with awk.
# - Sort the package names to make the output look nicer.
# - Convert sorted package names to a single line separated by spaces.
# - Format the line as a comma-separated list and remove trailing space.
#   This, when used inside square brackets, makes the list valid YAML.
%.packages: $(wildcard $(GARDEN_PROJECT_DIR)/tests/lib/pkgdb.sh) $(GARDEN_PROJECT_DIR)/.image-garden.mk
	PKGDB_DO_NOT_SEARCH_FOR_KERNEL_PACKAGES=1 \
	SPREAD_SYSTEM=$(shell $(GARDEN_PROJECT_DIR)/.image-garden/remap-name garden-to-snapd $*) \
	TESTSLIB=$(GARDEN_PROJECT_DIR)/tests/lib \
		bash -c '. $(GARDEN_PROJECT_DIR)/tests/lib/pkgdb.sh && pkg_dependencies' 2>$@.stderr \
		| awk '/ *[a-zA-Z0-9]+/ { print $$1 }' \
		| sort \
		| tr '\n' ' ' \
		| sed -e 's/ $$/\n/' -e 's/ /, /g' >$@


define ubuntu_cloud_init_magic
# Inject dependency on the .packages file from .user-data file.
# We cannot use pattern rules due to how make works when both pattern and non-pattern rules are used.
ubuntu-cloud-$1.$$(GARDEN_ARCH).user-data: ubuntu-cloud-$1.$$(GARDEN_ARCH).packages

define UBUNTU_$1_CLOUD_INIT_USER_DATA_TEMPLATE
$$(UBUNTU_CLOUD_INIT_USER_DATA_TEMPLATE)
- apt-get install -y linux-image-extra-$$$$(uname -r) || true
- apt-get install -y linux-modules-extra-$$$$(uname -r) || true
- apt-get install -y linux-tools-$$$$(uname -r) || true
packages: [$$(file <ubuntu-cloud-$1.$$(GARDEN_ARCH).packages)]
endef

endef

$(foreach r,16.04 18.04 20.04 22.04 24.04 25.04 25.10,$(eval $(call ubuntu_cloud_init_magic,$r)))

# In the snapd project Ubuntu Core images are built from classic Ubuntu images
# in a somewhat complex manner. Ubuntu Core 16 and 18 kernels do not support
# booting from. Use a quirk to make those systems use SCSI storage instead.
# The quirk is taken directly from image-garden's identical quirk for
# ubuntu-core-16 and ubuntu-core-18 systems.
ubuntu-cloud-16.04.x86_64.qcow2 ubuntu-cloud-16.04.x86_64.run ubuntu-cloud-18.04.x86_64.qcow2 ubuntu-cloud-18.04.x86_64.run: QEMU_ENV_QUIRKS=export QEMU_STORAGE_OPTION="$(strip \
    -drive file=$(1),if=none,format=qcow2,id=drive0,media=disk,cache=writeback,discard=unmap \
    -device virtio-scsi-pci,id=scsi0 \
    -device scsi-hd,drive=drive0,bus=scsi0.0,bootindex=0)";

# include local overrides if present
-include $(GARDEN_PROJECT_DIR)/.image-garden.local.mk
