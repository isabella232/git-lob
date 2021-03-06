package core

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/atlassian/git-lob/providers"
	"github.com/atlassian/git-lob/util"
)

type PruneCallbackType int

const (
	// Prune is working (for spinner)
	PruneWorking PruneCallbackType = iota
	// Prune is retaining LOB because referenced
	PruneRetainReferenced PruneCallbackType = iota
	// Prune is retaining LOB because commit referencing it is within retention period
	PruneRetainByDate PruneCallbackType = iota
	// Prune is retaining LOB because commit is referencing it is not pushed
	PruneRetainNotPushed PruneCallbackType = iota
	// Prune is deleting LOB (because unreferenced or out of date range & pushed)
	PruneDeleted PruneCallbackType = iota
)

// Callback when running prune, identifies what's going on
// When in dry run mode the same callbacks are made even if the actual act isn't performed (e.g. deletion)
type PruneCallback func(t PruneCallbackType, lobsha string)

var (
	diffLOBReferenceRegex *regexp.Regexp
	lobFilenameRegex      *regexp.Regexp
)

// Retrieve the full set of SHAs that currently have files locally (complete or not)
func getAllLocalLOBSHAs() (util.StringSet, error) {
	return getAllLOBSHAsInDir(GetLocalLOBRoot())
}

// Retrieve the full set of SHAs that currently have files in the shared store (complete or not)
func getAllSharedLOBSHAs() (util.StringSet, error) {
	return getAllLOBSHAsInDir(GetSharedLOBRoot())
}

func getAllLOBSHAsInDir(lobroot string) (util.StringSet, error) {

	// os.File.Readdirnames is the most efficient
	// os.File.Readdir retrieves extra info we don't usually need but in case other unexpected files
	// end up in there (e.g. .DS_Store), we use it to identify directories
	// ioutil.ReadDir and filepath.Walk do sorting which is unnecessary & inefficient

	if lobFilenameRegex == nil {
		lobFilenameRegex = regexp.MustCompile(`^([A-Za-z0-9]{40})_(meta|\d+)$`)
	}
	// Readdir returns in 'directory order' which means we may not get files for same SHA together
	// so use set to find uniques
	ret := util.NewStringSet()

	// We only need to support a 2-folder structure here & know that all files are at the bottom level
	// We always work on the local LOB folder (either only copy or hard link)
	rootf, err := os.Open(lobroot)
	if err != nil {
		return ret, errors.New(fmt.Sprintf("Unable to open LOB root: %v\n", err))
	}
	defer rootf.Close()
	dir1, err := rootf.Readdir(0)
	if err != nil {
		return ret, errors.New(fmt.Sprintf("Unable to read first level LOB dir: %v\n", err))
	}
	for _, dir1fi := range dir1 {
		if dir1fi.IsDir() {
			dir1path := filepath.Join(lobroot, dir1fi.Name())
			dir1f, err := os.Open(dir1path)
			if err != nil {
				return ret, errors.New(fmt.Sprintf("Unable to open LOB dir: %v\n", err))
			}
			defer dir1f.Close()
			dir2, err := dir1f.Readdir(0)
			if err != nil {
				return ret, errors.New(fmt.Sprintf("Unable to read second level LOB dir: %v\n", err))
			}
			for _, dir2fi := range dir2 {
				if dir2fi.IsDir() {
					dir2path := filepath.Join(dir1path, dir2fi.Name())
					dir2f, err := os.Open(dir2path)
					if err != nil {
						return ret, errors.New(fmt.Sprintf("Unable to open LOB dir: %v\n", err))
					}
					defer dir2f.Close()
					lobnames, err := dir2f.Readdirnames(0)
					if err != nil {
						return ret, errors.New(fmt.Sprintf("Unable to read innermost LOB dir: %v\n", err))
					}
					for _, lobname := range lobnames {
						// Make sure it's really a LOB file
						if match := lobFilenameRegex.FindStringSubmatch(lobname); match != nil {
							// Regex pulls out the SHA
							sha := match[1]
							ret.Add(sha)
						}
					}

				}
			}
		}

	}

	return ret, nil

}

