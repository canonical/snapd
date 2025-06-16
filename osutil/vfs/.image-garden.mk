# For compatibility with spread install bash.
define ALPINE_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
- sed -i -E -e 's/GRUB_TIMEOUT=[0-9]+/GRUB_TIMEOUT=0/' /etc/default/grub
- grub-mkconfig -o /boot/grub/grub.cfg
packages:
- bash
endef
