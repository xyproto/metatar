#!/bin/bash
set -e
scriptdir=$(dirname $0)
export LC_ALL=POSIX

./metatar --version 2>/dev/null || (go build; ./metatar --version)

# YAML -> TAR
echo -ne 'Testing tar file generation:\tYAML -> TAR...\t\t'
./metatar -g "$scriptdir/examples/hello.yml" hello.tar && gzip hello.tar
tar zxf hello.tar.gz
rm -f hello.tar.gz
message=$(cat hello.txt)
greeting='Hello, World!'
if [[ $message == $greeting ]]; then echo ok; else echo FAIL; exit 1; fi
rm -f hello.txt

# TAR -> YAML
echo -ne 'Testing metadata extraction:\tTAR -> YAML...\t\t'
./metatar -g "$scriptdir/examples/hello.yml" hello.tar
./metatar -s hello.tar hello.yml
grep -q 'hello.txt' hello.yml && echo ok || (echo FAIL; exit 1)

# TAR + YAML -> TAR
echo -ne 'Testing metadata application:\tTAR + YAML -> TAR...\t'
echo '  Rename: hi.txt' >> hello.yml
./metatar -a hello.tar hello.yml hello2.tar
rm -f hello.tar hello.yml
tar xf hello2.tar
rm -f hello2.tar
if [[ -f hi.txt ]]; then echo ok; else echo FAIL; exit 1; fi
rm -f hi.txt

# TAR -> YAML then TAR + YAML -> CPIO w/ symlink
echo -ne 'Testing CPIO linkname:\t\tTAR + YAML -> CPIO...\t'
mkdir -p "$scriptdir/ost"
echo hi > "$scriptdir/ost/hi.txt"
ln -s "$scriptdir/ost/hi.txt" "$scriptdir/ost/greeting"
tar cf "$scriptdir/ost.tar" "$scriptdir/ost/"
rm -rf "$scriptdir/ost"
./metatar -s -d -e -f "$scriptdir/ost.tar" "$scriptdir/ost.yml"
./metatar -a -c -f "$scriptdir/ost.tar" "$scriptdir/ost.yml" "$scriptdir/ost.cpio"
rm -f "$scriptdir/ost.tar" "$scriptdir/ost.yml" 
linkname=$(./metatar -p "$scriptdir/ost.cpio" | grep Linkname | cut -d" " -f2)
rm -f "$scriptdir/ost.cpio"
if [[ $linkname != './ost/hi.txt' ]]; then
  echo "Error: Linkname is not ./ost/hi.txt but: $linkname"
  exit 1
else
  echo ok
fi

# Handling files that are not included in the YAML file (the "hi" script)
echo -e 'Testing CPIO + missing file:\tTAR + YAML -> CPIO...\t'
./metatar -v -a -c -f "$scriptdir/examples/rename.tar" "$scriptdir/examples/missing.yml" "$scriptdir/examples/missing.cpio"
rm -rf "$scriptdir/test"
cpio -i -F "$scriptdir/examples/missing.cpio" 2>/dev/null
if [[ -f "$scriptdir/test/bin/hi" ]]; then
  echo ok
else
  echo "Error: file not listed in YAML metadata (test/bin/hi) is missing"
  exit 1
fi
rm -rf "$scriptdir/test" "$scriptdir/examples/missing.cpio"

# Renaming directories with CPIO
echo -e 'Testing CPIO + rename dir:\tTAR + YAML -> CPIO...\n'
./metatar -a -c -f -v "$scriptdir/examples/rename.tar" "$scriptdir/examples/rename.yml" "$scriptdir/examples/rename.cpio"
rm -rf "$scriptdir/test"
cpio -i -F "$scriptdir/examples/rename.cpio" 2>/dev/null
metatar -p "$scriptdir/examples/rename.cpio"
output=$($scriptdir/test/bin/hi)
rm -rf "$scriptdir/test" "$scriptdir/examples/rename.cpio"
if [[ $output == "hi" ]]; then
  echo ok
else
  echo "Error: could not run hi script"
  exit 1
fi