// Determine if a line from git diff output is referencing a LOB (returns "" if not)
func lobReferenceFromDiffLine(line string) string {
	// Because this is a diff, it will start with +/-
	// We only care about +, since - is stopping referencing a SHA
	// important when it comes to purging old files
	if diffLOBReferenceRegex == nil {
		diffLOBReferenceRegex = regexp.MustCompile(`^\+git-lob: ([A-Za-z0-9]{40})$`)
	}

	if match := diffLOBReferenceRegex.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	return ""
}

// Delete unreferenced binary files from local store
// For a file to be deleted it needs to not be referenced by any (reachable) commit
// Returns a list of SHAs that were deleted (unless dryRun = true)
func PruneUnreferenced(dryRun bool, callback PruneCallback) ([]string, error) {
	// Purging requires full git on the command line, no way around this really
	cmd := exec.Command("git", "log", "--all", "--no-color", "--oneline", "-p", "-G", SHALineRegexStr)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return make([]string, 0), errors.New("Unable to query git log for binary references: " + err.Error())
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return make([]string, 0), errors.New("Unable to open pipe: " + err.Error())
	}
	multi := io.MultiReader(stdout, stderr)
	scanner := bufio.NewScanner(multi)
	cmd.Start()
	referencedSHAs := util.NewStringSet()
	for scanner.Scan() {
		callback(PruneWorking, "")
		line := scanner.Text()
		if sha := lobReferenceFromDiffLine(line); sha != "" {
			if referencedSHAs.Add(sha) {
				callback(PruneRetainReferenced, sha)
			}
		}
	}
	cmd.Wait()

	// Must also not prune anything that's added but uncommitted
	cmd = exec.Command("git", "diff", "--cached", "--no-color", "-G", SHALineRegexStr)
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return make([]string, 0), errors.New("Unable to query git index for binary references: " + err.Error())
	}
	scanner = bufio.NewScanner(stdout)
	cmd.Start()
	for scanner.Scan() {
		callback(PruneWorking, "")
		line := scanner.Text()
		if sha := lobReferenceFromDiffLine(line); sha != "" {
			if referencedSHAs.Add(sha) {
				callback(PruneRetainReferenced, sha)
			}
		}
	}
	cmd.Wait()

	fileSHAs, err := getAllLocalLOBSHAs()
	if err == nil {

		var ret []string
		for sha := range fileSHAs.Iter() {
			callback(PruneWorking, "")
			if !referencedSHAs.Contains(sha) {
				ret = append(ret, string(sha))
				callback(PruneDeleted, sha)
				if !dryRun {
					DeleteLOB(string(sha))
				}
			}
		}
		return ret, nil
	} else {
		return make([]string, 0), errors.New("Unable to get list of binary files: " + err.Error())
	}

}

