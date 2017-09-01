#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import fileinput
import sys

def main():
    try:
        word = sys.argv[1]
    except IndexError:
        print("Usage: ./skip <word> <permissions.yml >permissions2.yml")
        print("Filenames containing the word will have Skip added in the output")
        print("Example: ./skip.py amigamouse1 <permissions.yml >permissions2.yml; mv -f permissions2.yml permissions.yml; git diff permissions.yml")
        sys.exit(1)
    s = ""
    justskipped = False
    for line in fileinput.input("-"):
        if line.startswith("- Filename:"):
            s += line
            if word in line:
                s += "  Skip: true\n"
                justskipped = True
            else:
                justskipped = False
        elif line.strip().startswith("Skip:") and justskipped:
            continue
        else:
            s += line
            justskipped = False
    print(s, end="")

if __name__ == "__main__":
    main()
