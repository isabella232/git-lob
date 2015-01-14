package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A git reference or reference range

type GitRefSpec struct {
	// First ref
	Ref1 string
	// Optional range operator if this is a range refspec (".." or "...")
	RangeOp string
	// Optional second ref
	Ref2 string
}

// Some top level information about a commit (only first line of message)
type GitCommitSummary struct {
	SHA            string
	ShortSHA       string
	Parents        []string
	CommitDate     time.Time
	AuthorDate     time.Time
	AuthorName     string
	AuthorEmail    string
	CommitterName  string
	CommitterEmail string
	Subject        string
}

// Returns whether a GitRefSpec is a range or not
func (r *GitRefSpec) IsRange() bool {
	return (r.RangeOp == ".." || r.RangeOp == "...") &&
		r.Ref1 != "" && r.Ref2 != ""
}

// Returns whether a GitRefSpec is an empty range (using the same ref for start & end)
func (r *GitRefSpec) IsEmptyRange() bool {
	return (r.RangeOp == ".." || r.RangeOp == "...") &&
		r.Ref1 != "" && r.Ref1 == r.Ref2
}

func (r *GitRefSpec) String() string {
	if r.IsRange() {
		return fmt.Sprintf("%v%v%v", r.Ref1, r.RangeOp, r.Ref2)
	} else {
		return r.Ref1
	}
}

// A record of a set of LOB shas that are associated with a commit
type CommitLOBRef struct {
	commit  string
	lobSHAs []string
}

// Walk first parents starting from startSHA and call callback
// First call will be startSHA & its parent
// Parent will be blank string if there are no more parents & walk will stop after
// Optimises internally to call Git only for batches of 50
func WalkGitHistory(startSHA string, callback func(currentSHA, parentSHA string) (quit bool, err error)) error {

	quit := false
	currentLogHEAD := startSHA
	var callbackError error
	for !quit {
		// get 50 parents
		// format as <SHA> <PARENT> so we can detect the end of history
		cmd := exec.Command("git", "log", "--first-parent", "--topo-order",
			"-n", "50", "--format=%H %P", currentLogHEAD)

		outp, err := cmd.StdoutPipe()
		if err != nil {
			LogErrorf("Unable to list commits from %v: %v", currentLogHEAD, err.Error())
			return err
		}
		cmd.Start()
		scanner := bufio.NewScanner(outp)
		var currentLine string
		var parentSHA string
		for scanner.Scan() {
			currentLine = scanner.Text()
			currentSHA := currentLine[:40]
			// If we got here, we still haven't found an ancestor that was already marked
			// check next batch, provided there's a parent on the last one
			// 81 chars long, 2x40 SHAs + space
			if len(currentLine) >= 81 {
				parentSHA = strings.TrimSpace(currentLine[41:81])
			} else {
				parentSHA = ""
			}
			quit, callbackError = callback(currentSHA, parentSHA)
			if quit {
				break
			}
		}
		cmd.Wait()
		// End of history
		if parentSHA == "" {
			break
		} else {
			currentLogHEAD = parentSHA
		}
	}
	return callbackError
}

// Gets the default push remote for the working dir
// Determined from branch.*.remote configuration for the
// current branch if present, or defaults to origin.
func GetGitDefaultRemoteForPush() string {

	remote, ok := GlobalOptions.GitConfig[fmt.Sprintf("branch.%v.remote", GetGitCurrentBranch())]
	if ok {
		return remote
	}
	return "origin"

}

// Gets the default fetch remote for the working dir
// Determined from tracking state of current branch
// if present, or defaults to origin.
func GetGitDefaultRemoteForPull() string {

	remoteName, _ := GetGitUpstreamBranch(GetGitCurrentBranch())
	if remoteName != "" {
		return remoteName
	}
	return "origin"
}

var cachedCurrentBranch string