// Remove LOBs from the local store if they fall outside the range we would normally fetch for
// Returns a list of SHAs that were deleted (unless dryRun = true)
// Unreferenced binaries are also deleted by this
func PruneOld(dryRun, safeMode bool, callback PruneCallback) ([]string, error) {
	refSHAsDone := util.NewStringSet()
	// Build a list to keep, then delete all else (includes deleting unreferenced)
	// Can't just look at diffs (just like fetch) since LOB changed 3 years ago but still valid = recent
	retainSet := util.NewStringSet()

	// Add LOBs to retainSet for this commit and history
	retainLOBs := func(commit string, days int, notPushedScanOnly bool, remoteName string) error {

		var err error
		var earliestCommit string
		if notPushedScanOnly {
			// We only want to include lobs from this ref if not pushed
			earliestCommit = commit
			// we never have to snapshot the file system because we're only interested in
			// lobs which haven't been pushed. If that's all of them, then we'll eventually
			// find the original addition of the lob in the history anyway
		} else {
			callback(PruneWorking, "")
			// This ref is itself included so perform usual 'all lobs at checkout + n days history' query
			var lobs []string
			lobs, earliestCommit, err = GetGitAllLOBsToCheckoutAtCommitAndRecent(commit, days, []string{}, []string{})
			if err != nil {
				return fmt.Errorf("Error determining recent commits from %v: %v", commit, err.Error())
			}
			for _, l := range lobs {
				if retainSet.Add(l) {
					callback(PruneRetainByDate, l)
				}
			}
		}

		// earliestCommit is the earliest one which changed (replaced) a binary SHA
		// and therefore the SHA we pulled out of it applied UP TO that point
		// that we've included in the lobs list already
		// If this commit is pushed then we're OK, if not we have to go backwards
		// until we find the one that is.
		// A pushed commit indicates the SHA pulled out of the *following* commit
		// has been pushed:
		//
		// Binary A <-- --> B          B <-- --> C               C <-- --> D
		// ------------|-----------|--------|-------------------------|
		// Commit      1           |        2                         3
		// "Retention"             R
		//
		// Given 3 commits (1/2/3) each changing a binary through states A/B/C/D
		// 1. We retrieve state D through ls-files
		// 2. We retrieve statees B and C through log --since=R, since we pick up
		//    commits 2 and 3 and hence the SHAs for C and then B from the '-' side of the diff
		// 3. 'Earliest commit' is 2
		// 4. We then walk all commits that are at 2 or ancestors which reference LOBs
		//    and are not pushed (this happens forwards from earliest up to & including 2)
		//    This actually picks up the '+' sides of the diff

		// This switching between using '-' and '+' lines of diff might seem odd but using
		// the '-' lines is the easiest way to get required state in between commits. When
		// your threshold date is in between commits you actually want the SHA from the commit
		// before which changed that file, which is awkward & could be different for every file.
		// Using the '-' lines eliminates that issue & also lets us just use git log --since.
		// When you're looking at commits (rather than between them) you can use '+' which is easier

		// WalkGitCommitLOBsToPush already finds the earliest commits that are not pushed before / on a ref
		// so we use that plus a walk function
		walkHistoryFunc := func(commitLOB *CommitLOBRef) (quit bool, err error) {
			callback(PruneWorking, "")

			// we asked to be told about the '+' side of the diff for LOBs while doing this walk,
			// so that it corresponds with the push flag. Snapshots above include that already, so
			// here we only deal with differences.
			// We have to use the '-' diffs *between* commits (arbitrary date), but can use '+' when *on* commits
			for _, l := range commitLOB.LobSHAs {
				if retainSet.Add(l) {
					callback(PruneRetainNotPushed, l)
				}
			}

			return false, nil

		}

		// Now walk all unpushed commits referencing LOBs that are earlier than this
		err = WalkGitCommitLOBsToPush(remoteName, earliestCommit, false, walkHistoryFunc)

		return nil

	}

	// What remote(s) do we check for push? Defaults to "origin"
	remoteName := util.GlobalOptions.PruneRemote

	// First, include HEAD (we always want to keep that)
	util.LogConsoleDebugf("\r") // to reset any progress spinner but don't want \r in log
	util.LogDebugf("Retaining HEAD and %dd of history\n", util.GlobalOptions.RetentionCommitsPeriodHEAD)
	headsha, _ := GitRefToFullSHA("HEAD")
	err := retainLOBs(headsha, util.GlobalOptions.RetentionCommitsPeriodHEAD, false, remoteName)
	if err != nil {
		return []string{}, err
	}
	refSHAsDone.Add(headsha)

	// Get all refs - we get all refs and not just recent refs like fetch, because we should
	// not purge binaries in old refs if they are not pushed. However we get them in date order
	// so that we don't have to check date once we cross retention-period-refs threshold
	refs, err := GetGitRecentRefs(-1, true, "")
	if err != nil {
		return []string{}, err
	}
	outsideRefRetention := false
	earliestRefDate := time.Now().AddDate(0, 0, -util.GlobalOptions.RetentionRefsPeriod)
	for _, ref := range refs {
		callback(PruneWorking, "")
		// Don't duplicate work when >1 ref has the same SHA
		// Most common with HEAD if not detached but also tags
		if refSHAsDone.Contains(ref.CommitSHA) {
			continue
		}
		refSHAsDone.Add(ref.CommitSHA)

		notPushedScanOnly := false
		// Is the ref out of the retention-period-refs window already? If so jump straight to push check
		// refs are reverse date ordered so once we've found one that's outside, all following are too
		if outsideRefRetention {
			// previus ref being ouside ref retention manes this one is too (date ordered), save time
			notPushedScanOnly = true
		} else {
			// check individual date
			commit, err := GetGitCommitSummary(ref.CommitSHA)
			if err != nil {
				// We can't tell when this was last committed, so be safe & assume it's recent
			} else if commit.CommitDate.Before(earliestRefDate) {
				// this ref is already out of retention, so only keep if not pushed
				notPushedScanOnly = true
				// all subseqent refs are earlier
				outsideRefRetention = true
			}
		}

		if !notPushedScanOnly {
			util.LogConsoleDebugf("\r") // to reset any progress spinner but don't want \r in log
			util.LogDebugf("Retaining %v and %dd of history\n", ref.Name, util.GlobalOptions.RetentionCommitsPeriodOther)
		}

		// LOBs to keep for this ref
		err := retainLOBs(ref.CommitSHA, util.GlobalOptions.RetentionCommitsPeriodOther, notPushedScanOnly, remoteName)
		if err != nil {
			return []string{}, fmt.Errorf("Error determining LOBs to keep for %v: %v", err.Error())
		}

	}

	var provider providers.SyncProvider
	safeRemote := "origin"
	if safeMode {
		if util.GlobalOptions.PruneRemote != "" {
			safeRemote = util.GlobalOptions.PruneRemote
			if safeRemote == "*" {
				remotes, err := GetGitRemotes()
				if err != nil {
					return []string{}, fmt.Errorf("Can't determine remotes to check in safe mode for '*': %v", err.Error())
				}
				if len(remotes) == 0 {
					return []string{}, fmt.Errorf("No remotes exist, cannot prune anything in --safe mode")
				}

				for _, remote := range remotes {
					// default to origin if present
					if remote == "origin" {
						safeRemote = remote
						break
					}
				}
				// If not found, use the first one
				if safeRemote == "*" {
					safeRemote = remotes[0]
				}
			}
		}
		var err error
		provider, err = providers.GetProviderForRemote(safeRemote)
		if err != nil {
			return []string{}, err
		}
		if err = provider.ValidateConfig(safeRemote); err != nil {
			return []string{}, fmt.Errorf("Remote %v has configuration problems:\n%v", safeRemote, err)
		}

	}
	var removedList []string
	localLOBs, err := getAllLocalLOBSHAs()
	if err == nil {
		for sha := range localLOBs.Iter() {
			callback(PruneWorking, "")
			if !retainSet.Contains(sha) {
				if safeMode {
					// check with remote before deleting
					if CheckRemoteLOBFilesForSHA(sha, provider, safeRemote) != nil {
						util.LogDebugf("Would have deleted %v but it does not exist on the remote %v, so keeping", sha, safeRemote)
						continue
					}
				}
				removedList = append(removedList, string(sha))
				callback(PruneDeleted, sha)
				if !dryRun {
					DeleteLOB(string(sha))
				}
			}
		}
	} else {
		return []string{}, errors.New("Unable to get list of binary files: " + err.Error())
	}
	util.LogConsoleDebugf("\r") // to reset any progress spinner but don't want \r in log
	util.LogDebugf("Also retained everything that hasn't been pushed to %v\n", remoteName)

	return removedList, nil
}

