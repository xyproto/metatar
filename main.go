package main

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/fatih/color"
	"github.com/gobwas/glob"
	"github.com/surma/gocpio"
	"github.com/xyproto/yaml"
)

// TODO: In need of some refactoring:
// * A struct for all metadata, with functions for extracting and reading different types of metadata.
// * Split large functions into smaller functions.

const (
	metatarVersion = 1.9
	metatarName    = "MetaTAR"
	usage          = `metatar

Usage:
  metatar -s | --save [(-f | --force)] [(-v | --verbose)] [(-d | --data)] [(-e | --expand)] [(-r | --root)] [(-n | --nouser)] <tarfile> <yamlfile>
  metatar -a | --apply [(-f | --force)] [(-v | --verbose)] [(-d | --data)] [(-c | --cpio)] [(-o | --noskip)] <tarfile> <yamlfile> <newfile>
  metatar -g | --generate [(-f | --force)] [(-v | --verbose)] <yamlfile> <newfile>
  metatar -l | --list <tarfile>
  metatar -p | --listcpio <cpiofile>
  metatar -y | --yaml [(-v | --verbose)] [(-d | --data)] [(-e | --expand)] [(-r | --root)] [(-n | --nouser)] <tarfile>
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
  -o --noskip      Don't skip empty regular files.

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
  "StripEmptyLines: true", for stripping newlines.
  "StripComments: true", for stripping lines beginning with "#" (but not #!).
`
)

// Xattr represents the X attributes for a file in a tar archive
type Xattr struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

// MetaFileRegular represents all metadata for a file in a tar archive.
// "omitempty" is used to omit several fields that are normally empty.
type MetaFileRegular struct {
	Filename        string     `yaml:"Filename"`
	Skip            bool       `yaml:"Skip,omitempty"`   // For skipping files
	Rename          string     `yaml:"Rename,omitempty"` // For renaming files + altering metadata
	Linkname        string     `yaml:"Linkname,omitempty"`
	StripEmptyLines bool       `yaml:"StripEmptyLines,omitempty"` // For stripping empty lines
	StripComments   bool       `yaml:"StripComments,omitempty"`   // For stripping comments
	Type            string     `yaml:"Type"`
	Mode            yaml.Octal `yaml:"Mode"`
	UID             int        `yaml:"UID"`
	GID             int        `yaml:"GID"`
	Username        string     `yaml:"Username"`
	Groupname       string     `yaml:"Groupname"`
	Devmajor        int64      `yaml:"Devmajor,omitempty"`
	Devminor        int64      `yaml:"Devminor,omitempty"`
	BodySize        int        `yaml:"Size,omitempty"` // size of decoded file body
	Body            string     `yaml:"Body,omitempty"` // base64 encoded file body
	Xattrs          []Xattr    `yaml:"Xattrs,omitempty"`
}

// MetaFileExpanded represents all metadata for a file in a tar archive.
// Like MetaFile, but without the "omitempty" tag.
type MetaFileExpanded struct {
	Filename        string     `yaml:"Filename"`
	Skip            bool       `yaml:"Skip"`   // For skipping files
	Rename          string     `yaml:"Rename"` // For renaming files + altering metadata
	Linkname        string     `yaml:"Linkname"`
	StripEmptyLines bool       `yaml:"StripEmptyLines"` // For stripping empty lines
	StripComments   bool       `yaml:"StripComments"`   // For stripping comments
	Type            string     `yaml:"Type"`
	Mode            yaml.Octal `yaml:"Mode"`
	UID             int        `yaml:"UID"`
	GID             int        `yaml:"GID"`
	Username        string     `yaml:"Username"`
	Groupname       string     `yaml:"Groupname"`
	Devmajor        int64      `yaml:"Devmajor"`
	Devminor        int64      `yaml:"Devminor"`
	BodySize        int        `yaml:"Size"` // size of decoded file body
	Body            string     `yaml:"Body"` // base64 encoded file body
	Xattrs          []Xattr    `yaml:"Xattrs,flow"`
}

// MetaArchiveRegular represents all the metadata in a tar file.
// Everything but the actual file contents.
// Same as MetaArchiveExpanded, but with different YAML tags.
type MetaArchiveRegular struct {
	Version  float64           `yaml:"MetaTAR Version"`
	Contents []MetaFileRegular `yaml:"Contents"`
	SkipList []string          `yaml:"SkipList,omitempty"`
}

// ShouldSkipFunc is a function that determines if a given filename should be skipped or not
type ShouldSkipFunc func(string) bool

// MetaArchiveExpanded represents all the metadata in a tar file.
// Everything but the actual file contents.
// Same as MetaArchiveRegular, but with different YAML tags.
type MetaArchiveExpanded struct {
	Version  float64            `yaml:"MetaTAR Version"`
	Contents []MetaFileExpanded `yaml:"Contents"`
	SkipList []string           `yaml:"SkipList,omitempty"`
}

// Typeflag2string converts a given tar filetype byte to a string
func Typeflag2string(tf byte) string {
	switch tf {
	case tar.TypeReg:
		return "regular file"
	case tar.TypeRegA:
		return "regular file (A)"
	case tar.TypeLink:
		return "hard link"
	case tar.TypeSymlink:
		return "symlink"
	case tar.TypeChar:
		return "character device node"
	case tar.TypeBlock:
		return "block device node"
	case tar.TypeDir:
		return "directory"
	case tar.TypeFifo:
		return "fifo node"
	case tar.TypeCont:
		return "reserved"
	case tar.TypeXHeader:
		return "extended header"
	case tar.TypeXGlobalHeader:
		return "global extended header"
	case tar.TypeGNULongName:
		return "next file has a long name"
	case tar.TypeGNULongLink:
		return "next file symlinks to a file with a long name"
	case tar.TypeGNUSparse:
		return "sparse file"
	default:
		// Unknown typeflag, convert the byte to a string
		return strconv.FormatUint(uint64(tf), 10)
	}
}

// Typeflag2cpio converts a given tar filetype byte to a cpio filetype int64
func Typeflag2cpio(tf byte) int64 {
	switch tf {
	case tar.TypeReg:
		return cpio.TYPE_REG
	case tar.TypeRegA:
		return cpio.TYPE_REG
	case tar.TypeLink:
		// Hard linked files are stored as the original file in CPIO
		// TODO: Document that hard linked files that are not regular files are not supported
		fmt.Fprintln(os.Stderr, "Warning: Hard links in CPIO archives are not supported")
		return cpio.TYPE_SYMLINK
	case tar.TypeSymlink:
		return cpio.TYPE_SYMLINK
	case tar.TypeChar:
		return cpio.TYPE_CHAR
	case tar.TypeBlock:
		return cpio.TYPE_BLK
	case tar.TypeDir:
		return cpio.TYPE_DIR
	case tar.TypeFifo:
		return cpio.TYPE_FIFO
	case tar.TypeCont:
		quit("No reserved file types for CPIO")
		return 0
	case tar.TypeXHeader:
		quit("No extended header file type for CPIO")
		return 0
	case tar.TypeXGlobalHeader:
		quit("No global extended header file type for CPIO")
		return 0
	case tar.TypeGNULongName:
		quit("No GNU long name file type for this implementation of CPIO")
		return 0
	case tar.TypeGNULongLink:
		quit("No GNU long link name file type for this implementation of CPIO")
		return 0
	case tar.TypeGNUSparse:
		quit("No sparse file for this implementation of CPIO")
		return 0
	default:
		// Unknown typeflag, return 0
		return 0
	}
}

