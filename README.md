# git-lob #
## About ##
git-lob is a git extension for efficiently handling binary files in [Git](http://git-scm.com). Instead of storing the binary file content inside the git repo, it is hashed & externalised, with the git commit only referring to the hash. The binary store is synchronised separately from the commits. 

## Why is it useful? ##
1. Adding large binaries to git slows it down & makes cloning slow
2. Most people don't need or want to have every version ever of large binary files on their disk when they only use the more recent ones
3. We need efficient ways to synchronise large binaries via deltas

git-lob keeps your Git repository smaller and faster, while providing rich functionality for managing binary files linked to the source code. 

The target audience is a project team on Windows, Mac or Linux who generally collaborate through one 'master' upstream repo (and maybe a small number of forks) and want to use the speed & power of Git for all their code & smaller resources while having lots of large binary files which are potentially inter-dependent with the history of that code.

## Why write another large file extension? Why not use X? ##
There are a number of existing projects attempting to address this issue. We looked at all of the ones we could find before deciding to start another one.
In truth we didn't find an existing solution that fulfilled all the requirements we set ourselves, which were:

1. **Fast**. This meant a preference for native code over scripts/VMs, especially as Git may be making frequent calls to our extension, startup had to be quick
2. **Support unmodified Git**. Forks of Git are out.
3. **First class support for Windows as well as Mac and Linux**. Game developers need binary files a lot, and a very large number are using Windows. 
4. **Simple to deploy**. Avoid complex runtime environments with lots of potentially version-specific dependencies. Again scripting languages come off worse here.
5. **Extended features**. There were many features we wanted that were missing from some or all existing implementations, including:
    * Sharing local binary stores across repos to avoid duplication
    * Branch-oriented synchronisation of binaries, like Git itself
    * Locking to avoid unintentional unresolveable merges. 
    * Using binary deltas to reduce transfer times
    * Support for proxy caches
    * Scalability to huge repositories if needed

So to relate this to specifically why we ruled out some of the existing projects:

* [git-annex](http://git-annex.branchable.com) doesn't fully support Windows and is more complex because it's highly generalised & distributed. It's solving a slightly different problem.
* [git-media](https://github.com/alebedev/git-media), [git-fat](https://github.com/jedbrown/git-fat), [git-bigstore](https://github.com/lionheart/git-bigstore) etc require scripting language VMs to be started on every call, which is slower than native code for startup and runtime. They'd also need significant extension anyway.
* [git-fit](https://github.com/dailymuse/git-fit) is too simple for our needs (doesn't integrate with Git really)
* [git-bigfiles](https://github.com/lionheart/git-bigstore) is a git fork, which we ruled out

## Why Go? ##
So why pick Go as the language for git-lob? I have experience in a lot of languages so I could have picked any of them (or, as it happened, learn a new one). Here's why:

1. It produces native binaries with zero runtime dependencies (fast & easy to deploy) on all 3 platforms
2. It's a productive, highish level language with good library support for hashing, threading, networking
3. It's mature & stable (compared to, say, Rust; at time of writing)
4. It's fairly easy to learn & contribute (compared to, say, Haskell or even C)