// Prune the shared store of all LOBs with only 1 hard link (itself)
// DeleteLOB will do this for individual LOBs we prune, but if the user
// manually deletes a repo then unreferenced shared LOBs may never be cleaned up
// callback is a basic function to let caller know something is happening
func PruneSharedStore(dryRun bool, callback PruneCallback) ([]string, error) {
	fileSHAs, err := getAllSharedLOBSHAs()
	if err == nil {
		ret := make([]string, 0, 10)
		for sha := range fileSHAs.Iter() {
			shareddir := GetSharedLOBDir(sha)
			names, err := filepath.Glob(filepath.Join(shareddir, fmt.Sprintf("%v*", sha)))
			if err != nil {
				return make([]string, 0), errors.New(fmt.Sprintf("Unable to glob shared files for %v: %v\n", sha, err))
			}
			var deleted bool = false
			var lastsha string
			for _, n := range names {
				callback(PruneWorking, "")
				links, err := GetHardLinkCount(n)
				if err == nil && links == 1 {
					// only 1 hard link means no other repo refers to this shared LOB
					// so it's safe to delete it
					deleted = true
					sha = filepath.Base(n)[:40]
					if lastsha != sha {
						callback(PruneDeleted, sha)
						lastsha = sha
					}
					if !dryRun {
						err = os.Remove(n)
						if err != nil {
							// don't abort for 1 failure, report & carry on
							util.LogErrorf("Unable to delete file %v: %v\n", n, err)
						}
					}
				}
			}
			if deleted {
				ret = append(ret, string(sha))
			}
		}
		return ret, nil
	} else {
		return make([]string, 0), err
	}

}
