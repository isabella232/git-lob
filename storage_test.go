package main

import (
	"crypto/sha1"
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
)

var _ = Describe("Storage", func() {

	root := path.Join(os.TempDir(), "StorageTest")
	separateGitDir := path.Join(os.TempDir(), "StorageTestGitDir")
	sharedStore := path.Join(os.TempDir(), "StorageTest_SharedStore")
	folders := []string{
		filepath.Join(root, "folder1"),
		filepath.Join(root, "folder2"),
		filepath.Join(root, "folder3"),
		filepath.Join(root, "folder1", "sub1"),
		filepath.Join(root, "folder1", "sub2"),
		filepath.Join(root, "folder1", "sub1", "subsub1"),
		filepath.Join(root, "folder1", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l")}

	Describe("Identifying git repo root", func() {
		Context("Valid git repo", func() {
			var oldwd string
			BeforeEach(func() {
				oldwd, _ = os.Getwd()
				// Set up git repo with some subfolders
				CreateGitRepoForTest(root)

				for _, f := range folders {
					err := os.MkdirAll(f, 0755)
					if err != nil {
						fmt.Printf("Can't MkdirAll %v: %v", f, err)
					}
				}

			})

			AfterEach(func() {
				// Delete repo
				os.RemoveAll(root)
				os.Chdir(oldwd)
			})

			It("finds root git folder", func() {

				// Need to expand root for symlinks here in order to guarantee string comparison works
				// /var turns into /private/var on OS X for example
				// Can't use this for creating repos etc though, OS X doesn't like direct access
				expandedroot, _ := filepath.EvalSymlinks(root)
				for _, f := range folders {
					err := os.Chdir(f)
					if err != nil {
						Fail(fmt.Sprintf("Can't chdir to %v: %v", f, err))
					}
					testroot, sep, err := GetRepoRoot()
					Expect(err).To(BeNil(), "Should be no error getting repo root")
					Expect(testroot).To(Equal(expandedroot))
					Expect(sep).To(Equal(false))
				}
			})
		})
		Context("Invalid git repo", func() {
			var oldwd string
			BeforeEach(func() {
				oldwd, _ = os.Getwd()
			})
			AfterEach(func() {
				os.Chdir(oldwd)
			})
			It("Fails safely outside a git repo", func() {
				// Relies on temp dir not being a git repo, which should be valid assumption
				os.Chdir(os.TempDir())
				testroot, _, err := GetRepoRoot()
				Expect(testroot).To(Equal(""))
				Expect(err).ToNot(BeNil(), "Should be error outside git repo")
			})

		})

	})

	Describe("Finding git dir", func() {
		Context("Git repo with standard git dir", func() {
			var oldwd string
			BeforeEach(func() {
				oldwd, _ = os.Getwd()
				// Set up git repo with some subfolders
				CreateGitRepoForTest(root)

				for _, f := range folders {
					err := os.MkdirAll(f, 0755)
					if err != nil {
						fmt.Printf("Can't MkdirAll %v: %v", f, err)
					}
				}

			})

			AfterEach(func() {
				os.Chdir(oldwd)
				// Delete repo
				os.RemoveAll(root)
			})

			It("finds git dir", func() {

				// Need to expand root for symlinks here in order to guarantee string comparison works
				// /var turns into /private/var on OS X for example
				// Can't use this for creating repos etc though, OS X doesn't like direct access
				gitdir, _ := filepath.EvalSymlinks(path.Join(root, ".git"))

				for _, f := range folders {
					err := os.Chdir(f)
					if err != nil {
						Fail(fmt.Sprintf("Can't chdir to %v: %v", f, err))
					}
					testgitdir := GetGitDir()
					Expect(testgitdir).To(Equal(gitdir))
				}
			})

		})

		Context("Git repo with separate git dir", func() {
			var oldwd string
			BeforeEach(func() {
				oldwd, _ = os.Getwd()
				// Set up git repo with some subfolders
				CreateGitRepoWithSeparateGitDirForTest(root, separateGitDir)

				for _, f := range folders {
					err := os.MkdirAll(f, 0755)
					if err != nil {
						fmt.Printf("Can't MkdirAll %v: %v", f, err)
					}
				}

			})

			AfterEach(func() {
				os.Chdir(oldwd)
				// Delete repo
				os.RemoveAll(root)
			})

			It("finds git dir", func() {

				// Need to expand root for symlinks here in order to guarantee string comparison works
				// /var turns into /private/var on OS X for example
				// Can't use this for creating repos etc though, OS X doesn't like direct access
				gitdir, _ := filepath.EvalSymlinks(separateGitDir)

				for _, f := range folders {
					err := os.Chdir(f)
					if err != nil {
						Fail(fmt.Sprintf("Can't chdir to %v: %v", f, err))
					}
					testgitdir := GetGitDir()
					Expect(testgitdir).To(Equal(gitdir))
				}
			})

		})
	})

	Describe("Storing a LOB", func() {
		// Common git repo
		var oldwd string
		BeforeEach(func() {
			oldwd, _ = os.Getwd()
			// Set up git repo with some subfolders
			CreateGitRepoForTest(root)

			for _, f := range folders {
				err := os.MkdirAll(f, 0755)
				if err != nil {
					fmt.Printf("Can't MkdirAll %v: %v", f, err)
				}
			}
			os.Chdir(root)
		})

		AfterEach(func() {
			os.Chdir(oldwd)
			// Delete repo
			os.RemoveAll(root)
		})

		Context("Store small single chunk LOB", func() {
			testFileName := path.Join(folders[2], "small.dat")
			var correctLOBInfo *LOBInfo

			BeforeEach(func() {
				correctLOBInfo = CreateSmallTestLOBFileForStoring(testFileName)
			})
			AfterEach(func() {
				os.Remove(testFileName)
			})

			It("correctly stores a small file", func() {
				f, err := os.Open(testFileName)
				if err != nil {
					Fail(fmt.Sprintf("Can't reopen test file %v: %v", testFileName, err))
				}
				defer f.Close()
				// Need to read leader for consistency with real usage
				leader := make([]byte, SHALineLen)
				c, err := f.Read(leader)
				if err != nil {
					Fail(fmt.Sprintf("Can't read leader of test file %v: %v", testFileName, err))
				}
				lobinfo, err := StoreLOB(f, leader[:c])
				Expect(err).To(BeNil(), "Shouldn't be error storing LOB")
				Expect(lobinfo).To(Equal(correctLOBInfo))
				fileinfo, err := os.Stat(getLocalLOBChunkPath(lobinfo.SHA, 0))
				Expect(err).To(BeNil(), "Shouldn't be error opening stored LOB")
				Expect(fileinfo.Size()).To(Equal(lobinfo.Size), "Stored LOB should be correct size")
			})

		})
		Context("Store zero size file", func() {
			zerofile := path.Join(folders[1], "zero.dat")
			BeforeEach(func() {
				CreateRandomFileForTest(0, zerofile)
			})
			AfterEach(func() {
				os.Remove(zerofile)
			})

			It("correctly stores zero size files", func() {
				f, err := os.Open(zerofile)
				if err != nil {
					Fail(fmt.Sprintf("Can't reopen test file %v: %v", zerofile, err))
				}
				defer f.Close()
				// Need TRY to read leader for consistency with real usage
				leader := make([]byte, SHALineLen)
				c, err := f.Read(leader)
				lobinfo, err := StoreLOB(f, leader[:c])
				Expect(err).To(BeNil(), "Shouldn't be error storing LOB")
				Expect(lobinfo.Size).To(BeEquivalentTo(0), "Size should be correct")
				Expect(lobinfo.NumChunks).To(BeEquivalentTo(0), "Should only be one chunk")
				Expect(lobinfo.ChunkSize).To(BeEquivalentTo(GlobalOptions.ChunkSize), "Chunk size should be correct")
				shaZero := sha1.New()
				shaZeroStr := fmt.Sprintf("%x", string(shaZero.Sum(nil)))
				Expect(lobinfo.SHA).To(Equal(shaZeroStr), "SHA should be the zero file content SHA")

			})
		})

		Context("Store single chunk LOB of exact chunk size", func() {
			exact1 := path.Join(folders[1], "exact1.dat")
			exact2 := path.Join(folders[1], "exact2.dat")
			var oldChunkSize int64

			BeforeEach(func() {
				// Jig the chunk size for efficient testing
				oldChunkSize = GlobalOptions.ChunkSize
				GlobalOptions.ChunkSize = 200
				CreateRandomFileForTest(GlobalOptions.ChunkSize, exact1)
				CreateRandomFileForTest(GlobalOptions.ChunkSize*2, exact2)

			})
			AfterEach(func() {
				os.Remove(exact1)
				os.Remove(exact2)
				GlobalOptions.ChunkSize = oldChunkSize
			})

			It("correctly stores files which are exact multiples of chunk size", func() {
				f, err := os.Open(exact1)
				if err != nil {
					Fail(fmt.Sprintf("Can't reopen test file %v: %v", exact1, err))
				}
				defer f.Close()
				// Need to read leader for consistency with real usage
				leader := make([]byte, SHALineLen)
				c, err := f.Read(leader)
				if err != nil {
					Fail(fmt.Sprintf("Can't read leader of test file %v: %v", exact1, err))
				}
				lobinfo, err := StoreLOB(f, leader[:c])
				Expect(err).To(BeNil(), "Shouldn't be error storing LOB")
				Expect(lobinfo.Size).To(BeEquivalentTo(GlobalOptions.ChunkSize), "Size should be correct")
				Expect(lobinfo.NumChunks).To(BeEquivalentTo(1), "Should only be one chunk")
				Expect(lobinfo.ChunkSize).To(BeEquivalentTo(GlobalOptions.ChunkSize), "Chunk size should be correct")
				fileinfo, err := os.Stat(getLocalLOBChunkPath(lobinfo.SHA, 0))
				Expect(err).To(BeNil(), "Shouldn't be error opening stored LOB")
				Expect(fileinfo.Size()).To(Equal(lobinfo.Size), "Stored LOB should be correct size")

				f2, err := os.Open(exact2)
				if err != nil {
					Fail(fmt.Sprintf("Can't reopen test file %v: %v", exact2, err))
				}
				defer f2.Close()
				// Need to read leader for consistency with real usage
				leader = make([]byte, SHALineLen)
				c, err = f2.Read(leader)
				if err != nil {
					Fail(fmt.Sprintf("Can't read leader of test file %v: %v", exact2, err))
				}
				lobinfo, err = StoreLOB(f2, leader[:c])
				Expect(err).To(BeNil(), "Shouldn't be error storing LOB")
				Expect(lobinfo.Size).To(BeEquivalentTo(GlobalOptions.ChunkSize*2), "Size should be correct")
				Expect(lobinfo.NumChunks).To(BeEquivalentTo(2), "Should be 2 chunks")
				Expect(lobinfo.ChunkSize).To(BeEquivalentTo(GlobalOptions.ChunkSize), "Chunk size should be correct")
				fileinfo, err = os.Stat(getLocalLOBChunkPath(lobinfo.SHA, 0))
				Expect(err).To(BeNil(), "Shouldn't be error opening stored LOB")
				Expect(fileinfo.Size()).To(Equal(lobinfo.ChunkSize), "Stored LOB should be correct size")
				fileinfo, err = os.Stat(getLocalLOBChunkPath(lobinfo.SHA, 1))
				Expect(err).To(BeNil(), "Shouldn't be error opening stored LOB")
				Expect(fileinfo.Size()).To(Equal(lobinfo.ChunkSize), "Stored LOB should be correct size")

			})

		})

		Context("Store large multiple chunk LOB [LONGTEST]", func() {

			testFileName := path.Join(folders[2], "large.dat")
			var correctLOBInfo *LOBInfo

			BeforeEach(func() {
				correctLOBInfo = CreateLargeTestLOBFileForStoring(testFileName)
			})
			AfterEach(func() {
				os.Remove(testFileName)
			})

			It("correctly stores a large file", func() {
				f, err := os.Open(testFileName)
				if err != nil {
					Fail(fmt.Sprintf("Can't reopen test file %v: %v", testFileName, err))
				}
				defer f.Close()
				// Need to read leader for consistency with real usage
				leader := make([]byte, SHALineLen)
				c, err := f.Read(leader)
				if err != nil {
					Fail(fmt.Sprintf("Can't read leader of test file %v: %v", testFileName, err))
				}
				lobinfo, err := StoreLOB(f, leader[:c])
				Expect(err).To(BeNil(), "Shouldn't be error storing LOB")
				Expect(lobinfo).To(Equal(correctLOBInfo))
				for i := 0; i < lobinfo.NumChunks; i++ {
					fileinfo, err := os.Stat(getLocalLOBChunkPath(lobinfo.SHA, i))
					Expect(err).To(BeNil(), "Shouldn't be error opening stored LOB #%v", i)
					if i+1 < lobinfo.NumChunks {
						Expect(fileinfo.Size()).To(BeEquivalentTo(GlobalOptions.ChunkSize), "Stored LOB #%v should be chunk limit size", i)
					} else {
						Expect(fileinfo.Size()).To(BeEquivalentTo(lobinfo.Size%GlobalOptions.ChunkSize), "Stored LOB #%v should be correct size", i)
					}

				}
			})

		})

	})

	Describe("Retrieving a LOB", func() {
		// Common git repo
		var oldwd string
		BeforeEach(func() {
			oldwd, _ = os.Getwd()
			// Set up git repo with some subfolders
			CreateGitRepoForTest(root)
			os.Chdir(root)

			for _, f := range folders {
				err := os.MkdirAll(f, 0755)
				if err != nil {
					fmt.Printf("Can't MkdirAll %v: %v", f, err)
				}
			}

		})

		AfterEach(func() {
			os.Chdir(oldwd)
			// Delete repo
			os.RemoveAll(root)
		})

		Context("Retrieve small single chunk LOB", func() {
			var correctLOBInfo *LOBInfo

			BeforeEach(func() {
				correctLOBInfo = CreateSmallTestLOBDataForRetrieval()
			})

			It("correctly retrieves small LOB file", func() {
				// output to a temp file
				out, err := ioutil.TempFile("", "lobsmall.dat")
				Expect(err).To(BeNil(), "Shouldn't be error creating temp file")
				outFilename := out.Name()
				info, err := RetrieveLOB(correctLOBInfo.SHA, out)

				Expect(err).To(BeNil(), "Shouldn't be error retrieving LOB")
				out.Close()

				Expect(info).To(Equal(correctLOBInfo), "Metadata should agree")
				// Check output file
				stat, err := os.Stat(outFilename)
				Expect(err).To(BeNil(), "Shouldn't be error checking output file")
				Expect(stat.Size()).To(Equal(info.Size), "Size on disk should agree with metadata")

				os.Remove(outFilename)

			})

		})
		Context("Retrieve large multiple chunk LOB [LONGTEST]", func() {
			var correctLOBInfo *LOBInfo

			BeforeEach(func() {
				correctLOBInfo = CreateLargeTestLOBDataForRetrieval()
			})

			It("correctly retrieves large LOB file", func() {
				// output to a temp file
				out, err := ioutil.TempFile("", "loblarge.dat")
				Expect(err).To(BeNil(), "Shouldn't be error creating temp file")
				outFilename := out.Name()
				info, err := RetrieveLOB(correctLOBInfo.SHA, out)

				Expect(err).To(BeNil(), "Shouldn't be error retrieving LOB")
				out.Close()

				Expect(info).To(Equal(correctLOBInfo), "Metadata should agree")
				// Check output file
				stat, err := os.Stat(outFilename)
				Expect(err).To(BeNil(), "Shouldn't be error checking output file")
				Expect(stat.Size()).To(Equal(info.Size), "Size on disk should agree with metadata")

				os.Remove(outFilename)

			})

		})

		Context("Retrieve a zero size file", func() {
			It("correctly retrieves zero size LOB file", func() {
				// Create the zero size storage (separate test for storing)
				infile := path.Join(folders[1], "zeroin.dat")
				CreateRandomFileForTest(0, infile)
				_, err := StoreLOBForTest(infile)
				os.Remove(infile)
				if err != nil {
					Fail(fmt.Sprintf("Error storing zero size file %v", infile))
				}

				// Zero size file SHA
				shaZero := sha1.New()
				shaZeroStr := fmt.Sprintf("%x", string(shaZero.Sum(nil)))

				// output to a temp file
				out, err := ioutil.TempFile("", "lobzerotest.dat")
				Expect(err).To(BeNil(), "Shouldn't be error creating temp file")
				outFilename := out.Name()
				info, err := RetrieveLOB(shaZeroStr, out)

				Expect(err).To(BeNil(), "Shouldn't be error retrieving LOB")
				out.Close()

				Expect(info.SHA).To(Equal(shaZeroStr), "SHA should agree")
				Expect(info.Size).To(BeEquivalentTo(0), "Should be zero size")
				Expect(info.NumChunks).To(BeEquivalentTo(0), "Should be no chunks should agree")
				// Check output file
				stat, err := os.Stat(outFilename)
				Expect(err).To(BeNil(), "Shouldn't be error checking output file")
				Expect(stat.Size()).To(BeEquivalentTo(0), "Size on disk should be zero")

				os.Remove(outFilename)

			})

		})
		Describe("Changing chunk size", func() {
			var oldChunkSize int64

			BeforeEach(func() {
				// Jig the chunk size for efficient testing
				oldChunkSize = GlobalOptions.ChunkSize
				GlobalOptions.ChunkSize = 512

			})
			AfterEach(func() {
				GlobalOptions.ChunkSize = oldChunkSize
			})

			It("correctly stores files which are exact multiples of chunk size", func() {
				filename := path.Join(folders[1], "changesize.dat")
				CreateRandomFileForTest(GlobalOptions.ChunkSize*4+120, filename)
				storeinfo, err := StoreLOBForTest(filename)
				if err != nil {
					Fail(fmt.Sprintf("Failed to store %v: %v", filename, err))
				}

				// Change the chunk size (smaller) & retrieve
				GlobalOptions.ChunkSize = 256
				out, err := ioutil.TempFile("", "changesize.dat")
				Expect(err).To(BeNil(), "Shouldn't be error creating temp file")
				outFilename := out.Name()
				retrieveinfo, err := RetrieveLOB(storeinfo.SHA, out)
				Expect(err).To(BeNil(), "Shouldn't be error retrieving LOB")
				out.Close()

				Expect(retrieveinfo).To(Equal(storeinfo), "Chunk size should not be changed on retrieve")
				// Check output file
				stat, err := os.Stat(outFilename)
				Expect(err).To(BeNil(), "Shouldn't be error checking output file")
				Expect(stat.Size()).To(Equal(retrieveinfo.Size), "Size on disk should agree with metadata")

			})

		})

	})

	// --- Shared tests
	Describe("Storing a LOB (shared store)", func() {
		// Common git repo
		var oldwd string
		BeforeEach(func() {
			oldwd, _ = os.Getwd()
			os.MkdirAll(sharedStore, 0755)
			GlobalOptions.SharedStore = sharedStore
			// Set up git repo with some subfolders
			CreateGitRepoForTest(root)

			for _, f := range folders {
				err := os.MkdirAll(f, 0755)
				if err != nil {
					fmt.Printf("Can't MkdirAll %v: %v", f, err)
				}
			}
			os.Chdir(root)
		})

		AfterEach(func() {
			os.Chdir(oldwd)
			// Delete repo
			os.RemoveAll(root)
			os.RemoveAll(sharedStore)
			GlobalOptions.SharedStore = ""
		})

		Context("Store small single chunk LOB (shared store)", func() {
			testFileName := path.Join(folders[2], "small.dat")
			var correctLOBInfo *LOBInfo

			BeforeEach(func() {
				correctLOBInfo = CreateSmallTestLOBFileForStoring(testFileName)
			})
			AfterEach(func() {
				os.Remove(testFileName)
			})

			It("correctly stores a small file (shared store)", func() {
				f, err := os.Open(testFileName)
				if err != nil {
					Fail(fmt.Sprintf("Can't reopen test file %v: %v", testFileName, err))
				}
				defer f.Close()
				// Need to read leader for consistency with real usage
				leader := make([]byte, SHALineLen)
				c, err := f.Read(leader)
				if err != nil {
					Fail(fmt.Sprintf("Can't read leader of test file %v: %v", testFileName, err))
				}
				lobinfo, err := StoreLOB(f, leader[:c])
				Expect(err).To(BeNil(), "Shouldn't be error storing LOB")
				Expect(lobinfo).To(Equal(correctLOBInfo))

				lobinfo, err = GetLOBInfo(correctLOBInfo.SHA)
				Expect(err).To(BeNil(), "Shouldn't be error retrieving LOB info")
				Expect(lobinfo).To(Equal(correctLOBInfo))

				fileinfo, err := os.Stat(getLocalLOBChunkPath(lobinfo.SHA, 0))
				Expect(err).To(BeNil(), "Shouldn't be error opening stored LOB (local)")
				Expect(fileinfo.Size()).To(Equal(lobinfo.Size), "Stored LOB should be correct size (local)")
				// Also test shared
				fileinfo, err = os.Stat(getSharedLOBChunkPath(lobinfo.SHA, 0))
				Expect(err).To(BeNil(), "Shouldn't be error opening stored LOB (shared)")
				Expect(fileinfo.Size()).To(Equal(lobinfo.Size), "Stored LOB should be correct size (shared)")

				links, err := GetHardLinkCount(getLocalLOBChunkPath(lobinfo.SHA, 0))
				Expect(err).To(BeNil(), "Shouldn't be error getting local LOB hard link info")
				Expect(links).To(Equal(2), "Should be the right number of hard links (shared)")
				links, err = GetHardLinkCount(getSharedLOBChunkPath(lobinfo.SHA, 0))
				Expect(err).To(BeNil(), "Shouldn't be error getting shared LOB hard link info")
				Expect(links).To(Equal(2), "Should be the right number of hard links (local)")
			})

		})

		Context("Store large multiple chunk LOB (shared store) [LONGTEST]", func() {

			testFileName := path.Join(folders[2], "large.dat")
			var correctLOBInfo *LOBInfo

			BeforeEach(func() {
				correctLOBInfo = CreateLargeTestLOBFileForStoring(testFileName)
			})
			AfterEach(func() {
				os.Remove(testFileName)
			})

			It("correctly stores a large file (shared store)", func() {
				f, err := os.Open(testFileName)
				if err != nil {
					Fail(fmt.Sprintf("Can't reopen test file %v: %v", testFileName, err))
				}
				defer f.Close()
				// Need to read leader for consistency with real usage
				leader := make([]byte, SHALineLen)
				c, err := f.Read(leader)
				if err != nil {
					Fail(fmt.Sprintf("Can't read leader of test file %v: %v", testFileName, err))
				}
				lobinfo, err := StoreLOB(f, leader[:c])
				Expect(err).To(BeNil(), "Shouldn't be error storing LOB")
				Expect(lobinfo).To(Equal(correctLOBInfo))
				lobinfo, err = GetLOBInfo(correctLOBInfo.SHA)
				Expect(err).To(BeNil(), "Shouldn't be error retrieving LOB info")
				Expect(lobinfo).To(Equal(correctLOBInfo))

				for i := 0; i < lobinfo.NumChunks; i++ {
					fileinfo, err := os.Stat(getLocalLOBChunkPath(lobinfo.SHA, i))
					Expect(err).To(BeNil(), "Shouldn't be error opening stored LOB #%v", i)
					if i+1 < lobinfo.NumChunks {
						Expect(fileinfo.Size()).To(BeEquivalentTo(GlobalOptions.ChunkSize), "Stored LOB #%v should be chunk limit size", i)
					} else {
						Expect(fileinfo.Size()).To(BeEquivalentTo(lobinfo.Size%GlobalOptions.ChunkSize), "Stored LOB #%v should be correct size", i)
					}
					// Also check shared
					fileinfo, err = os.Stat(getSharedLOBChunkPath(lobinfo.SHA, i))
					Expect(err).To(BeNil(), "Shouldn't be error opening stored LOB #%v", i)
					if i+1 < lobinfo.NumChunks {
						Expect(fileinfo.Size()).To(BeEquivalentTo(GlobalOptions.ChunkSize), "Stored LOB #%v should be chunk limit size", i)
					} else {
						Expect(fileinfo.Size()).To(BeEquivalentTo(lobinfo.Size%GlobalOptions.ChunkSize), "Stored LOB #%v should be correct size", i)
					}
					links, err := GetHardLinkCount(getLocalLOBChunkPath(lobinfo.SHA, i))
					Expect(err).To(BeNil(), "Shouldn't be error getting local LOB hard link info")
					Expect(links).To(Equal(2), "Should be the right number of hard links (shared)")
					links, err = GetHardLinkCount(getSharedLOBChunkPath(lobinfo.SHA, i))
					Expect(err).To(BeNil(), "Shouldn't be error getting shared LOB hard link info")
					Expect(links).To(Equal(2), "Should be the right number of hard links (local)")

				}
			})

		})

	})

	Describe("Retrieving a LOB (shared store)", func() {
		// Common git repo
		BeforeEach(func() {
			os.MkdirAll(sharedStore, 0755)
			GlobalOptions.SharedStore = sharedStore
			// Set up git repo with some subfolders
			CreateGitRepoForTest(root)

			for _, f := range folders {
				err := os.MkdirAll(f, 0755)
				if err != nil {
					fmt.Printf("Can't MkdirAll %v: %v", f, err)
				}
			}

		})

		AfterEach(func() {
			// Delete repo
			os.RemoveAll(root)
			os.RemoveAll(sharedStore)
			GlobalOptions.SharedStore = ""
		})

		Context("Retrieve small single chunk LOB (shared store)", func() {
			var correctLOBInfo *LOBInfo

			BeforeEach(func() {
				correctLOBInfo = CreateSmallTestLOBDataForRetrieval()
			})

			It("correctly retrieves small LOB file (shared store)", func() {
				// output to a temp file
				out, err := ioutil.TempFile("", "lobsmall.dat")
				Expect(err).To(BeNil(), "Shouldn't be error creating temp file")
				outFilename := out.Name()
				info, err := RetrieveLOB(correctLOBInfo.SHA, out)

				Expect(err).To(BeNil(), "Shouldn't be error retrieving LOB")
				out.Close()

				Expect(info).To(Equal(correctLOBInfo), "Metadata should agree")
				// Check output file
				stat, err := os.Stat(outFilename)
				Expect(err).To(BeNil(), "Shouldn't be error checking output file")
				Expect(stat.Size()).To(Equal(info.Size), "Size on disk should agree with metadata")

				os.Remove(outFilename)

			})

		})
		Context("Retrieve large multiple chunk LOB (shared store) [LONGTEST]", func() {
			var correctLOBInfo *LOBInfo

			BeforeEach(func() {
				correctLOBInfo = CreateLargeTestLOBDataForRetrieval()
			})

			It("correctly retrieves large LOB file (shared store)", func() {
				// output to a temp file
				out, err := ioutil.TempFile("", "loblarge.dat")
				Expect(err).To(BeNil(), "Shouldn't be error creating temp file")
				outFilename := out.Name()
				info, err := RetrieveLOB(correctLOBInfo.SHA, out)

				Expect(err).To(BeNil(), "Shouldn't be error retrieving LOB")
				out.Close()

				Expect(info).To(Equal(correctLOBInfo), "Metadata should agree")
				// Check output file
				stat, err := os.Stat(outFilename)
				Expect(err).To(BeNil(), "Shouldn't be error checking output file")
				Expect(stat.Size()).To(Equal(info.Size), "Size on disk should agree with metadata")

				os.Remove(outFilename)

			})

		})

	})
	Describe("Getting & checking LOB files", func() {
		var lobinfos []*LOBInfo
		var origDir string
		var smallFileIdx []int
		var midFileIdx []int
		var largeFileIdx []int
		var savedChunkSize int64
		BeforeEach(func() {
			CreateGitRepoForTest(root)
			origDir, _ = os.Getwd()
			os.Chdir(root)

			files := []string{
				"smallfile1.bin",
				"smallfile2.bin",
				"smallfile3.bin",
				"midfile1.bin",
				"midfile2.bin",
				"midfile3.bin",
				"largefile1.bin",
				"largefile2.bin"}

			// Reduce global chunk size for test
			// we need to test many chunks but let's not take lots of time
			savedChunkSize = GlobalOptions.ChunkSize
			GlobalOptions.ChunkSize = 16384

			sizes := []int64{50, 150, 200,
				GlobalOptions.ChunkSize + 100,
				GlobalOptions.ChunkSize + 1200,
				GlobalOptions.ChunkSize + 3400,
				GlobalOptions.ChunkSize*3 - 200,
				GlobalOptions.ChunkSize*3 - 1000}

			smallFileIdx = []int{0, 1, 2}
			midFileIdx = []int{3, 4, 5}
			largeFileIdx = []int{6, 7}

			// Create a bunch of files
			lobinfos = make([]*LOBInfo, 0, len(files))
			for i, f := range files {
				sz := sizes[i]
				filename := path.Join(root, f)
				CreateRandomFileForTest(sz, filename)
				info, err := StoreLOBForTest(filename)
				if err != nil {
					Fail(err.Error())
				}
				lobinfos = append(lobinfos, info)
			}

		})
		AfterEach(func() {
			os.Chdir(origDir)
			// Delete repo
			os.RemoveAll(root)

			GlobalOptions.ChunkSize = savedChunkSize
		})

		It("Shallow checks LOB files", func() {
			// Initial test, everything should validate (just use check)
			basedir := GetLocalLOBRoot()
			for _, li := range lobinfos {
				files, sz, err := GetLOBFilesForSHA(li.SHA, basedir, true, false)
				Expect(err).To(BeNil(), "Should be no error when checking LOB file for %v", li.SHA)
				Expect(files).To(HaveLen(li.NumChunks+1), "Should have the right number of files")
				Expect(sz).To(BeEquivalentTo(li.Size), "Total size should be correct")
			}

			// Test for simple corruptions
			// Remove a meta file
			var err error
			metafile := getLocalLOBMetaPath(lobinfos[smallFileIdx[0]].SHA)
			os.Remove(metafile)
			err = CheckLOBFilesForSHA(lobinfos[smallFileIdx[0]].SHA, basedir, false)
			Expect(err).ToNot(BeNil(), "Should detect missing meta file")

			var chunkfile string
			// Remove a chunk file (only one)
			chunkfile = getLocalLOBChunkPath(lobinfos[smallFileIdx[1]].SHA, 0)
			os.Remove(chunkfile)
			err = CheckLOBFilesForSHA(lobinfos[smallFileIdx[1]].SHA, basedir, false)
			Expect(err).ToNot(BeNil(), "Should detect missing chunk file for single-chunk file")
			// Remove a chunk file (one of many - first)
			chunkfile = getLocalLOBChunkPath(lobinfos[midFileIdx[0]].SHA, 0)
			os.Remove(chunkfile)
			err = CheckLOBFilesForSHA(lobinfos[midFileIdx[0]].SHA, basedir, false)
			Expect(err).ToNot(BeNil(), "Should detect missing first chunk file for 2-chunk file")
			// Remove a chunk file (one of many - last)
			chunkfile = getLocalLOBChunkPath(lobinfos[midFileIdx[1]].SHA, 1)
			os.Remove(chunkfile)
			err = CheckLOBFilesForSHA(lobinfos[midFileIdx[1]].SHA, basedir, false)
			Expect(err).ToNot(BeNil(), "Should detect missing second chunk file for 2-chunk file")

			// Change the size of a chunk file (single chunk)
			chunkfile = getLocalLOBChunkPath(lobinfos[smallFileIdx[2]].SHA, 0)
			f, _ := os.OpenFile(chunkfile, os.O_APPEND|os.O_RDWR, 0644)
			f.Write([]byte("icorruptthee"))
			f.Close()
			err = CheckLOBFilesForSHA(lobinfos[smallFileIdx[2]].SHA, basedir, false)
			Expect(err).ToNot(BeNil(), "Should detect incorrect size chunk file for single-chunk file")
			// Change the size of a chunk file (one of many - first)
			chunkfile = getLocalLOBChunkPath(lobinfos[midFileIdx[2]].SHA, 0)
			f, _ = os.OpenFile(chunkfile, os.O_APPEND|os.O_RDWR, 0644)
			f.Write([]byte("hssss"))
			f.Close()
			err = CheckLOBFilesForSHA(lobinfos[midFileIdx[2]].SHA, basedir, false)
			Expect(err).ToNot(BeNil(), "Should detect incorrect size chunk file for multi-chunk file (first)")
			// Change the size of a chunk file (one of many - middle)
			chunkfile = getLocalLOBChunkPath(lobinfos[largeFileIdx[0]].SHA, 1)
			f, _ = os.OpenFile(chunkfile, os.O_APPEND|os.O_RDWR, 0644)
			f.Write([]byte("itburns"))
			f.Close()
			err = CheckLOBFilesForSHA(lobinfos[largeFileIdx[0]].SHA, basedir, false)
			Expect(err).ToNot(BeNil(), "Should detect incorrect size chunk file for multi-chunk file (middle)")
			// Change the size of a chunk file (one of many - last)
			chunkfile = getLocalLOBChunkPath(lobinfos[largeFileIdx[1]].SHA, lobinfos[largeFileIdx[1]].NumChunks-1)
			f, _ = os.OpenFile(chunkfile, os.O_APPEND|os.O_RDWR, 0644)
			f.Write([]byte("ngggg"))
			f.Close()
			err = CheckLOBFilesForSHA(lobinfos[largeFileIdx[1]].SHA, basedir, false)
			Expect(err).ToNot(BeNil(), "Should detect incorrect size chunk file for multi-chunk file (last)")

		})

		It("Deep checks LOB files", func() {
			// Initial test, everything should validate (just use check)
			basedir := GetLocalLOBRoot()
			for _, li := range lobinfos {
				files, sz, err := GetLOBFilesForSHA(li.SHA, basedir, true, true)
				Expect(err).To(BeNil(), "Should be no error when checking LOB file for %v", li.SHA)
				Expect(files).To(HaveLen(li.NumChunks+1), "Should have the right number of files")
				Expect(sz).To(BeEquivalentTo(li.Size), "Total size should be correct")
			}

			// Test for deep corruptions
			var chunkfile string
			var err error
			// Change 2 bytes of a chunk file, size unchanged (single chunk)
			chunkfile = getLocalLOBChunkPath(lobinfos[smallFileIdx[0]].SHA, 0)
			f, _ := os.OpenFile(chunkfile, os.O_RDWR|os.O_SYNC, 0644)
			f.Seek(10, os.SEEK_SET)
			f.Write([]byte("qq"))
			f.Close()
			// check that we wouldn't detect this without checking the SHA
			err = CheckLOBFilesForSHA(lobinfos[smallFileIdx[0]].SHA, basedir, false)
			Expect(err).To(BeNil(), "Should not detect the corruption without deep hash check")
			err = CheckLOBFilesForSHA(lobinfos[smallFileIdx[0]].SHA, basedir, true)
			Expect(err).ToNot(BeNil(), "Should detect the corruption with deep hash check")
			// Change 2 bytes of a chunk file, size unchanged (multiple chunk)
			chunkfile = getLocalLOBChunkPath(lobinfos[midFileIdx[0]].SHA, 1)
			f, _ = os.OpenFile(chunkfile, os.O_RDWR|os.O_SYNC, 0644)
			f.Seek(51, os.SEEK_SET)
			f.Write([]byte("zf"))
			f.Close()
			err = CheckLOBFilesForSHA(lobinfos[midFileIdx[0]].SHA, basedir, false)
			Expect(err).To(BeNil(), "Should not detect the corruption without deep hash check (second chunk)")
			err = CheckLOBFilesForSHA(lobinfos[midFileIdx[0]].SHA, basedir, true)
			Expect(err).ToNot(BeNil(), "Should detect the corruption with deep hash check (second chunk)")

		})

	})

})
