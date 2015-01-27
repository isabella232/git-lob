package main

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
)

const BUFSIZE = 131072

// Information about a LOB
type LOBInfo struct {
	// SHA of the LOB
	SHA string
	// Total size of the LOB (all chunks)
	Size int64
	// Number of chunks that make up the whole LOB (integrity check)
	NumChunks int
	// The chunk size that was used to divide this LOB (since can be configured separately)
	ChunkSize int64
}

// Custom error type to indicate an integrity error, listing problem SHAs
type IntegrityError struct {
	FailedSHAs []string
}

func (i *IntegrityError) Error() string {
	return fmt.Sprintf("One or more SHAs failed integrity: %v", i.FailedSHAs)
}

var cachedRepoRoot string
var cachedRepoRootIsSeparate bool
var cachedRepoRootWorkingDir string

// Gets the root folder of this git repository (the one containing .git)
func GetRepoRoot() (path string, isSeparateGitDir bool) {
	// We could call 'git rev-parse --git-dir' but this requires shelling out = slow, especially on Windows
	// We should try to avoid that whenever we can
	// So let's just find it ourselves; first containing folder with a .git folder/file
	curDir, err := os.Getwd()
	if err != nil {
		LogErrorf("Getwd failed: %v\n", err)
		return "", false
	}
	origCurDir := curDir
	// Use the cached value if known
	if cachedRepoRootWorkingDir == curDir && cachedRepoRoot != "" {
		return cachedRepoRoot, cachedRepoRootIsSeparate
	}

	for {
		exists, isDir := FileOrDirExists(filepath.Join(curDir, ".git"))
		if exists {
			// Store in cache to speed up
			cachedRepoRoot = curDir
			cachedRepoRootWorkingDir = origCurDir
			cachedRepoRootIsSeparate = !isDir
			return curDir, !isDir
		}
		curDir = filepath.Dir(curDir)
		if len(curDir) == 0 || curDir[len(curDir)-1] == filepath.Separator || curDir == "." {
			// Not a repo
			LogError("Couldn't find repo root, not a git folder")
			return "", false
		}
	}
}

// Gets the git data dir of git repository (the .git dir, or where .git file points)
func GetGitDir() string {
	root, isSeparate := GetRepoRoot()
	git := filepath.Join(root, ".git")
	if isSeparate {
		// Git repo folder is separate, read location from file
		filebytes, err := ioutil.ReadFile(git)
		if err != nil {
			LogErrorf("Can't read .git file %v: %v\n", git, err)
			return ""
		}
		filestr := string(filebytes)
		match := regexp.MustCompile("gitdir:[\\s]+([^\\r\\n]+)").FindStringSubmatch(filestr)
		if match == nil {
			LogErrorf("Unexpected contents of .git file %v: %v\n", git, filestr)
			return ""
		}
		// The text in the git dir will use cygwin-style separators, so normalise
		return filepath.Clean(match[1])
	} else {
		// Regular git dir
		return git
	}

}

// Gets the root directory for local LOB files & creates if necessary
func GetLocalLOBRoot() string {
	ret := filepath.Join(GetGitDir(), "git-lob", "content")
	err := os.MkdirAll(ret, 0755)
	if err != nil {
		LogErrorf("Unable to create LOB root folder at %v: %v", ret, err)
		panic(err)
	}
	return ret
}

// Gets the root directory for shared LOB files & creates if necessary
func GetSharedLOBRoot() string {
	// We create shared store when loading config if specified
	return GlobalOptions.SharedStore
}

// Get relative directory for some base dir for a given sha
func getLOBRelDir(sha string) string {
	return filepath.Join(sha[:3], sha[3:6])
}

// Get a relative file name for a meta file (no dirs created as not rooted)
func getLOBRelMetaFilename(sha string) string {
	return filepath.Join(getLOBRelDir(sha), getLOBMetaFilename(sha))
}

// Get a relative file name for a meta file (no dirs created as not rooted)
func getLOBRelChunkFilename(sha string, chunkIdx int) string {
	return filepath.Join(getLOBRelDir(sha), getLOBChunkFilename(sha, chunkIdx))
}

