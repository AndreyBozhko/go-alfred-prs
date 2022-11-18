import argparse
import os
import sys


def do_find(args):
    pwd = os.environ["GIT_TOKEN"]
    print(f'password: "{pwd}"', file=sys.stderr)


def do_add(args):
    pass


parser = argparse.ArgumentParser(prog="security")
subparser = parser.add_subparsers()

parser1 = subparser.add_parser("find-generic-password")
parser1.add_argument("-a", type=str)
parser1.add_argument("-s", type=str)
parser1.add_argument("-g", action="store_true")
parser1.set_defaults(func=do_find)

parser2 = subparser.add_parser("add-generic-password")
parser2.add_argument("-a", type=str)
parser2.add_argument("-s", type=str)
parser2.add_argument("-w", type=str)
parser2.set_defaults(func=do_add)


if __name__ == "__main__":
    args = parser.parse_args()
    args.func(args)