// Get the name of the current branch
func GetGitCurrentBranch() string {
	// Use cache, we never switch branches ourselves within lifetime so save some
	// repeat calls if queried more than once
	if cachedCurrentBranch == "" {
		cmd := exec.Command("git", "branch")

		outp, err := cmd.StdoutPipe()
		if err != nil {
			LogErrorf("Unable to get current branch: %v", err.Error())
			return ""
		}
		cmd.Start()
		scanner := bufio.NewScanner(outp)
		found := false
		for scanner.Scan() {
			line := scanner.Text()

			if line[0] == '*' {
				cachedCurrentBranch = line[2:]
				found = true
				break
			}
		}
		cmd.Wait()

		// There's a special case in a newly initialised repository where 'git branch' returns nothing at all
		// In this case the branch really is 'master'
		if !found {
			cachedCurrentBranch = "master"
		}
	}

	return cachedCurrentBranch

}

// Parse a single git refspec string into a GitRefSpec structure ie identify ranges if present
// Does not perform any validation since refs can be symbolic anyway, up to the caller
// to check whether the returned refspec actually works
func ParseGitRefSpec(s string) *GitRefSpec {

	if idx := strings.Index(s, "..."); idx != -1 {
		// reachable from ref1 OR ref2, not both
		ref1 := strings.TrimSpace(s[:idx])
		ref2 := strings.TrimSpace(s[idx+3:])
		return &GitRefSpec{ref1, "...", ref2}
	} else if idx := strings.Index(s, ".."); idx != -1 {
		// range from ref1 -> ref2
		ref1 := strings.TrimSpace(s[:idx])
		ref2 := strings.TrimSpace(s[idx+2:])
		return &GitRefSpec{ref1, "..", ref2}
	} else {
		ref1 := strings.TrimSpace(s)
		return &GitRefSpec{Ref1: ref1}
	}

}

var IsSHARegex *regexp.Regexp = regexp.MustCompile("^[0-9A-Fa-f]{8,40}$")

// Return whether a single git reference (not refspec, so no ranges) is a full SHA or not
// SHAs can be used directly for things like lob lookup but other refs have too be converted
// This version requires a full length SHA (40 characters)
func GitRefIsFullSHA(ref string) bool {
	return len(ref) == 40 && IsSHARegex.MatchString(ref)
}

// Return whether a single git reference (not refspec, so no ranges) is a SHA or not
// SHAs can be used directly for things like lob lookup but other refs have too be converted
// This version accepts SHAs that are 8-40 characters in length, so accepts short SHAs
func GitRefIsSHA(ref string) bool {
	return IsSHARegex.MatchString(ref)
}

func GitRefToFullSHA(ref string) (string, error) {
	if GitRefIsFullSHA(ref) {
		return ref, nil
	}
	// Otherwise use Git to expand to full 40 character SHA
	cmd := exec.Command("git", "rev-parse", ref)
	outp, err := cmd.Output()
	if err != nil {
		return ref, fmt.Errorf("Can't convert %v to a SHA: %v", ref, err.Error())
	}
	return strings.TrimSpace(string(outp)), nil
}

// Return a list of all local branches
// Also FYI caches the current branch while we're at it so it's zero-cost to call
// GetGitCurrentBranch after this
func GetGitLocalBranches() ([]string, error) {
	cmd := exec.Command("git", "branch")

	outp, err := cmd.StdoutPipe()
	if err != nil {
		LogErrorf("Unable to get list local branches: %v", err.Error())
		return []string{}, err
	}
	cmd.Start()
	scanner := bufio.NewScanner(outp)
	foundcurrent := cachedCurrentBranch != ""
	var ret []string
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 2 {
			branch := line[2:]
			ret = append(ret, branch)
			// While we're at it, cache current branch
			if !foundcurrent && line[0] == '*' {
				cachedCurrentBranch = branch
				foundcurrent = true
			}

		}

	}
	cmd.Wait()

	return ret, nil

}

// Return a list of all remote branches for a given remote
// Note this doesn't retrieve mappings between local and remote branches, just a simple list
func GetGitRemoteBranches(remoteName string) ([]string, error) {
	cmd := exec.Command("git", "branch", "-r")

	outp, err := cmd.StdoutPipe()
	if err != nil {
		LogErrorf("Unable to get list remote branches: %v", err.Error())
		return []string{}, err
	}
	cmd.Start()
	scanner := bufio.NewScanner(outp)
	var ret []string
	prefix := remoteName + "/"
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 2 {
			line := line[2:]
			if strings.HasPrefix(line, prefix) {
				// Make sure we terminate at space, line may include alias
				remotebranch := strings.Fields(line[len(prefix):])[0]
				if remotebranch != "HEAD" {
					ret = append(ret, remotebranch)
				}
			}
		}

	}
	cmd.Wait()

	return ret, nil

}