// Get absolute directory for a sha & creates it
func getLOBSubDir(base, sha string) string {
	ret := filepath.Join(base, getLOBRelDir(sha))
	err := os.MkdirAll(ret, 0755)
	if err != nil {
		LogErrorf("Unable to create LOB 2nd-level folder at %v: %v", ret, err)
		panic(err)
	}
	return ret

}

// Gets the containing local folder for a given LOB SHA & creates if necessary
// LOBs are 'splayed' 2-levels deep based on first 6 chars of SHA (3 for each dir)
// We splay by 2 levels and by 3 each (4096 dirs) because we don't pack like git
// so need to ensure directory contents remain practical at high numbers of files
func GetLocalLOBDir(sha string) string {
	if len(sha) != 40 {
		LogErrorf("Invalid SHA format: %v\n", sha)
		return ""
	}
	return getLOBSubDir(GetLocalLOBRoot(), sha)
}

// Gets the containing shared folder for a given LOB SHA & creates if necessary
// LOBs are 'splayed' 2-levels deep based on first 6 chars of SHA (3 for each dir)
// We splay by 2 levels and by 3 each (4096 dirs) because we don't pack like git
// so need to ensure directory contents remain practical at high numbers of files
func GetSharedLOBDir(sha string) string {
	if len(sha) != 40 {
		LogErrorf("Invalid SHA format: %v\n", sha)
		return ""
	}
	return getLOBSubDir(GetSharedLOBRoot(), sha)
}

// get the filename for a meta file (no dir)
func getLOBMetaFilename(sha string) string {
	return sha + "_meta"
}

// get the filename for a chunk file (no dir)
func getLOBChunkFilename(sha string, chunkIdx int) string {
	return fmt.Sprintf("%v_%d", sha, chunkIdx)
}

func getLocalLOBMetaFilename(sha string) string {
	fld := GetLocalLOBDir(sha)
	return filepath.Join(fld, getLOBMetaFilename(sha))
}
func getLocalLOBChunkFilename(sha string, chunkIdx int) string {
	fld := GetLocalLOBDir(sha)
	return filepath.Join(fld, getLOBChunkFilename(sha, chunkIdx))
}
func getSharedLOBMetaFilename(sha string) string {
	fld := GetSharedLOBDir(sha)
	return filepath.Join(fld, getLOBMetaFilename(sha))
}
func getSharedLOBChunkFilename(sha string, chunkIdx int) string {
	fld := GetSharedLOBDir(sha)
	return filepath.Join(fld, getLOBChunkFilename(sha, chunkIdx))
}

