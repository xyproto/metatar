#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import fileinput
import sys
import os.path
import fnmatch

def main():
    try:
        word = sys.argv[1]
    except IndexError:
        print("Usage: ./willskip <filename> <permissions.yml")
        print("Check if the given filename will be skipped or not")
        print("Use -l to list all skipped files and skip-globs")
        sys.exit(1)
    skiplist = []
    globlist = []
    found_skiplist = False
    current_filename = ""
    for line in fileinput.input("-"):
        if line.lower().strip().startswith("skiplist:"):
            found_skiplist = True
            continue
        if line.startswith("- Filename:"):
            current_filename = line[11:].strip()
            continue
        if "skip:true" in line.lower().strip().replace(" ", ""):
            skiplist.append(current_filename)
        if found_skiplist:
            filename_or_glob = line.strip()
            if filename_or_glob.startswith("-"):
                filename_or_glob = filename_or_glob[1:].strip()
            if "*" in filename_or_glob:
                globexpr = filename_or_glob
                if globexpr.startswith('"') and globexpr.endswith('"'):
                    globexpr = globexpr[1:-1]
                if globexpr.startswith("'") and globexpr.endswith("'"):
                    globexpr = globexpr[1:-1]
                globlist.append(globexpr)
            else:
                skiplist.append(filename_or_glob)

    if word == "-l":
        # List the skipped filenames
        print("\n".join(skiplist))
        # List the glob expressions for skipping
        print("\n".join(globlist))
        sys.exit(0)

    # --- Check if skipped ---

    if word in skiplist:
        print(word + " is skipped")
        sys.exit(0)
    for skipfilename in skiplist:
        if word == skipfilename + "/":
            print("{} is skipped, for this path: {}".format(word, skipfilename))
            sys.exit(0)
        if word == "/" + skipfilename:
            print("{} is skipped, for this path: {}".format(word, skipfilename))
            sys.exit(0)
        if word == "/" + skipfilename + "/":
            print("{} is skipped, for this path: {}".format(word, skipfilename))
            sys.exit(0)
        if word == os.path.basename(skipfilename):
            if skipfilename == os.path.basename(skipfilename):
                break
            print("{} is skipped, for this path: {}".format(word, skipfilename))
            sys.exit(0)
    for globexpr in globlist:
        if fnmatch.fnmatch(word, globexpr) or fnmatch.fnmatch(word, "/" + globexpr):
            print("{} is skipped, it matches the glob expression: {}".format(word, globexpr))
            sys.exit(0)

    # --- Not skipped ---

    # Check for similar skips when the file is not skipped
    for skipfilename in skiplist:
        if os.path.basename(skipfilename) == "":
            continue
        if os.path.basename(word) == os.path.basename(skipfilename):
            print("{} is not skipped, but this path is similar: {}".format(word, skipfilename))
            sys.exit(1)
    for globexpr in globlist:
        if os.path.basename(globexpr) == "*":
            continue
        if fnmatch.fnmatch(os.path.basename(word), os.path.basename(globexpr)):
            print("{} is not skipped, but this glob expression is similar: {}".format(word, globexpr))
            sys.exit(1)

    print(word, "is not skipped")
    sys.exit(1)

if __name__ == "__main__":
    main()