// CPIOtypeflag2string converts a given cpio filetype number to a string
func CPIOtypeflag2string(tf int64) string {
	switch tf {
	case cpio.TYPE_SOCK:
		return "socket file"
	case cpio.TYPE_SYMLINK:
		return "symlink"
	case cpio.TYPE_REG:
		return "regular file"
	case cpio.TYPE_BLK:
		return "block device node"
	case cpio.TYPE_DIR:
		return "directory"
	case cpio.TYPE_CHAR:
		return "character device node"
	case cpio.TYPE_FIFO:
		return "fifo node"
	default:
		// Unknown typeflag, convert the byte to a string
		return strconv.FormatUint(uint64(tf), 10)
	}
}

// String2typeflag converts a given tar filetype string to a byte
func String2typeflag(tfs string) (byte, error) {
	switch tfs {
	case "regular file":
		return tar.TypeReg, nil
	case "regular file (A)":
		return tar.TypeRegA, nil
	case "hard link":
		return tar.TypeLink, nil
	case "symlink":
		return tar.TypeSymlink, nil
	case "character device node":
		return tar.TypeChar, nil
	case "block device node":
		return tar.TypeBlock, nil
	case "directory":
		return tar.TypeDir, nil
	case "fifo node":
		return tar.TypeFifo, nil
	case "reserved":
		return tar.TypeCont, nil
	case "extended header":
		return tar.TypeXHeader, nil
	case "global extended header":
		return tar.TypeXGlobalHeader, nil
	case "next file has a long name":
		return tar.TypeGNULongName, nil
	case "next file symlinks to a file with a long name":
		return tar.TypeGNULongLink, nil
	case "sparse file":
		return tar.TypeGNUSparse, nil
	default:
		// First try converting from string to a byte
		tf, err := strconv.ParseUint(tfs, 10, 8)
		if err != nil {
			// Assume it is a regular file (!)
			return tar.TypeReg, nil
			//return 255, errors.New("Invalid file type: " + tfs)
		}
		return byte(tf), nil
	}
}

// print error message and quit with exit code 1
func quit(msg string) {
	check(errors.New(msg))
}

// print error and quit, or just quit if err is nil
func quiterr(err error) {
	check(err) // Will quit with exit code 1 if err != nil
	os.Exit(0)
}

