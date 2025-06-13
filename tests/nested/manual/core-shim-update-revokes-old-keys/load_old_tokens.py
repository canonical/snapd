import argparse
import io
import json
import subprocess


def load_json(disk):
    raw = subprocess.run(['cryptsetup', 'luksDump', '--dump-json-metadata', disk], capture_output=True)
    return json.loads(raw.stdout)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--read',
                        action='store_true')
    parser.add_argument('--dry-run',
                        action='store_true')
    parser.add_argument('disk')
    parser.add_argument('dump')
    args = parser.parse_args()

    if args.read:
        data = load_json(args.disk)
        with open(args.dump, 'w') as f:
            json.dump(data, f)
    else:
        with open(args.dump, 'r') as f:
            backup = json.load(f)
        current = load_json(args.disk)
        for k, _ in current['tokens'].items():
            cmd = ['cryptsetup', 'token', 'remove', '--token-id', k, args.disk]
            if args.dry_run:
                print(' '.join(cmd))
            else:
                subprocess.run(cmd)
        for k, v in backup['tokens'].items():
            token_raw = json.dumps(v)
            cmd = ['cryptsetup', 'token', 'import', '--token-id', k, args.disk]
            if args.dry_run:
                print(' '.join(cmd))
                print(token_raw)
            else:
                p = subprocess.Popen(cmd, stdin=subprocess.PIPE)
                p.communicate(token_raw.encode('utf-8'))


if __name__ == '__main__':
    main()