// Retrieve information about an existing stored LOB
func GetLOBInfo(sha string) (*LOBInfo, error) {
	meta := getLocalLOBMetaFilename(sha)
	infobytes, err := ioutil.ReadFile(meta)

	if err != nil {
		if os.IsNotExist(err) {
			// Try to recover from shared
			if recoverLocalLOBFilesFromSharedStore(sha) {
				infobytes, err = ioutil.ReadFile(meta)
				if err != nil {
					// Dang
					return nil, err
				}
				// otherwise we recovered!
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	// Read JSON metadata
	info := &LOBInfo{}
	err = json.Unmarshal(infobytes, info)
	if err != nil {
		// Fatal, corruption
		LogErrorf("Unable to interpret meta file %v: %v\n", meta, err)
		return nil, err
	}

	return info, nil

}

// If files are missing in the local repo but available in the shared
// store, returns true after re-establishing the link
// Note: this doesn't validate sizes of any files because it's assumed
// because of hardlinking the files are either missing entirely or the
// same as the shared store
func recoverLocalLOBFilesFromSharedStore(sha string) bool {
	if !isUsingSharedStorage() {
		return false
	}

	metalocal := getLocalLOBMetaFilename(sha)
	if !FileExists(metalocal) {
		metashared := getSharedLOBMetaFilename(sha)
		if FileExists(metashared) {
			err := linkSharedLOBFilename(metashared)
			if err != nil {
				return false
			}
		} else {
			return false
		}
	}
	// Meta should be complete & local now
	info, err := GetLOBInfo(sha)
	if err != nil {
		return false
	}
	for i := 0; i < info.NumChunks; i++ {
		local := getLocalLOBChunkFilename(sha, i)
		expectedSize := getLOBExpectedChunkSize(info, i)
		if !FileExistsAndIsOfSize(local, expectedSize) {
			shared := getSharedLOBChunkFilename(sha, i)
			if FileExistsAndIsOfSize(shared, expectedSize) {
				err := linkSharedLOBFilename(shared)
				if err != nil {
					return false
				}
			} else {
				return false
			}
		}
	}

	return true
}

// Retrieve LOB from storage
func RetrieveLOB(sha string, out io.Writer) (info *LOBInfo, err error) {
	info, err = GetLOBInfo(sha)

	if err != nil {
		if os.IsNotExist(err) {
			// OK we don't have this file yet
			// Potentially auto-download
			// TODO
			LogErrorf("LOB meta not found TODO AUTODOWNLOAD %v: %v\n", sha, err)
			return nil, err
		} else {
			// A problem
			LogErrorf("Unable to retrieve LOB with SHA %v: %v\n", sha, err)
			return nil, err
		}
	}

	var totalBytesRead = int64(0)
	fileSize := info.Size
	// Pre-validate all the files BEFORE we start streaming data to out
	// if we fail part way through we don't want to have written partial
	// data, should be all or nothing
	lastChunkSize := fileSize - (int64(info.NumChunks-1) * info.ChunkSize)
	// Check all files
	for i := 0; i < info.NumChunks; i++ {
		chunkFilename := getLocalLOBChunkFilename(sha, i)
		var expectedSize int64
		if i+1 < info.NumChunks {
			expectedSize = info.ChunkSize
		} else {
			if info.NumChunks == 1 {
				expectedSize = fileSize
			} else {
				expectedSize = lastChunkSize
			}
		}
		if !FileExistsAndIsOfSize(chunkFilename, expectedSize) {
			// Try to recover from shared store
			recoveredFromShared := false
			if recoverLocalLOBFilesFromSharedStore(sha) {
				recoveredFromShared = FileExistsAndIsOfSize(chunkFilename, expectedSize)
			}

			if !recoveredFromShared {
				// TODO auto-download?
				LogErrorf("LOB file not found or wrong size TODO AUTODOWNLOAD: %v expected to be %d bytes\n", chunkFilename, expectedSize)
				return info, err
			}
		}
	}
	// If all was well, start reading & streaming content
	for i := 0; i < info.NumChunks; i++ {
		// Check each chunk file exists
		chunkFilename := getLocalLOBChunkFilename(info.SHA, i)
		in, err := os.OpenFile(chunkFilename, os.O_RDONLY, 0644)
		if err != nil {
			LogErrorf("Error reading LOB file %v: %v\n", chunkFilename, err)
			return info, err
		}
		c, err := io.Copy(out, in)
		if err != nil {
			LogErrorf("I/O error while copying LOB file %v, check working copy state\n", chunkFilename)
			return info, err
		}
		totalBytesRead += c
	}

	// Final check
	if totalBytesRead != fileSize {
		err = errors.New(fmt.Sprintf("Error, file length does not match expected in LOB %v, expected %d, total size %d", sha, fileSize, totalBytesRead))
		LogErrorf(err.Error())
		return info, err
	}

	LogDebugf("Successfully retrieved LOB %v from %d chunks, total size %d\n", sha, info.NumChunks, totalBytesRead)

	return info, nil

}

// Link a file from shared storage into the local repo
// The hard link means we only ever have one copy of the data
// but it appears under each repo's git-lob folder
// destFile should be a full path of shared file location
func linkSharedLOBFilename(destSharedFile string) error {
	// Get path relative to shared store root, then translate it to local path
	relPath, err := filepath.Rel(GlobalOptions.SharedStore, destSharedFile)
	if err != nil {
		return err
	}
	linkPath := filepath.Join(GetLocalLOBRoot(), relPath)

	// Make sure path exists since we're not using utility method to link
	os.MkdirAll(filepath.Dir(linkPath), 0755)

	os.Remove(linkPath)
	err = CreateHardLink(destSharedFile, linkPath)
	if err != nil {
		LogErrorf("Error creating hard link from %v to %v: %v\n", linkPath, destSharedFile, err)
		return err
	}
	return nil
}

// Store the metadata for a given sha
// If it already exists and is of the right size, will do nothing
func storeLOBInfo(info *LOBInfo) error {
	infoBytes, err := json.Marshal(info)
	if err != nil {
		LogErrorf("Unable to convert LOB info to JSON: %v\n", err)
		return err
	}
	var infoFilename string
	if isUsingSharedStorage() {
		infoFilename = getSharedLOBMetaFilename(info.SHA)
	} else {
		infoFilename = getLocalLOBMetaFilename(info.SHA)
	}
	if !FileExistsAndIsOfSize(infoFilename, int64(len(infoBytes))) {
		// Since all the details are derived from the SHA the only variant is chunking or incomplete writes so
		// we don't need to worry about needing to update the content (it must be correct)
		LogDebugf("Writing LOB metadata file: %v\n", infoFilename)
		err = ioutil.WriteFile(infoFilename, infoBytes, 0644)
		if err != nil {
			return err
		}
	} else {
		LogDebugf("LOB metadata file already exists & is valid: %v\n", infoFilename)
	}

	// This may have stored in shared storage, so link if required
	if isUsingSharedStorage() {
		return linkSharedLOBFilename(infoFilename)
	} else {
		return nil
	}

}

func isUsingSharedStorage() bool {
	if GlobalOptions.SharedStore != "" {
		// We create the folder on loading config
		return DirExists(GlobalOptions.SharedStore)
	}
	return false
}

// Write the contents of fromFile to final storage with sha, checking the size
// If file already exists and is of the right size, will do nothing
func storeLOBChunk(sha string, chunkNo int, fromChunkFile string, sz int64) error {
	var destFile string

	if isUsingSharedStorage() {
		destFile = getSharedLOBChunkFilename(sha, chunkNo)
	} else {
		destFile = getLocalLOBChunkFilename(sha, chunkNo)
	}
	if !FileExistsAndIsOfSize(destFile, int64(sz)) {
		LogDebugf("Saving final LOB metadata file: %v\n", destFile)
		// delete any existing (incorrectly sized) file since will probably not be allowed to rename over it
		// ignore any errors
		os.Remove(destFile)
		err := os.Rename(fromChunkFile, destFile)
		if err != nil {
			return err
		}
	} else {
		LogDebugf("LOB chunk file already exists & is valid: %v\n", destFile)
	}

	// This may have stored in shared storage, so link if required
	if isUsingSharedStorage() {
		return linkSharedLOBFilename(destFile)
	}
	return nil

}

// Read from a stream and calculate SHA, while also writing content to chunked content
// leader is a slice of bytes that has already been read (probe for SHA)
func StoreLOB(in io.Reader, leader []byte) (*LOBInfo, error) {

	sha := sha1.New()
	// Write chunks to temporary files, then move based on SHA filename once calculated
	chunkFilenames := make([]string, 0, 5)

	var outf *os.File
	var err error
	writeLeader := true
	buf := make([]byte, BUFSIZE)
	var fatalError error
	var currentChunkSize int64 = 0
	var totalSize int64 = 0

	for {
		var dataToWrite []byte

		if writeLeader && len(leader) > 0 {
			LogDebugf("Writing leader of size %d\n", len(leader))
			dataToWrite = leader
			writeLeader = false
		} else {
			var bytesToRead int64 = BUFSIZE
			if BUFSIZE+currentChunkSize > GlobalOptions.ChunkSize {
				// Read less than BUFSIZE so we stick to CHUNKLIMIT
				bytesToRead = GlobalOptions.ChunkSize - currentChunkSize
			}
			c, err := in.Read(buf[:bytesToRead])
			// Write any data to SHA & output
			if c > 0 {
				dataToWrite = buf[:c]
			} else if err != nil {
				if err == io.EOF {
					// End of input
					outf.Close()
					break
				} else {
					LogErrorf("I/O error reading chunk %d: %v", len(chunkFilenames), err)
					outf.Close()
					fatalError = err
					break
				}
			}

		}

		// Write data
		if len(dataToWrite) > 0 {
			// New chunk file?
			if outf == nil {
				outf, err = ioutil.TempFile("", "tempchunk")
				if err != nil {
					LogErrorf("Unable to create chunk %d: %v\n", len(chunkFilenames), err)
					fatalError = err
					break
				}
				LogDebugf("Creating temporary chunk file #%d: %v\n", len(chunkFilenames), outf.Name())
				chunkFilenames = append(chunkFilenames, outf.Name())
				currentChunkSize = 0
			}
			sha.Write(dataToWrite)
			c, err := outf.Write(dataToWrite)
			if err != nil {
				LogErrorf("I/O error writing chunk: %v wrote %d bytes of %d\n", err, c, len(dataToWrite))
				fatalError = err
				break
			}
			currentChunkSize += int64(c)
			totalSize += int64(c)

			// Read from incoming
			// Deal with chunk limit
			if currentChunkSize >= GlobalOptions.ChunkSize {
				// Close this output, next iteration will create the next file
				outf.Close()
				outf = nil
				currentChunkSize = 0
			}
		} else {
			// No data to write
			outf.Close()
			break
		}
	}

	if fatalError != nil {
		// Clean up temporaries
		for _, f := range chunkFilenames {
			os.Remove(f)
		}
		return nil, fatalError
	}

	shaStr := fmt.Sprintf("%x", string(sha.Sum(nil)))

	// We *may* now move the data to LOB dir
	// We won't if it already exists & is the correct size
	// Construct LOBInfo & write to final location
	info := &LOBInfo{SHA: shaStr, Size: totalSize, NumChunks: len(chunkFilenames), ChunkSize: GlobalOptions.ChunkSize}
	err = storeLOBInfo(info)

	// Check each chunk file
	for i, f := range chunkFilenames {
		sz := GlobalOptions.ChunkSize
		if i+1 == len(chunkFilenames) {
			// Last chunk, get size
			sz = currentChunkSize
		}
		storeLOBChunk(shaStr, i, f, sz)
	}

	return info, nil

}

// Delete all files associated with a given LOB SHA
func DeleteLOB(sha string) error {
	// Delete from local always (either only copy, or hard link)
	localdir := GetLocalLOBDir(sha)

	names, err := filepath.Glob(filepath.Join(localdir, fmt.Sprintf("%v*", sha)))
	if err != nil {
		LogErrorf("Unable to glob local files for %v: %v\n", sha, err)
		return err
	}
	for _, n := range names {
		err = os.Remove(n)
		if err != nil {
			LogErrorf("Unable to delete file %v: %v\n", n, err)
			return err
		}
		LogDebugf("Deleted %v\n", n)
	}

	if isUsingSharedStorage() {
		// If we're using shared storage, then also check the number of links in
		// shared storage for this SHA. See PruneSharedStore for a more general
		// sweep for files that don't go through DeleteLOB (e.g. repo deleted manually)
		shareddir := GetSharedLOBDir(sha)
		names, err := filepath.Glob(filepath.Join(shareddir, fmt.Sprintf("%v*", sha)))
		if err != nil {
			LogErrorf("Unable to glob shared files for %v: %v\n", sha, err)
			return err
		}
		for _, n := range names {
			links, err := GetHardLinkCount(n)
			if err == nil && links == 1 {
				// only 1 hard link means no other repo refers to this shared LOB
				// so it's safe to delete it
				err = os.Remove(n)
				if err != nil {
					LogErrorf("Unable to delete file %v: %v\n", n, err)
					return err
				}
				LogDebugf("Deleted shared file %v\n", n)
			}

		}

	}

	return nil

}

// Get the local/shared storage of a LOB with a given SHA
// Returns the list of files (relative to basedir) & checks for
// integrity if check = true
// If check = true and checkHash = true, reads all the data in the files and re-calculates
// the SHA for a deep validation of content
// If check = true and checkHash = false, just checks the presence & size of all files
// If there are any errors the returned list may not be correct
// In the rare case that a break has occurred between shared storage
// and the local hardlink, this method will re-link if the shared
// store has it
func GetLOBFilesForSHA(sha, basedir string, check bool, checkHash bool) (files []string, size int64, _err error) {
	var ret []string
	info, err := GetLOBInfo(sha)
	if err != nil {
		return []string{}, 0, err
	}
	// add meta file (relative) - already checked by GetLOBInfo above
	relmeta := getLOBRelMetaFilename(sha)
	ret = append(ret, relmeta)

	var shaRecalc hash.Hash
	if checkHash {
		shaRecalc = sha1.New()
	}
	lastChunkSize := info.Size - (int64(info.NumChunks-1) * info.ChunkSize)
	for i := 0; i < info.NumChunks; i++ {
		relchunk := getLOBRelChunkFilename(sha, i)
		ret = append(ret, relchunk)
		if check {
			abschunk := filepath.Join(basedir, relchunk)
			// Check size first
			var expectedSize int64
			if i+1 < info.NumChunks {
				expectedSize = info.ChunkSize
			} else {
				if info.NumChunks == 1 {
					expectedSize = info.Size
				} else {
					expectedSize = lastChunkSize
				}
			}
			if !FileExistsAndIsOfSize(abschunk, expectedSize) {
				// Try to recover from shared store
				recoveredFromShared := false
				if recoverLocalLOBFilesFromSharedStore(sha) {
					recoveredFromShared = FileExistsAndIsOfSize(abschunk, expectedSize)
				}

				if !recoveredFromShared {
					msg := fmt.Sprintf("LOB file not found or wrong size: %v expected to be %d bytes", abschunk, expectedSize)
					LogError(msg)
					return ret, info.Size, errors.New(msg)
				}
			}

			// Check SHA content?
			if checkHash {
				f, err := os.OpenFile(abschunk, os.O_RDONLY, 0644)
				if err != nil {
					LogErrorf("Error opening LOB file %v to check SHA: %v\n", abschunk, err)
					return ret, info.Size, err
				}
				_, err = io.Copy(shaRecalc, f)
				if err != nil {
					LogErrorf("Error copying LOB file %v into SHA calculator: %v\n", abschunk, err)
					return ret, info.Size, err
				}
				f.Close()
			}

		}
	}

	if check && checkHash {
		shaRecalcStr := fmt.Sprintf("%x", string(shaRecalc.Sum(nil)))
		if sha != shaRecalcStr {
			msg := fmt.Sprintf("Integrity error; content of files for LOB SHA %v actually have SHA %v", sha, shaRecalcStr)
			LogErrorf("%v\n", msg)
			return ret, info.Size, errors.New(msg)
		}
	}

	return ret, info.Size, nil

}

// Check the integrity of the files for a given sha in the attached basedir
// If checkHash = true, reads all the data in the files and re-calculates
// the SHA for a deep validation of content (slower but complete)
// If ccheckHash = false, just checks the presence & size of all files (quick & most likely correct)
func CheckLOBFilesForSHA(sha, basedir string, checkHash bool) error {
	_, _, err := GetLOBFilesForSHA(sha, basedir, true, checkHash)
	return err
}

// Retrieve the list of local/shared filenames backing the list of LOB SHAs passed in
// This finds this machine's storage of the SHAs in question, including the metadata file and
// all of the chunks. If check = true (recommended) then the integrity of the files
// is checked and only if all the files for a SHA are valid are they included in the
// returned list. In this case the error return is an IntegrityError and includes the bad SHAs
// The filenames returned are relative to basedir, the root folder for all of the files
// Note that 'check' only checks the surface level integrity (all the files are there & correct size). If you
// want to do a deep integrity check (ensure all bytes are valid), use CheckLOBFilesForSHA with checkHash=true
func GetLOBFilenamesWithBaseDir(shas []string, check bool) (files []string, basedir string, totalSize int64, err error) {
	// Note how we always return the basedir as the local LOB root
	// this is because all SHAs are hard linked here even when using shared storage
	basedir = GetLocalLOBRoot()
	var ret []string
	var errorshas []string
	var retSize int64
	for _, sha := range shas {
		// Do basic check, not content check
		shafiles, shasize, shaerr := GetLOBFilesForSHA(sha, basedir, check, false)
		if shaerr != nil {
			errorshas = append(errorshas, sha)
		} else {
			ret = append(ret, shafiles...)
			retSize += shasize
		}
	}
	if len(errorshas) > 0 {
		return ret, basedir, retSize, &IntegrityError{errorshas}
	}
	return ret, basedir, retSize, nil
}

// Get the correct size of a given chunk
func getLOBExpectedChunkSize(info *LOBInfo, chunkIdx int) int64 {
	if chunkIdx+1 < info.NumChunks {
		return info.ChunkSize
	} else {
		if info.NumChunks == 1 {
			return info.Size
		} else {
			return info.Size - (int64(info.NumChunks-1) * info.ChunkSize)
		}
	}

}