// print out errors and quit, unless err is nil
func check(err error) {
	if err != nil {
		color.Set(color.FgRed)
		fmt.Fprint(os.Stderr, "Error: ")
		color.Unset()
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

// exists checks if the given filename exists, using stat
func exists(filename string) bool {
	if _, err := os.Stat(filename); err != nil {
		return false
	}
	return true
}

// Strip "Username:", "Groupname:", "UID:" and "GID:" lines from input
func stripUserGroup(inputbuf bytes.Buffer) bytes.Buffer {
	var buf bytes.Buffer
	newlines := []string{}

	lines := strings.Split(inputbuf.String(), "\n")
	for _, line := range lines {
		trimline := strings.TrimSpace(line)
		if strings.HasPrefix(trimline, "Username:") {
			continue
		} else if strings.HasPrefix(trimline, "Groupname:") {
			continue
		} else if strings.HasPrefix(trimline, "UID:") {
			continue
		} else if strings.HasPrefix(trimline, "GID:") {
			continue
		}
		newlines = append(newlines, line)
	}

	buf.WriteString(strings.Join(newlines, "\n"))
	return buf
}

func tar2metadata(hdr *tar.Header, root bool) MetaFileExpanded {
	m := MetaFileExpanded{}
	m.Filename = hdr.Name
	m.Linkname = hdr.Linkname
	m.Type = Typeflag2string(hdr.Typeflag)
	m.Mode = yaml.Octal(hdr.Mode)
	if root {
		m.Username = "root"
		m.UID = 0
		m.Groupname = "root"
		m.GID = 0
	} else {
		m.Username = hdr.Uname
		m.UID = hdr.Uid
		m.Groupname = hdr.Gname
		m.GID = hdr.Gid
	}
	m.Devmajor = hdr.Devmajor
	m.Devminor = hdr.Devminor

	for k, v := range hdr.Xattrs {
		x := Xattr{}
		x.Key = k
		x.Value = v
		m.Xattrs = append(m.Xattrs, x)
	}

	return m
}

// WriteMetadata takes a tar archive and outputs a YAML file
func WriteMetadata(tarfilename, yamlfilename string, force, withBody, verbose, expand, root, nouser bool) error {

	dat, err := ioutil.ReadFile(tarfilename)
	if err != nil {
		return err
	}

	// Open the tar archive for reading.
	r := bytes.NewReader(dat)
	tr := tar.NewReader(r)

	var (
		x   Xattr        // For the Xattrs
		buf bytes.Buffer // For the resulting YAML file
	)

	// A big hacky if/else here. The alternative is using interface{} or
	// adding methods for each field in the MetaArchiveRegular and
	// MetaArchiveExpanded structs. Or changing the struct tags runtime
	// somehow, possibly using: github.com/sevlyar/retag

	if !expand {

		// Create the data structure
		mfs := MetaArchiveRegular{}
		mfs.Version = metatarVersion

		// Iterate through the files in the archive.
		for {

			hdr, err := tr.Next()
			if err == io.EOF {
				// end of tar archive
				break
			}
			if err != nil {
				return err
			}

			m := MetaFileRegular{}
			m.Filename = hdr.Name
			m.Linkname = hdr.Linkname
			m.Type = Typeflag2string(hdr.Typeflag)
			m.Mode = yaml.Octal(hdr.Mode)
			if root {
				m.Username = "root"
				m.UID = 0
				m.Groupname = "root"
				m.GID = 0
			} else {
				m.Username = hdr.Uname
				m.UID = hdr.Uid
				m.Groupname = hdr.Gname
				m.GID = hdr.Gid
			}
			m.Devmajor = hdr.Devmajor
			m.Devminor = hdr.Devminor

			for k, v := range hdr.Xattrs {
				x = Xattr{}
				x.Key = k
				x.Value = v
				m.Xattrs = append(m.Xattrs, x)
			}

			// Store the file body as a base64 encoded string
			if withBody {
				var bodybuf bytes.Buffer
				_, err = io.Copy(&bodybuf, tr)
				if err != nil {
					return err
				}
				m.BodySize = len(bodybuf.Bytes())
				if m.BodySize == 0 {
					if verbose {
						fmt.Println(hdr.Name + " is empty, body not written to YAML file")
					}
				}
				m.Body = base64.StdEncoding.EncodeToString(bodybuf.Bytes())
			}

			// Append the metadata about a file to the collection
			mfs.Contents = append(mfs.Contents, m)
		}

		// Create YML code
		if d, err := yaml.Marshal(&mfs); err != nil {
			if err != nil {
				return err
			}
		} else {
			if _, err := buf.Write(d); err != nil {
				return err
			}
		}

	} else {

		// Create the data structure
		mfs := MetaArchiveExpanded{}
		mfs.Version = metatarVersion

		// Iterate through the files in the archive.
		for {

			hdr, err := tr.Next()
			if err == io.EOF {
				// end of tar archive
				break
			}
			if err != nil {
				return err
			}

			m := tar2metadata(hdr, root)

			// Store the file body as a base64 encoded string
			if withBody {
				var bodybuf bytes.Buffer
				_, err = io.Copy(&bodybuf, tr)
				if err != nil {
					return err
				}
				m.BodySize = len(bodybuf.Bytes())
				if m.BodySize == 0 {
					if verbose {
						fmt.Println(hdr.Name + " is empty, body not written to YAML file")
					}
				}
				m.Body = base64.StdEncoding.EncodeToString(bodybuf.Bytes())
			}

			// Append the metadata about a file to the collection
			mfs.Contents = append(mfs.Contents, m)
		}

		// Create YML code
		if d, err := yaml.Marshal(&mfs); err != nil {
			if err != nil {
				return err
			}
		} else {
			if _, err := buf.Write(d); err != nil {
				return err
			}
		}

	}

	// If the --nouser flag is given, don't record user/group information
	if nouser {
		buf = stripUserGroup(buf)
	}

	if yamlfilename == "-" {
		// Write to stdout
		fmt.Print(buf.String())
	} else {
		// Check if the YAML file exists first
		if !force && exists(yamlfilename) {
			quit(fmt.Sprintf("%s already exists", yamlfilename))
		}
		// Write the YAML file
		if ioutil.WriteFile(yamlfilename, buf.Bytes(), 0644) != nil {
			return err
		}
	}

	return nil
}

// ListTar takes a tar archive and lists the contents
func ListTar(filename string) error {
	//fmt.Printf("\n--- Contents of %s ---\n\n", filename)
	dat, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	// Open the tar archive for reading.
	r := bytes.NewReader(dat)
	tr := tar.NewReader(r)

	// Loop through the files in the input tar archive.
	prevname := ""
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// End of tar
			break
		}
		if err != nil {
			return errors.New(filename + ": after: " + prevname + ": " + err.Error())
		}

		prevname = hdr.Name
		fmt.Printf("%s:\n", hdr.Name)
		fmt.Printf("\tType: %s\n", Typeflag2string(hdr.Typeflag))
		fmt.Printf("\tMode: %s\n", yaml.Octal(hdr.Mode).String())
		fmt.Printf("\tUser: %d\n", hdr.Uid)
		fmt.Printf("\tGroup: %d\n", hdr.Gid)
		fmt.Printf("\tUsername: %s\n", hdr.Uname)
		fmt.Printf("\tGroupname: %s\n", hdr.Gname)
		if hdr.Devmajor != 0 {
			fmt.Printf("\tDevmajor: %d\n", hdr.Devmajor)
		}
		if hdr.Devminor != 0 {
			fmt.Printf("\tDevminor: %d\n", hdr.Devminor)
		}

		for k, v := range hdr.Xattrs {
			fmt.Printf("\tXattrs for %s: %s=%s\n", hdr.Name, k, v)
		}

		var bodybuf bytes.Buffer
		_, err = io.Copy(&bodybuf, tr)
		if err != nil {
			return err
		}
		fmt.Printf("\tSize: %v\n", len(bodybuf.Bytes()))
	}
	return nil
}

// ListCPIO takes a cpio (newc) archive and lists the contents
func ListCPIO(filename string) error {
	//fmt.Printf("\n--- Contents of %s ---\n\n", filename)
	dat, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	// Open the tar archive for reading.
	r := bytes.NewReader(dat)
	gr := cpio.NewReader(r)

	// Loop through the files in the input tar archive.
	prevname := ""
	for {
		hdr, err := gr.Next()
		if err == io.EOF {
			// End of cpio
			break
		}
		if err != nil {
			if err.Error() == "Did not find valid magic number" {
				// End of file
				break
			}
			return errors.New(filename + ": after: " + prevname + ": " + err.Error())
		}

		// weird gocpio marker for end of file
		if hdr.Name == "TRAILER!!!" {
			break
		}

		prevname = hdr.Name
		fmt.Printf("%s:\n", hdr.Name)
		fmt.Printf("\tType: %s\n", CPIOtypeflag2string(hdr.Type))
		fmt.Printf("\tMode: %s\n", yaml.Octal(hdr.Mode).String())
		fmt.Printf("\tUser: %d\n", hdr.Uid)
		fmt.Printf("\tGroup: %d\n", hdr.Gid)
		if hdr.Devmajor != 0 {
			fmt.Printf("\tDevmajor: %d\n", hdr.Devmajor)
		}
		if hdr.Devminor != 0 {
			fmt.Printf("\tDevminor: %d\n", hdr.Devminor)
		}

		var bodybuf bytes.Buffer
		_, err = io.Copy(&bodybuf, gr)
		if err != nil {
			return err
		}
		fmt.Printf("\tSize: %v\n", len(bodybuf.Bytes()))

		if hdr.Type == cpio.TYPE_SYMLINK {
			fmt.Printf("\tLinkname: %s\n", bodybuf.String())
		}
	}
	return nil
}