// Return a list of branches to push by default, based on push.default and local/remote branches
// See push.default docs at https://www.kernel.org/pub/software/scm/git/docs/git-config.html
func GetGitPushDefaultBranches(remoteName string) []string {
	pushdef := GlobalOptions.GitConfig["push.default"]
	if pushdef == "" {
		// Use the git 2.0 'simple' default
		pushdef = "simple"
	}

	if pushdef == "matching" {
		// Multiple branches, but only where remote branch name matches
		localbranches, err := GetGitLocalBranches()
		if err != nil {
			// will be logged, safe return
			return []string{}
		}
		remotebranches, err := GetGitRemoteBranches(remoteName)
		if err != nil {
			// will be logged, safe return
			return []string{}
		}
		// Probably sorted already but to be sure
		sort.Strings(remotebranches)
		var ret []string
		for _, branch := range localbranches {
			present, _ := StringBinarySearch(remotebranches, branch)

			if present {
				ret = append(ret, branch)
			}
		}
		return ret
	} else if pushdef == "current" || pushdef == "upstream" || pushdef == "simple" {
		// Current, upstream, simple (in ascending complexity)
		currentBranch := GetGitCurrentBranch()
		if pushdef == "current" {
			return []string{currentBranch}
		}
		// For upstream & simple we need to know what the upstream branch is
		upstreamRemote, upstreamBranch := GetGitUpstreamBranch(currentBranch)
		// Only proceed if the upstream is on this remote
		if upstreamRemote == remoteName && upstreamBranch != "" {
			if pushdef == "upstream" {
				// For upstream we don't care what the remote branch is called
				return []string{currentBranch}
			} else {
				// "simple"
				// In this case git would only push if remote branch matches as well
				if upstreamBranch == currentBranch {
					return []string{currentBranch}
				}
			}
		}
	}

	// "nothing", something we don't understand (safety), or fallthrough non-matched
	return []string{}

}

// Get the upstream branch for a given local branch, as defined in what 'git pull' would do by default
// returns the remote name and the remote branch separately for ease of use
func GetGitUpstreamBranch(localbranch string) (remoteName, remoteBranch string) {
	// Super-verbose mode gives us tracking branch info
	cmd := exec.Command("git", "branch", "-vv")

	outp, err := cmd.StdoutPipe()
	if err != nil {
		LogErrorf("Unable to get list branches: %v", err.Error())
		return "", ""
	}
	cmd.Start()
	scanner := bufio.NewScanner(outp)

	// Output is like this:
	//   branch1              387def9 [origin/branch1] Another new branch
	// * master               aec3297 [origin/master: behind 1] Master change
	// * feature1             e88c156 [origin/feature1: ahead 4, behind 6] Something something dark side
	//   nottrackingbranch    f33e451 Some message

	// Extract branch name and tracking branch (won't match branches with no tracking)
	// Stops at ']' or ':' in tracking branch to deal with ahead/behind markers
	trackRegex := regexp.MustCompile(`^[* ] (\S+)\s+[a-fA-F0-9]+\s+\[([^/]+)/([^\:]+)[\]:]`)

	for scanner.Scan() {
		line := scanner.Text()
		if match := trackRegex.FindStringSubmatch(line); match != nil {
			lbranch := match[1]
			if lbranch == localbranch {
				return match[2], match[3]
			}
		}

	}
	cmd.Wait()

	// no tracking for this branch
	return "", ""

}

// Returns list of commits which have LOB SHAs referenced in them, in a given commit range
// Commits will be in ASCENDING order (parents before children) unlike WalkGitHistory
// Either of from, to or both can be blank to have an unbounded range of commits based on current HEAD
// It is required that if both are supplied, 'from' is an ancestor of 'to'
// Range is exclusive of 'from' and inclusive of 'to'
func GetGitCommitsReferencingLOBsInRange(from, to string) ([]CommitLOBRef, error) {
	// We want '+' lines
	return getGitCommitsReferencingLOBsInRange(from, to, true, false)
}

