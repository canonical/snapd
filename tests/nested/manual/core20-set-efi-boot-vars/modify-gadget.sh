#!/bin/sh

set -eu

USAGE='USAGE: sh modify-gadget.sh GADGET_DIR ARCH {fallback | no-fallback}'

GADGET_DIR="$1"
ARCH="$2"
ARCH_UPPER="$(echo "$ARCH" | tr '[:lower:]' '[:upper:]')"
FALLBACK="$3"

file_exists() {
	filepath="$1"
	if [ -f "$filepath" ]; then
		return 0
	fi
	echo "ERROR: file does not exist: $filepath"
	echo "$USAGE"
	exit 1
}

prepare_boot_csv() {
	if [ -f "${GADGET_DIR}/BOOT${ARCH_UPPER}.CSV" ]; then
		return 0
	elif [ -f "/usr/lib/shim/BOOT${ARCH_UPPER}.CSV" ]; then
		cp "/usr/lib/shim/BOOT${ARCH_UPPER}.CSV" "${GADGET_DIR}/"
		return 0
	fi
	echo "ERROR: neither gadget nor host has boot CSV: BOOT${ARCH_UPPER}.CSV"
	exit 1
}

prepare_fallback() {
	if [ -f "${GADGET_DIR}/fb${ARCH}.efi" ]; then
		return 0
	elif [ -f "${GADGET_DIR}/fb${ARCH}.efi.bak" ]; then
		mv "${GADGET_DIR}/fb${ARCH}.efi.bak" "${GADGET_DIR}/fb${ARCH}.efi"
		return 0
	elif [ -f "/usr/lib/shim/fb${ARCH}.efi" ]; then
		cp "/usr/lib/shim/fb${ARCH}.efi" "${GADGET_DIR}/"
		return 0
	fi
	echo "ERROR: neither gadget nor host has fallback binary: fb${ARCH}.efi"
	exit 1
}

prepare_no_fallback() {
	if [ -f "${GADGET_DIR}/fb${ARCH}.efi" ]; then
		mv "${GADGET_DIR}/fb${ARCH}.efi" "${GADGET_DIR}/fb${ARCH}.efi.bak"
	fi
}

if ! [ -d "$GADGET_DIR" ]; then
	echo "ERROR: unpacked gadget directory not found: $GADGET_DIR"
	echo "$USAGE"
	exit 1
fi

file_exists "${GADGET_DIR}/shim.efi.signed" || exit 1
file_exists "${GADGET_DIR}/grub${ARCH}.efi" || exit 1
file_exists "${GADGET_DIR}/meta/gadget.yaml" || exit 1

command -v yq > /dev/null || sudo snap install yq || snap install yq

if ! [ -f "${GADGET_DIR}/meta/gadget.yaml.bak" ]; then
	cp "${GADGET_DIR}/meta/gadget.yaml" "${GADGET_DIR}/meta/gadget.yaml.bak" || exit 1
fi

case "$FALLBACK" in
	"fallback")
		prepare_boot_csv || exit 1
		prepare_fallback || exit 1
		yq -i '(.volumes.pc.structure[] | select(.role == "system-seed") | .content) |= [
			{"source": "BOOT'"$ARCH_UPPER"'.CSV", "target": "EFI/ubuntu/BOOT'"$ARCH_UPPER"'.CSV"},
			{"source": "grub'"$ARCH"'.efi",       "target": "EFI/ubuntu/grub'"$ARCH"'.efi"},
			{"source": "shim.efi.signed",         "target": "EFI/ubuntu/shim'"$ARCH"'.efi"},
			{"source": "shim.efi.signed",         "target": "EFI/boot/boot'"$ARCH"'.efi"},
			{"source": "fb'"$ARCH"'.efi",         "target": "EFI/boot/fb'"$ARCH"'.efi"}
		]' "${GADGET_DIR}/meta/gadget.yaml"
		;;
	"no-fallback")
		prepare_no_fallback || exit 1
		yq -i '(.volumes.pc.structure[] | select(.role == "system-seed") | .content) |= [
			{"source": "grub'"$ARCH"'.efi", "target": "EFI/boot/grub'"$ARCH"'.efi"},
			{"source": "shim.efi.signed",   "target": "EFI/boot/boot'"$ARCH"'.efi"}
		]' "${GADGET_DIR}/meta/gadget.yaml"
		;;
	*)
		echo 'ERROR: must specify "fallback" or "no-fallback"'
		echo "$USAGE"
		exit 1
		;;
esac

# Increment edition. If the gadget was previously modified, make sure it is not
# reset to its original state, as this incremented edition must be greater than
# the previous for the new gadget to be installed.
yq -i '(.volumes.pc.structure[] | select(.role == "system-seed") | .update.edition) |= . + 1' \
	pc-gadget/meta/gadget.yaml