// ApplyMetadataToTar takes a tar archive and a YAML metadata file. It then applies
// all the metadata to the tar archive contents and outputs a new tar archive.
func ApplyMetadataToTar(tarfilename, yamlfilename, newfilename string, force, withBody, verbose, skipEmptyFiles bool) error {

	// Read the metadata
	yamldata, err := ioutil.ReadFile(yamlfilename)
	if err != nil {
		return err
	}

	// Unmarshal the metadata (MetaArchiveRegular or MetaArchiveExpanded has the same fields, so either is fine)
	mfs := MetaArchiveRegular{}
	if err := yaml.Unmarshal(yamldata, &mfs); err != nil {
		return err
	}

	// Check the metadata version
	if mfs.Version > metatarVersion {
		if verbose {
			fmt.Printf("YML metadata is from the future, from MetaTAR %v\n", mfs.Version)
		}
	}

	// Store the files in the input archive in a map
	bodymap := make(map[string][]byte)
	// Store if files are copied over in this map
	donemap := make(map[string]bool)

	// Skip the input tar file if the filename is empty
	if tarfilename != "" {

		// Read the input tarfile
		dat, err := ioutil.ReadFile(tarfilename)
		if err != nil {
			return err
		}
		r := bytes.NewReader(dat)
		tr := tar.NewReader(r)

		// Loop over all files in the input tar archive
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				// End of tar
				break
			}
			if err != nil {
				return errors.New(tarfilename + ": " + err.Error())
			}
			var bodybuf bytes.Buffer
			if _, err = io.Copy(&bodybuf, tr); err != nil {
				return err
			}
			bodymap[hdr.Name] = bodybuf.Bytes()
		}

	}

	// Create a buffer to write our new archive to.
	buf := new(bytes.Buffer)

	// Create a new tar archive.
	tw := tar.NewWriter(buf)

	// Loop through the files in the metadata and write the corresponding file to the tar
	for _, mf := range mfs.Contents {
		emptyRegularFile := false
		if hasl(mfs.SkipList, mf.Filename) || hasglob(mfs.SkipList, mf.Filename) {
			mf.Skip = true
		}
		if mf.Skip {
			if verbose {
				fmt.Printf("%s: skipping %s\n", filepath.Base(yamlfilename), mf.Filename)
			}
			continue
		}
		if _, ok := donemap[mf.Filename]; ok {
			if verbose {
				fmt.Printf("%s: skipping duplicate filename: %s\n", filepath.Base(yamlfilename), mf.Filename)
			}
			continue
		}
		if _, ok := bodymap[mf.Filename]; !ok {
			emptyRegularFile = len(bodymap[mf.Filename]) == 0 && (mf.Type == "regular file" || mf.Type == "regular file (A)")
			if emptyRegularFile && skipEmptyFiles {
				continue
			}
			if verbose {
				user := mf.Username
				if user == "" {
					user = strconv.Itoa(mf.UID)
				}
				group := mf.Groupname
				if group == "" {
					group = strconv.Itoa(mf.GID)
				}
				if emptyRegularFile {
					fmt.Printf("%s: creating empty file %s (%s:%s, mode %s)\n", filepath.Base(yamlfilename), mf.Filename, user, group, mf.Mode)
				} else {
					fmt.Printf("%s: create %s (%s:%s, mode %s)\n", filepath.Base(yamlfilename), mf.Filename, user, group, mf.Mode)
				}
			}
		}
		typeflag, err := String2typeflag(mf.Type)
		if err != nil {
			quit(fmt.Sprintf("For %s: %s", mf.Filename, err.Error()))
		}
		if (mf.Devmajor != 0 || mf.Devminor != 0) && !strings.Contains(mf.Type, "device") {
			quit(fmt.Sprintf("%s: Major and minor device numbers only apply to character and block device file!", mf.Filename))
		}
		headerFilename := mf.Filename
		if mf.Rename != "" {
			headerFilename = mf.Rename
			if _, ok := bodymap[mf.Rename]; ok {
				if verbose {
					fmt.Printf("%s: rename %s -> %s: %s already exists in %s!\n", filepath.Base(yamlfilename), mf.Filename, mf.Rename, mf.Rename, tarfilename)
				}
			} else {
				// Make sure the renamed file exists in the bodymap too, since it's used for checking later on
				if _, ok2 := bodymap[mf.Filename]; ok2 {
					bodymap[mf.Rename] = bodymap[mf.Filename]
				}
			}
		}

		if withBody && len(mf.Body) != 0 {
			if b, err := base64.StdEncoding.DecodeString(string(mf.Body)); err != nil {
				quit(fmt.Sprintf("Could not decode base64 string for %s!", mf.Filename))
			} else {
				if mf.BodySize > 0 {
					if len(b) != mf.BodySize {
						quit(fmt.Sprintf("%s: size is wrong for %s: %d != %d", filepath.Base(yamlfilename), mf.Filename, len(b), mf.BodySize))
					}
				}
				bodymap[mf.Filename] = b
			}
		}

		if mf.StripEmptyLines {
			// Strip empty lines from the data in bodymap[mf.Filename]
			s := string(bodymap[mf.Filename])
			re, err := regexp.Compile("\n\n")
			check(err)
			bodymap[mf.Filename] = []byte(re.ReplaceAllString(s, "\n"))
		}

		if mf.StripComments {
			// Remove bash comments, skip the first line
			s := string(bodymap[mf.Filename])
			l := []string{}
			for _, line := range strings.Split(s, "\n") {
				if strings.HasPrefix(line, "#!") {
					l = append(l, line)
					continue
				}
				if strings.HasPrefix(strings.TrimSpace(line), "#") {
					continue
				}
				l = append(l, line)
			}
			bodymap[mf.Filename] = []byte(strings.Join(l, "\n"))
		}

		mode := mf.Mode.Int64()
		if mode == 0 {
			// Permissions for file or directory is missing!
			if typeflag == tar.TypeDir {
				mode = 0770 // octal
			} else {
				mode = 0660 // octal
			}
			if verbose {
				fmt.Printf("%s: using default file permissions for %s: %s\n", filepath.Base(yamlfilename), mf.Filename, yaml.Octal(mode))
			}
		}

		hdr := &tar.Header{
			Name:     headerFilename,
			Linkname: mf.Linkname,
			Typeflag: typeflag,
			Mode:     mode,
			Uname:    mf.Username,
			Uid:      mf.UID,
			Gname:    mf.Groupname,
			Gid:      mf.GID,
			Devmajor: mf.Devmajor,
			Devminor: mf.Devminor,
			Size:     int64(len(bodymap[mf.Filename])), // Get size from corresponding file in tarfilename
		}
		for _, xattr := range mf.Xattrs {
			hdr.Xattrs[xattr.Key] = xattr.Value
		}

		// Extra skip check before writing header and body
		if !(skipEmptyFiles && emptyRegularFile) {
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if _, err := tw.Write(bodymap[mf.Filename]); err != nil {
				return err
			}
		}

		donemap[mf.Filename] = true
	}
	if err := tw.Close(); err != nil {
		quiterr(err)
	}

	for filename, done := range donemap {
		if !done {
			if verbose {
				fmt.Printf("%s from %s was skipped!", filename, tarfilename)
			}
		}
	}

	// Check if the TAR file exists first
	if !force && exists(newfilename) {
		quit(fmt.Sprintf("%s already exists", newfilename))
	}
	// Write the new tarfile
	return ioutil.WriteFile(newfilename, buf.Bytes(), 0644)
}

