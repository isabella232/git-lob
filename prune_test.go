package main

import (
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
)

var _ = Describe("Prune", func() {
	Describe("Prune all unreferenced", func() {

		root := filepath.Join(os.TempDir(), "PruneTest")
		var initialCommit string
		var oldwd string
		BeforeEach(func() {
			// Set up git repo with some subfolders
			CreateGitRepoForTest(root)
			oldwd, _ = os.Getwd()
			os.Chdir(root)

			// Create a single commit (not referencing any SHA)
			initialCommit = CreateInitialCommitForTest(root)
		})
		AfterEach(func() {
			os.Chdir(oldwd)
			os.RemoveAll(root)
		})

		Context("No files", func() {
			It("does nothing when no files present", func() {
				shasToDelete, err := PruneUnreferenced(false)
				Expect(err).To(BeNil(), "PruneUnreferenced should succeed")
				Expect(shasToDelete).To(BeEmpty(), "Should report no files to prune")
			})
		})

		Context("Local storage only", func() {
			var lobshaset StringSet
			var lobshas []string
			var lobfiles []string
			BeforeEach(func() {
				// Manually create a bunch of files, no need to really store things
				// since we just want to see what gets deleted
				lobfiles = make([]string, 0, 20)
				// Create a bunch of files, 20 SHAs
				lobshas = GetListOfRandomSHAsForTest(20)
				lobshaset = NewStringSetFromSlice(lobshas)
				for _, s := range lobshas {
					metafile := getLocalLOBMetaFilename(s)
					ioutil.WriteFile(metafile, []byte("meta something"), 0644)
					lobfiles = append(lobfiles, metafile)
					numChunks := rand.Intn(3) + 1
					for c := 0; c < numChunks; c++ {
						chunkfile := getLocalLOBChunkFilename(s, c)
						lobfiles = append(lobfiles, chunkfile)
						ioutil.WriteFile(chunkfile, []byte("data something"), 0644)
					}
				}

			})
			AfterEach(func() {
				for _, l := range lobfiles {
					os.Remove(l)
				}
			})
			Context("prunes all files when no references", func() {
				// Because we've created no commits, all LOBs should be eligible for deletion
				It("lists files but doesn't act on it in dry run mode", func() {
					shasToDelete, err := PruneUnreferenced(true)
					Expect(err).To(BeNil(), "PruneUnreferenced should succeed")
					// Use sets to compare so ordering doesn't matter
					actualset := NewStringSetFromSlice(shasToDelete)
					Expect(actualset).To(Equal(lobshaset), "Should want to delete all files")

					for _, file := range lobfiles {
						exists, _ := FileOrDirExists(file)
						Expect(exists).To(Equal(true), "File %v should still exist", file)
					}

				})
				It("deletes files when not in dry run mode", func() {
					shasToDelete, err := PruneUnreferenced(false)
					Expect(err).To(BeNil(), "PruneUnreferenced should succeed")
					// Use sets to compare so ordering doesn't matter
					actualset := NewStringSetFromSlice(shasToDelete)
					Expect(actualset).To(Equal(lobshaset), "Should want to delete all files")

					for _, file := range lobfiles {
						exists, _ := FileOrDirExists(file)
						Expect(exists).To(Equal(false), "File %v should have been deleted", file)
					}

				})
			})
			Context("some files referenced", func() {
				It("correctly identifies referenced and unreferenced files", func() {
					// Manually create commits of files which reference the first few SHAs but not
					// the latter few. Also spread these references across several different branches,
					// and put at least one of them in a file modification

					// First 3 SHAs, create in one branch with different files
					CreateCommitReferencingLOBsForTest(root, map[string]string{
						lobshas[0]: "test1.png",
						lobshas[1]: "test2.png",
						lobshas[2]: "test3.png"})

					// 4th SHA is a modification
					CreateCommitReferencingLOBsForTest(root, map[string]string{lobshas[3]: "test1.png"})

					// next 3, create in a different branch, 2 modifications & 1 new
					exec.Command("git", "branch", "branch2").Run()
					CreateCommitReferencingLOBsForTest(root, map[string]string{
						lobshas[4]: "test2.png",
						lobshas[5]: "test3.png"})
					CreateCommitReferencingLOBsForTest(root, map[string]string{lobshas[6]: "test4.png"})

					// Back to main branch
					exec.Command("git", "checkout", "master").Run()
					CreateCommitReferencingLOBsForTest(root, map[string]string{lobshas[7]: "test10.png"})

					// next 3, create in a different branch, 2 modifications & 1 new
					exec.Command("git", "branch", "branch3").Run()
					CreateCommitReferencingLOBsForTest(root, map[string]string{
						lobshas[8]: "test1.png",
						lobshas[9]: "test12.png"})
					CreateCommitReferencingLOBsForTest(root, map[string]string{
						lobshas[10]: "test2.png",
						lobshas[11]: "test12.png"})

					exec.Command("git", "checkout", "master").Run()
					// Last one, reference 12 & 13 in index, not committed.
					ioutil.WriteFile(filepath.Join(root, "test3.png"), []byte(fmt.Sprintf("git-lob: %v", lobshas[12])), 0644)
					ioutil.WriteFile(filepath.Join(root, "test20.png"), []byte(fmt.Sprintf("git-lob: %v", lobshas[13])), 0644)
					exec.Command("git", "add", "test3.png", "test20.png").Run()

					// At this point we should be deleting SHAs 14-19 but not the others
					//shasShouldKeep := NewStringSetFromSlice(lobshas[0:14])
					shasShouldDelete := NewStringSetFromSlice(lobshas[14:])

					deletedSlice, err := PruneUnreferenced(false)
					Expect(err).To(BeNil(), "PruneUnreferenced should succeed")
					shasDidDelete := NewStringSetFromSlice(deletedSlice)

					Expect(shasDidDelete).To(Equal(shasShouldDelete), "Should delete the correct LOBs")
					// Make sure files don't exist
					for sha := range shasShouldDelete.Iter() {
						matches, err := filepath.Glob(fmt.Sprintf("%v*", filepath.Join(GetLocalLOBDir(sha), sha)))
						Expect(err).To(BeNil(), "Should not be error in glob checking")
						Expect(matches).To(BeEmpty(), "All local SHAs should be deleted")
					}

				})

			})

		})
		Context("Shared storage - deleting at same time as local", func() {
			var lobshaset StringSet
			var lobshas []string
			var lobfiles []string
			sharedStore := filepath.Join(os.TempDir(), "PruneTest_SharedStore")

			BeforeEach(func() {
				os.MkdirAll(sharedStore, 0755)
				GlobalOptions.SharedStore = sharedStore
				// Manually create a bunch of files, no need to really store things
				// since we just want to see what gets deleted
				lobfiles = make([]string, 0, 20)
				// Create a bunch of files, 20 SHAs
				lobshas = GetListOfRandomSHAsForTest(20)
				lobshaset = NewStringSetFromSlice(lobshas)
				for _, s := range lobshas {
					metafile := getSharedLOBMetaFilename(s)
					ioutil.WriteFile(metafile, []byte("meta something"), 0644)
					lobfiles = append(lobfiles, metafile)
					// link shared locally
					metalinkfile := getLocalLOBMetaFilename(s)
					CreateHardLink(metafile, metalinkfile)
					lobfiles = append(lobfiles, metalinkfile)
					numChunks := rand.Intn(3) + 1
					for c := 0; c < numChunks; c++ {
						chunkfile := getSharedLOBChunkFilename(s, c)
						lobfiles = append(lobfiles, chunkfile)
						ioutil.WriteFile(chunkfile, []byte("data something"), 0644)
						// link shared locally
						linkfile := getLocalLOBChunkFilename(s, c)
						CreateHardLink(chunkfile, linkfile)
						lobfiles = append(lobfiles, linkfile)
					}
				}

			})
			AfterEach(func() {
				for _, l := range lobfiles {
					os.Remove(l)
				}
				os.RemoveAll(sharedStore)
				GlobalOptions.SharedStore = ""
			})
			Context("prunes all files when no references", func() {
				// Because we've created no commits, all LOBs should be eligible for deletion
				It("lists files but doesn't act on it in dry run mode", func() {
					shasToDelete, err := PruneUnreferenced(true)
					Expect(err).To(BeNil(), "PruneUnreferenced should succeed")
					// Use sets to compare so ordering doesn't matter
					actualset := NewStringSetFromSlice(shasToDelete)
					Expect(actualset).To(Equal(lobshaset), "Should want to delete all files")

					// This includes both local links and shared files
					for _, file := range lobfiles {
						exists, _ := FileOrDirExists(file)
						Expect(exists).To(Equal(true), "File %v should still exist", file)
					}

				})
				It("deletes files when not in dry run mode", func() {
					shasToDelete, err := PruneUnreferenced(false)
					Expect(err).To(BeNil(), "PruneUnreferenced should succeed")
					// Use sets to compare so ordering doesn't matter
					actualset := NewStringSetFromSlice(shasToDelete)
					Expect(actualset).To(Equal(lobshaset), "Should want to delete all files")

					// This includes both local links and shared files
					for _, file := range lobfiles {
						exists, _ := FileOrDirExists(file)
						Expect(exists).To(Equal(false), "File %v should have been deleted", file)
					}

				})
			})
			Context("some files referenced", func() {
				It("correctly identifies referenced and unreferenced files", func() {
					// Manually create commits of files which reference the first few SHAs but not
					// the latter few. Also spread these references across several different branches,
					// and put at least one of them in a file modification

					// First 3 SHAs, create in one branch with different files
					CreateCommitReferencingLOBsForTest(root, map[string]string{
						lobshas[0]: "test1.png",
						lobshas[1]: "test2.png",
						lobshas[2]: "test3.png"})

					// 4th SHA is a modification
					CreateCommitReferencingLOBsForTest(root, map[string]string{lobshas[3]: "test1.png"})

					// next 3, create in a different branch, 2 modifications & 1 new
					exec.Command("git", "branch", "branch2").Run()
					CreateCommitReferencingLOBsForTest(root, map[string]string{
						lobshas[4]: "test2.png",
						lobshas[5]: "test3.png"})
					CreateCommitReferencingLOBsForTest(root, map[string]string{lobshas[6]: "test4.png"})

					// Back to main branch
					exec.Command("git", "checkout", "master").Run()
					CreateCommitReferencingLOBsForTest(root, map[string]string{lobshas[7]: "test10.png"})

					// next 3, create in a different branch, 2 modifications & 1 new
					exec.Command("git", "branch", "branch3").Run()
					CreateCommitReferencingLOBsForTest(root, map[string]string{
						lobshas[8]: "test1.png",
						lobshas[9]: "test12.png"})
					CreateCommitReferencingLOBsForTest(root, map[string]string{
						lobshas[10]: "test2.png",
						lobshas[11]: "test12.png"})

					exec.Command("git", "checkout", "master").Run()
					// Last one, reference 12 & 13 in index, not committed.
					ioutil.WriteFile(filepath.Join(root, "test3.png"), []byte(fmt.Sprintf("git-lob: %v", lobshas[12])), 0644)
					ioutil.WriteFile(filepath.Join(root, "test20.png"), []byte(fmt.Sprintf("git-lob: %v", lobshas[13])), 0644)
					exec.Command("git", "add", "test3.png", "test20.png").Run()

					// At this point we should be deleting SHAs 14-19 but not the others
					//shasShouldKeep := NewStringSetFromSlice(lobshas[0:14])
					shasShouldDelete := NewStringSetFromSlice(lobshas[14:])

					deletedSlice, err := PruneUnreferenced(false)
					Expect(err).To(BeNil(), "PruneUnreferenced should succeed")
					shasDidDelete := NewStringSetFromSlice(deletedSlice)

					Expect(shasDidDelete).To(Equal(shasShouldDelete), "Should delete the correct LOBs")

					// Make sure files don't exist
					for sha := range shasShouldDelete.Iter() {
						matches, err := filepath.Glob(fmt.Sprintf("%v*", filepath.Join(GetLocalLOBDir(sha), sha)))
						Expect(err).To(BeNil(), "Should not be error in glob checking")
						Expect(matches).To(BeEmpty(), "All local SHAs should be deleted")

						matches, err = filepath.Glob(fmt.Sprintf("%v*", filepath.Join(GetSharedLOBDir(sha), sha)))
						Expect(err).To(BeNil(), "Should not be error in glob checking")
						Expect(matches).To(BeEmpty(), "All shared SHAs should be deleted")
					}
				})

			})
		})

		Context("Shared storage - cleaning up", func() {
			var lobshaset StringSet
			var lobshas []string
			var sharedlobfiles []string
			sharedStore := filepath.Join(os.TempDir(), "PruneTest_SharedStore")

			BeforeEach(func() {
				os.MkdirAll(sharedStore, 0755)
				GlobalOptions.SharedStore = sharedStore
				// Manually create a bunch of files, no need to really store things
				// since we just want to see what gets deleted
				sharedlobfiles = make([]string, 0, 20)
				// Create a bunch of files, 20 SHAs, in shared area
				lobshas = GetListOfRandomSHAsForTest(20)
				lobshaset = NewStringSetFromSlice(lobshas)
				for _, s := range lobshas {
					metafile := getSharedLOBMetaFilename(s)
					ioutil.WriteFile(metafile, []byte("meta something"), 0644)
					sharedlobfiles = append(sharedlobfiles, metafile)
					numChunks := rand.Intn(3) + 1
					for c := 0; c < numChunks; c++ {
						chunkfile := getSharedLOBChunkFilename(s, c)
						sharedlobfiles = append(sharedlobfiles, chunkfile)
						ioutil.WriteFile(chunkfile, []byte("data something"), 0644)
					}
				}

			})
			AfterEach(func() {
				os.RemoveAll(sharedStore)
				GlobalOptions.SharedStore = ""
			})
			Context("prunes all files when no references", func() {
				// Because we've created no hard links to the shared store, everything should be available for deletion
				It("lists files but doesn't act on it in dry run mode", func() {
					shasToDelete, err := PruneSharedStore(true)
					Expect(err).To(BeNil(), "PruneSharedStore should succeed")
					// Use sets to compare so ordering doesn't matter
					actualset := NewStringSetFromSlice(shasToDelete)
					Expect(actualset).To(Equal(lobshaset), "Should want to delete all files")

					// This includes both local links and shared files
					for _, file := range sharedlobfiles {
						exists, _ := FileOrDirExists(file)
						Expect(exists).To(Equal(true), "File %v should still exist", file)
					}

				})
				It("deletes files when not in dry run mode", func() {
					shasToDelete, err := PruneSharedStore(false)
					Expect(err).To(BeNil(), "PruneSharedStore should succeed")
					// Use sets to compare so ordering doesn't matter
					actualset := NewStringSetFromSlice(shasToDelete)
					Expect(actualset).To(Equal(lobshaset), "Should want to delete all files")

					// This includes both local links and shared files
					for _, file := range sharedlobfiles {
						exists, _ := FileOrDirExists(file)
						Expect(exists).To(Equal(false), "File %v should have been deleted", file)
					}

				})
			})
			Context("some files referenced", func() {
				var locallobfiles []string
				const referenceUpTo = 10
				BeforeEach(func() {
					locallobfiles = make([]string, 0, 10)
					for i, sharedfile := range sharedlobfiles {
						// Just link into temp dir, doesn't matter where the link is
						localfile := filepath.Join(os.TempDir(), filepath.Base(sharedfile))
						CreateHardLink(sharedfile, localfile)
						locallobfiles = append(locallobfiles, localfile)

						// Only reference some
						if i >= referenceUpTo {
							break
						}

					}
				})
				AfterEach(func() {
					for _, l := range locallobfiles {
						os.Remove(l)
					}
				})

				It("does nothing in dry run mode", func() {
					PruneSharedStore(true)
					for _, sharedfile := range sharedlobfiles {
						exists, _ := FileOrDirExists(sharedfile)
						Expect(exists).To(BeTrue(), "Should not have deleted %v", sharedfile)
					}
				})
				It("correctly identifies referenced and unreferenced shared files", func() {
					PruneSharedStore(false)
					for i, sharedfile := range sharedlobfiles {
						exists, _ := FileOrDirExists(sharedfile)
						if i <= referenceUpTo {
							Expect(exists).To(BeTrue(), "Should not have deleted %v", sharedfile)
						} else {
							Expect(exists).To(BeFalse(), "Should have deleted %v", sharedfile)
						}
					}
				})

			})
		})
	})

})