// Returns list of commits which have LOB SHAs referenced in them, in a given commit range
// Range is exclusive of 'from' and inclusive of 'to'
// additions/removals controls whether we report only diffs with '+' lines of git-lob, '-' lines, or both
func getGitCommitsReferencingLOBsInRange(from, to string, additions, removals bool) ([]CommitLOBRef, error) {

	args := []string{"log", `--format=commitsha: %H`, "-p",
		"--topo-order", "--first-parent",
		"--reverse", // we want to list them in ascending order
		"-G", "^git-lob: [A-Fa-f0-9]{40}$"}

	if from != "" && to != "" {
		args = append(args, fmt.Sprintf("%v..%v", from, to))
	} else {
		if to != "" {
			args = append(args, to)
		} else if from != "" {
			args = append(args, fmt.Sprintf("%v..HEAD", from))
		}
		// if from & to are both blank, just use default behaviour of git log
	}

	cmd := exec.Command("git", args...)
	outp, err := cmd.StdoutPipe()
	if err != nil {
		LogErrorf("Unable to call git-log: %v", err.Error())
		return []CommitLOBRef{}, err
	}
	cmd.Start()

	ret := scanGitLogOutputForLOBReferences(outp, additions, removals)

	return ret, nil

}

// Internal utility for scanning git-log output for git-lob references
// Log output must be formated like this: `--format=commitsha: %H`
// outp must be output from a running git log task
func scanGitLogOutputForLOBReferences(outp io.Reader, additions, removals bool) []CommitLOBRef {
	// Sadly we still get more output than we actually need, but this is the minimum we can get
	// For each commit we'll get something like this:
	/*
	   COMMITSHA:af2607421c9fee2e430cde7e7073a7dad07be559

	   diff --git a/atheneNormalMap.png b/atheneNormalMap.png
	   new file mode 100644
	   index 0000000..272b5c1
	   --- /dev/null
	   +++ b/atheneNormalMap.png
	   @@ -0,0 +1 @@
	   +git-lob: b022770eab414c36575290c993c29799bc6610c3
	*/
	// There can be multiple diffs per commit (multiple binaries)
	// Also when a binary is changed the diff will include a '-' line for the old SHA
	// Depending on which direction in history the caller wants, they'll specify the
	// parameters 'additions' and 'removals' to determine which get included

	// Use 1 regex to capture all for speed
	var regex *regexp.Regexp
	if additions && !removals {
		regex = regexp.MustCompile(`^(commitsha|\+git-lob): ([A-Fa-f0-9]{40})`)
	} else if removals && !additions {
		regex = regexp.MustCompile(`^(commitsha|\-git-lob): ([A-Fa-f0-9]{40})`)
	} else {
		regex = regexp.MustCompile(`^(commitsha|[\+\-]git-lob): ([A-Fa-f0-9]{40})`)
	}

	scanner := bufio.NewScanner(outp)

	var currentCommit *CommitLOBRef
	var ret []CommitLOBRef
	for scanner.Scan() {
		line := scanner.Text()
		if match := regex.FindStringSubmatch(line); match != nil {
			sha := match[2]
			if match[1] == "commitsha" {
				if currentCommit != nil {
					ret = append(ret, *currentCommit)
					currentCommit = nil
				}
				currentCommit = &CommitLOBRef{commit: sha}
			} else { // git-lob is the only other possibility from regex, skip pointless test
				currentCommit.lobSHAs = append(currentCommit.lobSHAs, sha)
			}
		}
	}
	// Final commit
	if currentCommit != nil {
		ret = append(ret, *currentCommit)
		currentCommit = nil
	}

	return ret
}