// Add a file to a CPIO archive by writing to a cpio.Writer,
// given a MetaFileRegular struct.
// Also needs the tar and YAML filename for error messages.
// Needs a bodymap, donemap and dirmap to keep track of file bodies,
// if they are already written and if directories needs to be created.
// Will read and write to these maps.
// mtime is the default time to use for the files
// withBody is if the file body from the metadata should be used, if present.
// verbose gives more verbose output along the way.
// Returns nil if everything worked out fine.
func addFileToCPIO(cw *cpio.Writer, mf MetaFileExpanded, tarfilename, yamlfilename string, bodymap map[string][]byte, metamap map[string]MetaFileExpanded, skipmap, donemap, renmap, dirmap map[string]bool, mtime int64, withBody, verbose, declaredInYAML, skipEmptyFiles bool, ssf ShouldSkipFunc) error {
	emptyRegularFile := false
	if mf.Skip {
		if verbose {
			fmt.Printf("%s: skip %s\n", filepath.Base(yamlfilename), mf.Filename)
		}
		skipmap[mf.Filename] = true
		return nil
	}
	if _, ok := donemap[mf.Filename]; ok {
		if verbose && declaredInYAML {
			fmt.Printf("%s: skip duplicate filename: %s\n", filepath.Base(yamlfilename), mf.Filename)
		}
		return nil
	}
	if _, ok := bodymap[mf.Filename]; !ok {
		emptyRegularFile = len(bodymap[mf.Filename]) == 0 && (mf.Type == "regular file" || mf.Type == "regular file (A)")
		if emptyRegularFile && skipEmptyFiles {
			// Skip empty regular files
			return nil
		}
		if verbose {
			user := mf.Username
			if user == "" {
				user = strconv.Itoa(mf.UID)
			}
			group := mf.Groupname
			if group == "" {
				group = strconv.Itoa(mf.GID)
			}
			if emptyRegularFile {
				fmt.Printf("%s: creating empty file %s (%s:%s, mode %s)\n", filepath.Base(yamlfilename), mf.Filename, user, group, mf.Mode)
			} else {
				fmt.Printf("%s: create %s (%s:%s, mode %s)\n", filepath.Base(yamlfilename), mf.Filename, user, group, mf.Mode)
			}
		}
	}

	typeflag, err := String2typeflag(mf.Type)
	if err != nil {
		quit(fmt.Sprintf("For %s: %s", mf.Filename, err.Error()))
	}
	if (mf.Devmajor != 0 || mf.Devminor != 0) && !strings.Contains(mf.Type, "device") {
		quit(fmt.Sprintf("%s: %s: Major and minor device numbers only apply to character and block device files!", filepath.Base(yamlfilename), mf.Filename))
	}

	headerFilename := mf.Filename
	if mf.Rename != "" {
		headerFilename = mf.Rename
		if _, ok := bodymap[mf.Rename]; ok {
			emptyRegularFile = len(bodymap[mf.Rename]) == 0 && (mf.Type == "regular file" || mf.Type == "regular file (A)")
			if verbose {
				fmt.Printf("%s: when renaming %s to %s: %s already exists in %s!\n", filepath.Base(yamlfilename), mf.Filename, mf.Rename, mf.Rename, tarfilename)
			}
		} else {
			// Make sure the renamed file exists in the bodymap too, since it's used for checking later on
			if _, ok2 := bodymap[mf.Filename]; ok2 {
				bodymap[mf.Rename] = bodymap[mf.Filename]
				renmap[mf.Rename] = true
				if _, ok3 := metamap[mf.Filename]; ok3 {
					if verbose {
						fmt.Printf("%s: rename %s -> %s\n", filepath.Base(yamlfilename), mf.Filename, mf.Rename)
					}
					metamap[mf.Rename] = metamap[mf.Filename]

				}
			}

			// TODO: Refactor the code below into a function

			// Create a directory if needed
			dirname := filepath.Clean(filepath.Dir(headerFilename))
			headerDirname := dirname + "/"
			if _, ok := dirmap[dirname]; !(ok || strings.HasPrefix(dirname, ".") || strings.HasPrefix(dirname, "/")) {
				if !ssf(headerDirname) {
					if verbose {
						fmt.Printf("%s: creating missing directory for \"%s\" (renamed from %s) in CPIO, creating: %s\n", filepath.Base(yamlfilename), headerFilename, mf.Filename, headerDirname)
					}
					dirhdr := &cpio.Header{
						Name:     headerDirname,
						Mode:     0555,
						Uid:      mf.UID,
						Gid:      mf.GID,
						Mtime:    mtime,
						Size:     0, // Get size from corresponding file in tarfilename
						Devmajor: 0,
						Devminor: 0,
						Type:     cpio.TYPE_DIR,
					}
					cw.WriteHeader(dirhdr)
					dirmap[dirname] = true
					donemap[dirname] = true
				}
			}

		}
	}

	// If the actual file body is specified in the YAML file as base64
	if withBody && len(mf.Body) != 0 {
		if b, err := base64.StdEncoding.DecodeString(string(mf.Body)); err != nil {
			quit(fmt.Sprintf("Could not decode base64 string for %s!", mf.Filename))
		} else {
			if mf.BodySize > 0 {
				if len(b) != mf.BodySize {
					quit(fmt.Sprintf("%s: size is wrong for %s: %d != %d", filepath.Base(yamlfilename), mf.Filename, len(b), mf.BodySize))
				}
			}
			bodymap[mf.Filename] = b
			metamap[mf.Filename] = mf
		}
	}

	if mf.StripEmptyLines {
		// TODO: Check if mf.Filename exists in bodymap
		// Strip empty lines from the data in bodymap[mf.Filename]
		s := string(bodymap[mf.Filename])
		re := regexp.MustCompile("\n\n")
		bodymap[mf.Filename] = []byte(re.ReplaceAllString(s, "\n"))
	}

	if mf.StripComments {
		// TODO: Check if mf.Filename exists in bodymap
		// Remove bash comments, skip the first line
		s := string(bodymap[mf.Filename])
		l := []string{}
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(line, "#!") {
				l = append(l, line)
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			l = append(l, line)
		}
		bodymap[mf.Filename] = []byte(strings.Join(l, "\n"))
	}

	// Make sure directory names has a trailing slash
	if typeflag == tar.TypeDir {
		if !strings.HasSuffix(headerFilename, "/") {
			headerFilename += "/"
		}
	} else {
		if strings.HasSuffix(headerFilename, "/") {
			quit(fmt.Sprintf("%s: Filename field is not a directory but ends with a slash: %s", filepath.Base(yamlfilename), headerFilename))
		}
	}

	mode := mf.Mode.Int64()
	if mode == 0 {
		// Permissions for file or directory is missing!
		if typeflag == tar.TypeDir {
			mode = 0770 // octal
		} else {
			mode = 0660 // octal
		}
		if verbose {
			fmt.Printf("%s: using default file permissions for %s: %s\n", filepath.Base(yamlfilename), mf.Filename, yaml.Octal(mode))
		}
	}

	hdr := &cpio.Header{
		Name:     headerFilename,
		Mode:     mode,
		Uid:      mf.UID,
		Gid:      mf.GID,
		Mtime:    mtime,
		Size:     int64(len(bodymap[mf.Filename])), // Get size from corresponding file in tarfilename
		Devmajor: mf.Devmajor,
		Devminor: mf.Devminor,
		Type:     Typeflag2cpio(typeflag),
	}

	size := int64(len(bodymap[mf.Filename]))
	if hdr.Type == cpio.TYPE_REG && size == 0 {
		fmt.Printf("%s: WARNING: size of %s is 0!\n", filepath.Base(yamlfilename), mf.Filename)
	}

	if hdr.Type == cpio.TYPE_SYMLINK {
		// Check that the linkname exists, unless the linkname starts with a "." or a "/"
		if verbose {
			if _, ok := bodymap[mf.Linkname]; !ok && !(strings.HasPrefix(mf.Linkname, ".") || strings.HasPrefix(mf.Linkname, "/")) {
				if _, ok2 := bodymap[mf.Linkname+"/"]; ok2 {
					fmt.Printf("%s: link %s -> %s\n", filepath.Base(yamlfilename), headerFilename, mf.Linkname+"/")
				} else {
					fmt.Printf("%s: link %s -> %s\n", filepath.Base(yamlfilename), headerFilename, mf.Linkname)
				}
			}
		}
		// Create a directory if needed
		dirname := filepath.Clean(filepath.Join(filepath.Dir(mf.Filename), filepath.Dir(mf.Linkname)))
		if _, ok := dirmap[dirname]; !(ok || strings.HasPrefix(dirname, ".") || strings.HasPrefix(dirname, "/")) {
			// it's a relative link
			headerDirname := dirname + "/"
			if !ssf(headerDirname) {
				if verbose {
					fmt.Printf("%s: creating missing directory for \"%s\" (renamed from %s) in CPIO, creating: %s\n", filepath.Base(yamlfilename), headerFilename, mf.Filename, headerDirname)
				}
				dirhdr := &cpio.Header{
					Name:     headerDirname,
					Mode:     0555,
					Uid:      mf.UID,
					Gid:      mf.GID,
					Mtime:    mtime,
					Size:     0, // Get size from corresponding file in tarfilename
					Devmajor: 0,
					Devminor: 0,
					Type:     cpio.TYPE_DIR,
				}
				cw.WriteHeader(dirhdr)
				dirmap[dirname] = true
				donemap[dirname] = true
			}
		}

		// If the type is a symlink set the body size to the length of the Linkname as bytes
		hdr.Size = int64(len([]byte(mf.Linkname)))
	} else {
		// Create a directory if needed
		dirname := filepath.Clean(filepath.Dir(headerFilename))
		headerDirname := dirname + "/"
		if _, ok := dirmap[dirname]; !(ok || strings.HasPrefix(dirname, ".") || strings.HasPrefix(dirname, "/")) {
			if !ssf(headerDirname) {
				if verbose {
					fmt.Printf("%s: creating missing directory for \"%s\" (renamed from %s) in CPIO, creating: %s\n", filepath.Base(yamlfilename), headerFilename, mf.Filename, headerDirname)
				}
				dirhdr := &cpio.Header{
					Name:     headerDirname,
					Mode:     0555,
					Uid:      mf.UID,
					Gid:      mf.GID,
					Mtime:    mtime,
					Size:     0,
					Devmajor: 0,
					Devminor: 0,
					Type:     cpio.TYPE_DIR,
				}
				cw.WriteHeader(dirhdr)
				dirmap[dirname] = true
				donemap[dirname] = true
			}
		}
	}

	// Check if this directory has already been written
	if _, ok := dirmap[filepath.Clean(hdr.Name)]; ok {
		// skip
		return nil
	}

	// Extra skip check
	if !(skipEmptyFiles && emptyRegularFile) {
		// Write the header
		if err := cw.WriteHeader(hdr); err != nil {
			return err
		}
		// Write the body (or Linkname, if it is a symlink)
		if hdr.Type == cpio.TYPE_SYMLINK {
			// Write symbolink link Linknames to the file body
			if _, err := cw.Write([]byte(mf.Linkname)); err != nil {
				return err
			}
		} else {
			// Other file content is written to the file body too
			if _, err := cw.Write(bodymap[mf.Filename]); err != nil {
				return err
			}
		}
	}
	donemap[mf.Filename] = true
	return nil
}

