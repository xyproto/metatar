# MetaTAR [![Software License](https://img.shields.io/badge/license-BSD-brightgreen.svg?style=flat-square)](LICENSE) [![Go Report Card](https://goreportcard.com/badge/github.com/xyproto/metatar?style=flat-square)](https://goreportcard.com/report/github.com/xyproto/metatar)

MetaTAR can extract metadata from a tar archive and save it as a YAML file.

The YAML file can then be edited and applied to the original tar archive, in order to produce a new tar archive with changed metadata.

This can be useful for creating filesystem images with device files without having to use fakeroot and mknod.

It's also useful for checking in data about file owners and permissions into git. It makes it easier to compare changes and see who changed what.

It can also be used for converting a tar file to a standalone YAML file and back, using base64 encoding for the file data. This produces large, but highly patchable and diffable files. Use `tar --sort=name` to archive files in sorted order.

When applying metadata, a cpio (newc format) file can be produced instead.

MetaTAR can list the contents of both tar and cpio (newc) files.

Note that CPIO archives are ordered, and that files/directories must exist before relative symlinks for them appear in the archive.

<!--
## Simple example

<a href="https://asciinema.org/a/bmsk91mof9cl9ccra7jc7pcs9"><img src="https://raw.githubusercontent.com/xyproto/metatar/main/img/metatar.gif" style="margin-left: 2em" alt="asciinema screencast"></a>
-->

## Quick installation

If you have a recent version of `go` installed:

    go install github.com/xyproto/metatar@latest

## Usage information


```
metatar

Usage:
  metatar -s | --save [(-f | --force)] [(-v | --verbose)] [(-d | --data)] [(-e | --expand)] [(-r | --root)] <tarfile> <yamlfile>
  metatar -a | --apply [(-f | --force)] [(-v | --verbose)] [(-d | --data)] [(-c | --cpio)] <tarfile> <yamlfile> <newfile>
  metatar -g | --generate [(-f | --force)] [(-v | --verbose)] <yamlfile> <newfile>
  metatar -l | --list <tarfile>
  metatar -p | --listcpio <cpiofile>
  metatar -y | --yaml [(-v | --verbose)] [(-d | --data)] [(-e | --expand)] [(-r | --root)] <tarfile>
  metatar -m | --merge [(-f | --force)] [(-v | --verbose)] <yamlfile1> <yamlfile2> <newfile>
  metatar -h | --help
  metatar -V | --version

Options:
  -h --help        Show this screen.
  -V --version     Show version.
  -s --save        Save the tar metadata to a YAML file.
  -a --apply       Apply YAML metadata to tar file.
  -l --list        List the contents of a tar file.
  -p --listcpio    List the contents of a cpio/newc file.
  -y --yaml        Output YAML metadata.
  -f --force       Overwrite a file if it already exists.
  -v --verbose     More verbose output.
  -d --data        Add file data as base64 encoded strings.
  -e --expand      Expand the metadata to include all possible fields.
  -g --generate    Generate a new tar file from a given YAML file.
  -r --root        Set all permissions to root, UID/GID 0
  -m --merge       Merge two YAML files. Let the second file override the first.
  -c --cpio        Output a cpio/newc file instead of tar.
  -n --nouser      Don't output User, Group, UID and GID fields.
  -o --noskip      Don't skip empty files.

Possible values for the 'type:' field in the YAML file:
  "regular file"		"regular file (A)"			"hard link"
  "symlink"			"character device node"			"block device node"
  "directory"			"fifo node"				"reserved"
  "extended header"		"global extended header"		"sparse file"
  "unknown tar entry"		"next file has a long name"
  "next file symlinks to a file with a long name"

Possible commands for files in the YAML file:
  "Skip: true", for skipping the file when writing the new archive file.
  "Rename: newfilename.txt", for renaming a file.
  "Strip: true", for stripping newlines.
  "StripComments: true", for stripping lines beginning with "#" (but not #!).
```

## Typeflags

When files are stored in a tar archive, an unsigned 8-bit int is used to tell which type of file is being stored.

Possible values for the `Type` field in the YAML file:

* `"regular file"`
* `"regular file (A)"`
* `"hard link"`
* `"symlink"`
* `"character device node"`
* `"block device node"`
* `"directory"`
* `"fifo node"`
* `"reserved"`
* `"extended header"`
* `"global extended header"`
* `"next file has a long name"`
* `"next file symlinks to a file with a long name"`
* `"sparse file" and "unknown tar entry"`


## Merging metadata

If two metadata YAML files are combined to a single file, it's the one at the bottom of the new file that counts.
Previous entries for the same filename are disregarded.

Such a combined file can be applied to a tar file, from which the metadata can be extracted again, to get a merged metadata file.

This is experimental and may be problematic.

## Omitted fields

Several fields in the YAML metadata are omitted, unless an `-e` or `--expand` flag is provided.

### Renaming files

`Rename:` can be used to provide a different filename. Example:

    - Filename: test.txt
      Rename: test2.txt

When applied, `test.txt` from the source archive will be named `test2.txt` in the resulting archive.

### Symbolic links

`Linkname:` can be used to provide a filename that the resulting file will link to. Example:

    - Filename: profile
      Linkname: /etc/profile

When the resulting archive is extracted, the `profile` file will link to `/etc/profile` (soft link).

### Deleting files

`Skip:` can be used to omit a file in the resulting archive. Example:

    - Filename: README.md
      Skip: true

When applied, `README.md` will not be included in the resulting archive.

### Base64 encoded body

`Body:` can be used to provide the contents for a file as a base64 encoded string.

`Size:` is optional, but the number provided must match the length of the decoded data.

Example file:

    Contents:
    - Filename: hello.txt
      Body: SGVsbG8sIFdvcmxkIQo=

### Xattrs

`Xattrs:` is optional. Generate metadata with `-e` from a tar file with files that uses Xattrs for an example.

### Device nodes

`Devmajor:` and `Devminor:` can be set to a number if the `Type:` is set to **either** `character device node` **or** `block device node`. The resulting tarball may require root access when being extracted. The file body should be empty.

Example:

    Contents:
    - Filename: /dev/ttyS0
      Type: character device node
      Devmajor: 4
      Devminor: 64
      Mode: 0660

## From a tar file to only YAML metadata and back

It is possible to use standalone YAML, without corresponding tar file. The YAML file will be huge.

From `.tar` to `.yaml` (including file data):

    metatar -sd input.tar data.yaml

From `.yaml` to `.tar`:

    metatar -g data.yaml output.tar

## Known issues

* If __files__ are added to the metadata with a path, like for instance `usr/bin/filename`, then both `usr/` and `usr/bin/` are created automatically. However, if __empty directories__ are added, the parent directories are not created automatically and has to be added manually to the metadata.

## General information

* Version: 1.8.0
* License: BSD-3
* Author: Alexander F. Rødseth
