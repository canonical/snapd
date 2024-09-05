import json
import sys

doc = json.load(sys.stdin)

slot_to_token = {}
for k, v in doc.get('tokens', {}).items():
    for slot in v.get('keyslots', []):
        slot_to_token[slot] = k

orphans = []
for k, v in doc.get('keyslots', {}).items():
    if k not in slot_to_token:
        orphans.append(k)

if len(orphans) > 0:
    formatted = ', '.join(orphans)
    print(f'orphan key slots: {formatted}', file=sys.stderr)
    sys.exit(1)
else:
    sys.exit(0)