// Given a slice of strings and a string, figure out if the string is present
func hasl(l []string, e string) bool {
	for _, x := range l {
		if x == e {
			return true
		}
	}
	return false
}

// Given a slice of strings that are regular expressions, and a string, figure out if the string matches any of the regular expressions
func hasglob(l []string, e string) bool {
	for _, globexpr := range l {
		g, err := glob.Compile(globexpr)
		check(err)
		if g.Match(e) {
			return true
		}
	}
	return false
}

// Given a map of string->bool and a string, figure out if the string is present as a key in the map
func has(m map[string]bool, e string) bool {
	if _, ok := m[e]; ok {
		return true
	}
	return false
}

// Given a map of string->MetaFileExpanded and a string, figure out if the string is present as a key in the map
func hasm(m map[string]MetaFileExpanded, e string) bool {
	if _, ok := m[e]; ok {
		return true
	}
	return false
}

// Given a map of string->[]byte and a string, figure out if the string is present as a key in the map
func hasb(m map[string][]byte, e string) bool {
	if _, ok := m[e]; ok {
		return true
	}
	return false
}

// ApplyMetadataToCpio takes a tar archive and a YAML metadata file. It then applies
// all the metadata to the tar archive contents and outputs a new tar archive.
// root == True will not set alle file permissions to root, only the undeclared ones.
func ApplyMetadataToCpio(tarfilename, yamlfilename, newfilename string, force, withBody, root, verbose, skipEmptyFiles bool) error {

	// Read the metadata
	yamldata, err := ioutil.ReadFile(yamlfilename)
	if err != nil {
		return err
	}

	// Unmarshal the metadata (MetaArchiveRegular or MetaArchiveExpanded has the same fields, so either is fine)
	mfs := MetaArchiveRegular{}
	if err := yaml.Unmarshal(yamldata, &mfs); err != nil {
		return err
	}

	// Check the metadata version
	if mfs.Version > metatarVersion {
		if verbose {
			fmt.Printf("YML metadata is from the future, from MetaTAR %v\n", mfs.Version)
		}
	}

	// Store the files in the input archive in a map
	bodymap := make(map[string][]byte)
	// Store if files are copied over in this map
	donemap := make(map[string]bool)
	// Store if files are renamed
	renmap := make(map[string]bool)
	// Store directory names. Create directories if needed. Use "filepath.Clean".
	dirmap := make(map[string]bool)
	// Store metadata directly from the tar file, in case they are missing from the YAML metadata
	metamap := make(map[string]MetaFileExpanded)
	// Store skipped files
	skipmap := make(map[string]bool)

	// Skip the input tar file if the filename is empty
	if tarfilename != "" {

		// Read the input tarfile
		dat, err := ioutil.ReadFile(tarfilename)
		if err != nil {
			return err
		}
		r := bytes.NewReader(dat)
		tr := tar.NewReader(r)

		// Loop over all files in the input tar archive
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				// End of tar
				break
			}
			if err != nil {
				return errors.New(tarfilename + ": " + err.Error())
			}
			var bodybuf bytes.Buffer
			if _, err = io.Copy(&bodybuf, tr); err != nil {
				return err
			}
			bodymap[hdr.Name] = bodybuf.Bytes()
			metamap[hdr.Name] = tar2metadata(hdr, root)
		}

	}

	// Create a buffer to write our new archive to.
	buf := new(bytes.Buffer)

	// Create a new tar archive.
	cw := cpio.NewWriter(buf)

	// Choose a timestamp (seconds since epoch)
	mtime := time.Now().Unix()

	// "Should skip" function
	ssf := func(filename string) bool {
		return hasl(mfs.SkipList, filename) || hasglob(mfs.SkipList, filename)
	}

	// Loop through the files in the metadata and write the corresponding file to the tar
	for _, mf := range mfs.Contents {
		if ssf(mf.Filename) {
			mf.Skip = true
		}
		addFileToCPIO(cw, MetaFileExpanded(mf), tarfilename, yamlfilename, bodymap, metamap, skipmap, donemap, renmap, dirmap, mtime, withBody, verbose, true, skipEmptyFiles, ssf)
	}

	// List all files in bodymap but not in donemap (from the tar, but no YAML metadata)
	for filename := range bodymap {
		autocreatedDirectory := has(dirmap, filename) || has(dirmap, filepath.Clean(filename))
		isDone := has(donemap, filename) || has(donemap, filepath.Clean(filename))
		isRenamed := has(renmap, filename) || has(renmap, filepath.Clean(filename))
		isSkipped := has(skipmap, filename) || has(skipmap, filepath.Clean(filename))
		var (
			mf          MetaFileExpanded
			hasMetadata bool
		)
		if hasm(metamap, filename) {
			hasMetadata = true
			mf = metamap[filename]
		} else if hasm(metamap, filepath.Clean(filename)) {
			hasMetadata = true
			mf = metamap[filepath.Clean(filename)]
		}
		//fmt.Printf("autodir=%v\tmeta=%v\tdone=%v\trename=%v\tfilename=%s\n", autocreatedDirectory, hasMetadata, isDone, isRenamed, filename)

		if isDone && !hasMetadata {
			quit(fmt.Sprintf("metatar: %s is in %s, but the file itself is missing!", filename, yamlfilename))
		} else if !isRenamed && !isDone && !autocreatedDirectory && !isSkipped {
			if verbose {
				fmt.Printf("%s: found no metadata for %s\n", filepath.Base(yamlfilename), filename)
			}
			if hasl(mfs.SkipList, mf.Filename) || hasglob(mfs.SkipList, mf.Filename) {
				mf.Skip = true
			}
			addFileToCPIO(cw, mf, tarfilename, yamlfilename, bodymap, metamap, skipmap, donemap, renmap, dirmap, mtime, withBody, verbose, false, skipEmptyFiles, ssf)
		}
	}

	if err := cw.Close(); err != nil {
		quiterr(err)
	}

	for filename, done := range donemap {
		if !done {
			if verbose {
				fmt.Printf("%s from %s was skipped!", filename, tarfilename)
			}
		}
	}

	// Check if the CPIO file exists first
	if !force && exists(newfilename) {
		quit(fmt.Sprintf("%s already exists", newfilename))
	}

	// Write the new CPIO file
	return ioutil.WriteFile(newfilename, buf.Bytes(), 0644)
}