// Gets a list of LOB SHAs for all binary files that are needed when checking out any of
// the commits referred to by refspec.
// As opposed to GetGitCommitsReferencingLOBsInRange which only picks up changes to LOBs,
// this function returns the complete set of LOBs needed if you checked out a commit either at
// a single commit, or any in a range (if the refspec is a range; only .. range operator allowed)
// This means it will include any LOBs that were added in commits before the range, if they are still used,
// while GetGitCommitsReferencingLOBsInRange wouldn't mention those.
// Note that git ranges are start AND end inclusive in this case.
// Note that duplicate SHAs are not eliminated for efficiency, you must do it if you need it
func GetGitAllLOBsToCheckoutInRefSpec(refspec *GitRefSpec) ([]string, error) {

	var snapshotref string
	if refspec.IsRange() {
		if refspec.RangeOp != ".." {
			return []string{}, errors.New("Only '..' range operator allowed in GetGitAllLOBsToCheckoutInRefSpec")
		}
		// snapshot at end of range, then look at diffs later
		snapshotref = refspec.Ref2
	} else {
		snapshotref = refspec.Ref1
	}

	ret, err := GetGitAllGitLOBsToCheckoutAtCommit(snapshotref)
	if err != nil {
		return ret, err
	}

	if refspec.IsRange() {
		// Now we have all LOBs at the snapshot, find any extra ones earlier in the range
		// to do this, we look for diffs in the commit range that start with "-git-lob:"
		// because a removal means it was referenced before that commit therefore we need it
		// to go back to that state
		// git log is range start exclusive, but that's actually OK since a -git-lob diff line
		// represents the state one commit earlier, giving us an inclusive start range
		commits, err := getGitCommitsReferencingLOBsInRange(refspec.Ref1, refspec.Ref2, false, true)
		if err != nil {
			return ret, err
		}
		for _, commit := range commits {
			// possible to end up with duplicates here if same SHA referenced more than once
			// caller to resolve if they need uniques
			ret = append(ret, commit.lobSHAs...)
		}

	}

	return ret, nil

}

// Get all the LOB SHAs that you would need to have available to check out a commit, and any other
// ancestor of it within a number of days of that commit date (not today's date)
// Note that if a LOB was modified to the same SHA more than once, duplicates may appear in the return
// They are not routinely eliminated for performance, so perform your own dupe removal if you need it
func GetGitAllLOBsToCheckoutAtCommitAndRecent(commit string, days int) ([]string, error) {
	// All LOBs at the commit itself
	shasAtCommit, err := GetGitAllGitLOBsToCheckoutAtCommit(commit)
	if err != nil {
		return []string{}, err
	}
	// get the commit date
	commitDetails, err := GetGitCommitSummary(commit)
	if err != nil {
		return []string{}, err
	}
	sinceDate := commitDetails.CommitDate.AddDate(0, 0, -days)

	// Now use git log to scan backwards
	// We use git log from commit backwards, not commit^ (parent) because
	// we're looking for *previous* SHAs, which means we're looking for diffs
	// with a '-' line. So SHAs replaced in the latest commit are old versions too
	// that we haven't included yet in shasAtCommit
	args := []string{"log", `--format=commitsha: %H`, "-p",
		fmt.Sprintf("--since=%v", sinceDate),
		"-G", "^git-lob: [A-Fa-f0-9]{40}$",
		commit}

	cmd := exec.Command("git", args...)
	outp, err := cmd.StdoutPipe()
	if err != nil {
		LogErrorf("Unable to call git-log: %v", err.Error())
		return []string{}, err
	}
	cmd.Start()

	// Looking backwards, so removals
	commitsWithLOBs := scanGitLogOutputForLOBReferences(outp, false, true)
	ret := shasAtCommit
	for _, lobcommit := range commitsWithLOBs {
		ret = append(ret, lobcommit.lobSHAs...)
	}

	return ret, nil

}

