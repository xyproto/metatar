#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import fileinput
import sys
import os.path
import fnmatch

def get_skiplist_globlist():
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
    return skiplist, globlist

def skipped(word, skiplist, globlist):
    """Return skipcount for the given word"""

    skipcount = 0

    if word in skiplist:
        print(word + " is skipped")
        skipcount += 1
    for skipfilename in skiplist:
        if word == skipfilename + "/":
            print("{} is skipped, for this path: {}".format(word, skipfilename))
            skipcount += 1
            break
        if word == "/" + skipfilename:
            print("{} is skipped, for this path: {}".format(word, skipfilename))
            skipcount += 1
            break
        if word == "/" + skipfilename + "/":
            print("{} is skipped, for this path: {}".format(word, skipfilename))
            skipcount += 1
            break
        if word == os.path.basename(skipfilename):
            if skipfilename == os.path.basename(skipfilename):
                break
            print("{} is skipped, for this path: {}".format(word, skipfilename))
            skipcount += 1
            break
    for globexpr in globlist:
        if fnmatch.fnmatch(word, globexpr) or fnmatch.fnmatch(word, "/" + globexpr):
            print("{} is skipped, it matches the glob expression: {}".format(word, globexpr))
            skipcount += 1
            break

    # --- Not skipped ---

    if skipcount == 0:

        similarcount = 0
    
        # Check for similar skips when the file is not skipped
        for skipfilename in skiplist:
            if os.path.basename(word) == os.path.basename(skipfilename):
                if os.path.basename(skipfilename) == "":
                    continue
                print("{} is not skipped, but this path is similar: {}".format(word, skipfilename))
                similarcount += 1
                break
        for globexpr in globlist:
            if os.path.basename(globexpr) == "*":
                continue
            if fnmatch.fnmatch(os.path.basename(word), os.path.basename(globexpr)):
                print("{} is not skipped, but this glob expression is similar: {}".format(word, globexpr))
                similarcount += 1
                break

    if skipcount == 0:
        print(word, "is not skipped")

    return skipcount

def main():
    skiplist, globlist = get_skiplist_globlist()

    for filename in skiplist:
        if skipped(filename, skiplist, globlist) > 1:
            print("Duplicate skip! ", filename)
            sys.exit(1)

    print("\nNo duplicate skips. :)")

if __name__ == "__main__":
    main()