// MergeMetadata merges two YAML files. The first file contents are overridden by the second one.
// The newfilename can be a new YAML filename or "-" for standard out.
func MergeMetadata(yamlfilename1, yamlfilename2, newfilename string, force, verbose bool) error {

	if verbose {
		fmt.Printf("Merge %s and %s into %s, with force=%v and verbose=%v\n", yamlfilename1, yamlfilename2, newfilename, force, verbose)
	}

	// Read the metadata
	yamldata1, err := ioutil.ReadFile(yamlfilename1)
	if err != nil {
		return err
	}
	yamldata2, err := ioutil.ReadFile(yamlfilename2)
	if err != nil {
		return err
	}

	// Unmarshal the metadata (MetaArchiveRegular or MetaArchiveExpanded has the same fields, so either is fine)
	mfs1 := MetaArchiveRegular{}
	if err := yaml.Unmarshal(yamldata1, &mfs1); err != nil {
		return err
	}
	mfs2 := MetaArchiveRegular{}
	if err := yaml.Unmarshal(yamldata2, &mfs2); err != nil {
		return err
	}

	// Check the metadata version
	if mfs1.Version > metatarVersion {
		if verbose {
			fmt.Printf("YML metadata for %s is from the future, from MetaTAR %v\n", yamlfilename1, mfs1.Version)
		}
	}
	if mfs2.Version > metatarVersion {
		if verbose {
			fmt.Printf("YML metadata for %s is from the future, from MetaTAR %v\n", yamlfilename2, mfs2.Version)
		}
	}

	// The new contents
	newmfs := MetaArchiveRegular{}
	hasfile := make(map[string]bool)

	// Use mfs1.Contents as the basis
	for _, mf := range mfs1.Contents {
		if hasl(mfs1.SkipList, mf.Filename) || hasglob(mfs1.SkipList, mf.Filename) {
			mf.Skip = true
		}
		if mf.Skip {
			if verbose {
				fmt.Printf("%s: skipping %s\n", filepath.Base(yamlfilename1), mf.Filename)
			}
			continue
		}
		if v, ok := hasfile[mf.Filename]; ok && v {
			if verbose {
				fmt.Printf("%s: Duplicate filename: %s\n", filepath.Base(yamlfilename1), mf.Filename)
			}
			continue
		}
		newmfs.Contents = append(newmfs.Contents, mf)
		hasfile[mf.Filename] = true
	}

	// Loop through the files in the metadata 2 and apply to the new contents
UP:
	for _, mf := range mfs2.Contents {
		if hasl(mfs2.SkipList, mf.Filename) || hasglob(mfs2.SkipList, mf.Filename) {
			mf.Skip = true
		}
		if mf.Skip {
			if verbose {
				fmt.Printf("%s: skipping %s\n", filepath.Base(yamlfilename2), mf.Filename)
			}
			continue
		}
		// If the filename exists in newmfs, overwrite it, if not, append it
		if v, ok := hasfile[mf.Filename]; ok && v {
			for i := range newmfs.Contents {
				if newmfs.Contents[i].Filename == mf.Filename {
					if verbose {
						fmt.Printf("%s from %s\n", mf.Filename, yamlfilename2)
					}
					newmfs.Contents[i] = mf
					continue UP
				}
			}
		} else {
			if verbose {
				fmt.Printf("%s from %s\n", mf.Filename, yamlfilename1)
			}
		}
	}

	var buf bytes.Buffer

	// Write the merged metadata to the new YAML file

	if yamldata, err := yaml.Marshal(&newmfs); err != nil {
		if err != nil {
			return err
		}
	} else {
		if _, err := buf.Write(yamldata); err != nil {
			return err
		}
	}

	if newfilename == "-" {
		// Write to stdout
		fmt.Print(buf.String())
	} else {
		// Check if the YAML file exists first
		if !force && exists(newfilename) {
			quit(fmt.Sprintf("%s already exists", newfilename))
		}
		// Write the YAML file
		if ioutil.WriteFile(newfilename, buf.Bytes(), 0644) != nil {
			return err
		}
	}

	return nil
}

