import yaml
import sys

with open(sys.argv[1], 'r') as f:
    data = yaml.safe_load(f)

for entry in data['volumes']['pc']['structure']:
    if entry.get('role') == 'system-seed':
        entry['role'] = 'system-seed-null'
        entry['name'] = 'EFI System partition'
        # TODO make this realistically smaller?
        entry['size'] = '99M'
    if entry.get('role') == 'system-boot':
        # Such that potentially there is space to later slot-in 1200M
        # large ubuntu-seed partition
        entry['offset'] = '1202M'

with open(sys.argv[1], 'w') as f:
    yaml.dump(data, f)