// Get all the LOB SHAs that you would need to check out at a given commit (not changed in that commit)
func GetGitAllGitLOBsToCheckoutAtCommit(commit string) ([]string, error) {
	// Snapshot using ls-tree
	args := []string{"ls-tree",
		"-r",          // recurse
		"-l",          // report object size (we'll need this)
		"--full-tree", // start at the root regardless of where we are in it
		commit}

	lstreecmd := exec.Command("git", args...)
	outp, err := lstreecmd.StdoutPipe()
	if err != nil {
		return []string{}, errors.New(fmt.Sprintf("Unable to call git ls-tree: %v", err.Error()))
	}
	defer outp.Close()
	lstreecmd.Start()
	lstreescanner := bufio.NewScanner(outp)

	// We will look for objects that are *exactly* the size of the git-lob line
	regex := regexp.MustCompile(fmt.Sprintf(`^\d+\s+blob\s+([0-9a-zA-Z]{40})\s+%d\s+(.*)$`, SHALineLen))
	// This will give us object SHAs of content which is exactly the right size, we must
	// then use cat-file (in batch mode) to get the content & parse out anything that's really
	// a git-lob reference.
	// Start git cat-file in parallel and feed its stdin
	catfilecmd := exec.Command("git", "cat-file", "--batch")
	catout, err := catfilecmd.StdoutPipe()
	if err != nil {
		return []string{}, errors.New(fmt.Sprintf("Unable to call git cat-file: %v", err.Error()))
	}
	defer catout.Close()
	catin, err := catfilecmd.StdinPipe()
	if err != nil {
		return []string{}, errors.New(fmt.Sprintf("Unable to call git cat-file: %v", err.Error()))
	}
	defer catin.Close()
	catfilecmd.Start()
	catscanner := bufio.NewScanner(catout)

	var ret []string
	for lstreescanner.Scan() {
		line := lstreescanner.Text()
		if match := regex.FindStringSubmatch(line); match != nil {
			objsha := match[1]

			// Now feed object sha to cat-file to get git-lob SHA if any
			// remember we're already only finding files of exactly the right size (49 bytes)
			_, err := catin.Write([]byte(objsha))
			if err != nil {
				return []string{}, errors.New(fmt.Sprintf("Unable to write to cat-file stream: %v", err.Error()))
			}
			_, err = catin.Write([]byte{'\n'})
			if err != nil {
				return []string{}, errors.New(fmt.Sprintf("Unable to write to cat-file stream: %v", err.Error()))
			}

			// Now read back response - first line is report of object sha, type & size
			// second line is content in our case
			if !catscanner.Scan() || !catscanner.Scan() {
				return []string{}, errors.New(fmt.Sprintf("Couldn't read response from cat-file stream: %v", catscanner.Err()))
			}

			// object SHA is the last 40 characters, after the prefix
			line := catscanner.Text()
			if len(line) == SHALineLen {
				lobsha := line[len(SHAPrefix):]
				ret = append(ret, lobsha)
			}

		}
	}

	return ret, nil

}

// Get summary information about a commit
func GetGitCommitSummary(commit string) (*GitCommitSummary, error) {
	cmd := exec.Command("git", "show", "-s",
		"--format=%%H\u241E%%h\u241E%%aD\u241E%%cD\u241E%%ae\u241E%%an\u241E%%ce\u241E%%cn\u241E%%s", commit)

	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Sprintf("Error calling git show: %v", err.Error())
		LogError(msg)
		return nil, errors.New(msg)
	}

	fields := strings.Split(string(out), "\u241E")
	// Cope with the case where subject is blank
	if len(fields) >= 8 {
		ret := &GitCommitSummary{}
		// Get SHAs from output, not commit input, so we can support symbolic refs
		ret.SHA = fields[0]
		ret.ShortSHA = fields[1]
		// %aD & %cD (RFC2822) matches Go's RFC1123Z format
		ret.AuthorDate, _ = time.Parse(time.RFC1123Z, fields[2])
		ret.CommitDate, _ = time.Parse(time.RFC1123Z, fields[3])
		ret.AuthorEmail = fields[4]
		ret.AuthorName = fields[5]
		ret.CommitterEmail = fields[6]
		ret.CommitterName = fields[7]
		if len(fields) > 8 {
			ret.Subject = fields[8]
		}
		return ret, nil
	} else {
		msg := fmt.Sprintf("Unexpected output from git show: %v", out)
		LogError(msg)
		return nil, errors.New(msg)
	}

}

// Get a list of refs (branches, tags) that have received commits in the last numdays
func GetGitRecentRefs(numdays int) ([]string, error) {
	// TODO
	return nil, nil
}