func main() {
	arguments, _ := docopt.Parse(usage, nil, true, fmt.Sprintf("%s %v", metatarName, metatarVersion), false)

	//fmt.Println(arguments)

	yamlfilename := ""
	if !arguments["--list"].(bool) && !arguments["--yaml"].(bool) && !arguments["--merge"].(bool) && !arguments["--listcpio"].(bool) {
		var ok bool
		yamlfilename, ok = arguments["<yamlfile>"].(string)
		if !ok && arguments["<yamlfile>"] == nil {
			fmt.Println(usage)
			os.Exit(1)
		} else if ok && (strings.HasSuffix(".yml", yamlfilename) || strings.HasSuffix(".yaml", yamlfilename)) {
			// Filename is a string, but with the wrong extension
			quit(fmt.Sprintf("Invalid input YAML filename: %s", yamlfilename))
		}
	}

	yamlfilename1 := ""
	yamlfilename2 := ""
	if arguments["--merge"].(bool) {
		var ok bool
		yamlfilename1, ok = arguments["<yamlfile1>"].(string)
		if !ok && arguments["<yamlfile1>"] == nil {
			fmt.Println(usage)
			os.Exit(1)
		} else if ok && (strings.HasSuffix(".yml", yamlfilename1) || strings.HasSuffix(".yaml", yamlfilename1)) {
			// Filename is a string, but with the wrong extension
			quit(fmt.Sprintf("Invalid input YAML filename: %s", yamlfilename1))
		}
		yamlfilename2, ok = arguments["<yamlfile2>"].(string)
		if !ok && arguments["<yamlfile2>"] == nil {
			fmt.Println(usage)
			os.Exit(1)
		} else if ok && (strings.HasSuffix(".yml", yamlfilename2) || strings.HasSuffix(".yaml", yamlfilename2)) {
			// Filename is a string, but with the wrong extension
			quit(fmt.Sprintf("Invalid input YAML filename: %s", yamlfilename2))
		}
	}

	tarfilename := ""
	if !arguments["--generate"].(bool) && !arguments["--merge"].(bool) && !arguments["--listcpio"].(bool) {
		var ok bool
		tarfilename, ok = arguments["<tarfile>"].(string)
		if !ok && arguments["<tarfile>"] == nil {
			fmt.Println(usage)
			os.Exit(1)
		} else if !ok {
			quit(fmt.Sprintf("Invalid input tar filename: %s", tarfilename))
		}
	}

	cpiofilename := ""
	if arguments["--listcpio"].(bool) {
		var ok bool
		cpiofilename, ok = arguments["<cpiofile>"].(string)
		if !ok && arguments["<cpiofile>"] == nil {
			fmt.Println(usage)
			os.Exit(1)
		} else if !ok {
			quit(fmt.Sprintf("Invalid input tar filename: %s", cpiofilename))
		}
	}

	newfilename := ""
	if arguments["--apply"].(bool) || arguments["--generate"].(bool) || arguments["--merge"].(bool) {
		var ok bool
		newfilename, ok = arguments["<newfile>"].(string)
		if !ok && arguments["<newfile>"] == nil {
			fmt.Println(usage)
			os.Exit(1)
		} else if !ok {
			quit(fmt.Sprintf("Invalid output filename: %s", newfilename))
		}
	}

	force := arguments["--force"].(bool)
	verbose := arguments["--verbose"].(bool)
	withBody := arguments["--data"].(bool)
	expand := arguments["--expand"].(bool)
	root := arguments["--root"].(bool)
	writeCPIO := arguments["--cpio"].(bool)
	nouser := arguments["--nouser"].(bool)
	skipEmptyFiles := !arguments["--noskip"].(bool)

	if arguments["--apply"].(bool) {
		if writeCPIO {
			// Write a CPIO file
			check(ApplyMetadataToCpio(tarfilename, yamlfilename, newfilename, force, withBody, root, verbose, skipEmptyFiles))
		} else {
			// Write a TAR file
			check(ApplyMetadataToTar(tarfilename, yamlfilename, newfilename, force, withBody, verbose, skipEmptyFiles))
		}
	} else if arguments["--list"].(bool) {
		// Output contents of tar file
		check(ListTar(tarfilename))
	} else if arguments["--listcpio"].(bool) {
		// Output contents of cpio file
		check(ListCPIO(cpiofilename))
	} else if arguments["--yaml"].(bool) {
		// Output YAML metadata
		check(WriteMetadata(tarfilename, "-", force, withBody, verbose, expand, root, nouser))
	} else if arguments["--generate"].(bool) {
		// Convert YAML to tar or cpio, always use "Body:", if present
		check(ApplyMetadataToTar("", yamlfilename, newfilename, force, true, verbose, skipEmptyFiles))
	} else if arguments["--merge"].(bool) {
		check(MergeMetadata(yamlfilename1, yamlfilename2, newfilename, force, verbose))
	} else if tarfilename != "" && yamlfilename != "" {
		// Write a YAML file
		check(WriteMetadata(tarfilename, yamlfilename, force, withBody, verbose, expand, root, nouser))
	} else {
		fmt.Println(usage)
		os.Exit(1)
	}
}
