#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import fileinput
import sys

def main():
    s = ""
    found_filename = True
    for line in fileinput.input("-"):
        if line.lower().strip().startswith("skip: true"):
            s += line
            found_filename = False
        elif line.lower().strip().startswith("- filename:"):
            found_filename = True
            s += line
        elif found_filename:
            s += line
    print(s, end="")

if __name__ == "__main__":
    main()
