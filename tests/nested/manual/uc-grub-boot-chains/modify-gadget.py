import argparse
import yaml

parser = argparse.ArgumentParser()
parser.add_argument('edition_bump', type=int)
# Type of media:
# * A bootable removable media will have boot/bootx64.efi and boot/grubx64.efi.
#   It will not register boot entries in UEFI variables.
# * A bootable installed system (boot_entry) will have boot/bootx64.efi and boot/fbx64.efi as fallback.
#   It will also have <distro>/shimx64.efi and <distro>/grubx64.efi.
#   This will work with UEFI variables for boot entries.
# For more information see:
# See https://github.com/rhboot/shim/blob/66e6579dbf921152f647a0c16da1d3b2f40861ca/README.fallback
parser.add_argument('type', choices=['removable', 'boot_entry'])
parser.add_argument('gadget_yaml', type=argparse.FileType('r', encoding='utf-8'))
parser.add_argument('output', type=argparse.FileType('w', encoding='utf-8'))
args = parser.parse_args()

content = yaml.safe_load(args.gadget_yaml)
for partition in content['volumes']['pc']['structure']:
    if partition.get('role') == 'system-seed':
        partition['update']['edition'] += args.edition_bump
        if args.type == 'removable':
            partition['content'] = [
                {'source': 'grubx64.efi',
                'target': 'EFI/boot/grubx64.efi'},
                {'source': 'shim.efi.signed',
                 'target': 'EFI/boot/bootx64.efi'},
            ]
        elif args.type == 'boot_entry':
            partition['content'] = [
                {'source': 'grubx64.efi',
                 'target': 'EFI/ubuntu/grubx64.efi'},
                {'source': 'shim.efi.signed',
                 'target': 'EFI/ubuntu/shimx64.efi'},
                {'source': 'boot.csv',
                 'target': 'EFI/ubuntu/bootx64.csv'},
                {'source': 'fb.efi',
                 'target': 'EFI/boot/fbx64.efi'},
                {'source': 'shim.efi.signed',
                 'target': 'EFI/boot/bootx64.efi'},
            ]
    elif partition.get('role') == 'system-boot':
        partition['update']['edition'] += args.edition_bump

yaml.safe_dump(content, stream=args.output)
