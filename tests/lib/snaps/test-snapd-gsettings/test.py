#! /usr/bin/python
 
from argparse import ArgumentParser
from gi.repository import Gio, GLib


def _get_settings(schema):
    settings = None
    try:
        settings = Gio.Settings.new(schema)
    except Exception as e:
        print('Schema not found: {}'.format(e))
        exit(1)

    return settings


def is_schema(args):
    """ Check if a schema exists """
    schema = None

    if args:
        schema = args[0]
    else:
        print('No schema provided')
        exit(1)

    settings = _get_settings(schema)
    if not settings:
        print('Schema not found')
        exit(1)


def get_value(args):
    """ Print the value """
    if len(args) < 2:
        print('Schema or key not provided')
        exit(1)

    schema = args[0]
    key = args[1]
    value = None
    settings = _get_settings(schema)

    try:
        value = settings.get_value(key)
    except Exception as e:
        print('Error getting value: {}'.format(e))
        exit(1)

    print(value)


def set_value(args):
    """ Print the value """
    if len(args) < 3:
        print('Schema, key or value not provided')
        exit(1)

    schema = args[0]
    key = args[1]
    value = args[2]

    settings = _get_settings(schema)
    try:
        settings.set_value(key, GLib.Variant.parse(None, value, None, None))
    except Exception as e:
        print('Error setting value: {}'.format(e))
        exit(1)


def main():
    args_parser = ArgumentParser("Parse input parameters")
    args_parser.add_argument('command')
    args, remaining_argv = args_parser.parse_known_args()

    if args.command == 'is-schema':
        is_schema(remaining_argv)
    elif args.command == 'get-value':
        get_value(remaining_argv)
    elif args.command == 'set-value':
        set_value(remaining_argv)
    else:
        print('Command not supported: {}'.format(args.command))
        exit(1)


if __name__ == '__main__':
    main()