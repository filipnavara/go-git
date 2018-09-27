package main

import (
	"fmt"
	"os"

	"github.com/emirpasic/gods/trees/binaryheap"
	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/_examples"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/cache"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"

	"gopkg.in/src-d/go-billy.v4/osfs"
)

// Example how to resolve a revision into its commit counterpart
func main() {
	CheckArgs("<path>", "<revision>", "<tree path>")

	path := os.Args[1]
	revision := os.Args[2]
	treePath := os.Args[3]

	// We instantiate a new repository targeting the given path (the .git folder)
	fs := osfs.New(path)
	s := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{KeepDescriptors: true})
	r, err := git.Open(s, fs)
	CheckIfError(err)

	// Resolve revision into a sha1 commit, only some revisions are resolved
	// look at the doc to get more details
	Info("git rev-parse %s", revision)

	h, err := r.ResolveRevision(plumbing.Revision(revision))
	CheckIfError(err)

	commit, err := r.CommitObject(*h)
	CheckIfError(err)

	tree, err := commit.Tree()
	CheckIfError(err)
	if treePath != "" {
		tree, err = tree.Tree(treePath)
		CheckIfError(err)
	}

	var paths []string
	for _, entry := range tree.Entries {
		paths = append(paths, entry.Name)
	}

	revs, err := getLastCommitForPaths(r.CommitNodeIndex(), commit, treePath, paths)
	CheckIfError(err)
	for path, rev := range revs {
		fmt.Println(path)
		fmt.Println(rev)
	}

	s.Close()
}

type commitAndPaths struct {
	commit object.CommitNode
	// Paths that are still on the branch represented by commit
	paths []string
	// Set of hashes for the paths
	hashes map[string]plumbing.Hash
}

func getCommitTree(c object.CommitNode, treePath string) (*object.Tree, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}

	// Optimize deep traversals by focusing only on the specific tree
	if treePath != "" {
		tree, err = tree.Tree(treePath)
		if err != nil {
			return nil, err
		}
	}

	return tree, nil
}

func getFileHashes(c object.CommitNode, treePath string, paths []string) (map[string]plumbing.Hash, error) {
	tree, err := getCommitTree(c, treePath)
	if err == object.ErrDirectoryNotFound {
		// The whole tree didn't exist, so return empty map
		return make(map[string]plumbing.Hash), nil
	}
	if err != nil {
		return nil, err
	}

	hashes := make(map[string]plumbing.Hash)
	for _, path := range paths {
		if path != "" {
			entry, err := tree.FindEntry(path)
			if err == nil {
				hashes[path] = entry.Hash
			}
		} else {
			hashes[path] = tree.Hash
		}
	}

	return hashes, nil
}

func canSkipCommit(index object.CommitNodeIndex, commit object.CommitNode, treePath string, paths []string) bool {
	if bloom, err := index.BloomFilter(commit); err == nil {
		for _, path := range paths {
			fullPath := path
			if treePath != "" {
				if path != "" {
					fullPath = treePath + "/" + path
				} else {
					fullPath = treePath
				}
			}

			if bloom.Test(fullPath) {
				return false
			}
		}
		return true
	}
	return false
}

func getLastCommitForPaths(index object.CommitNodeIndex, c object.CommitNode, treePath string, paths []string) (map[string]*object.Commit, error) {
	// We do a tree traversal with nodes sorted by commit time
	seen := make(map[plumbing.Hash]bool)
	heap := binaryheap.NewWith(func(a, b interface{}) int {
		if a.(*commitAndPaths).commit.CommitTime().Before(b.(*commitAndPaths).commit.CommitTime()) {
			return 1
		}
		return -1
	})

	resultNodes := make(map[string]object.CommitNode)
	initialHashes, err := getFileHashes(c, treePath, paths)
	if err != nil {
		return nil, err
	}

	// Start search from the root commit and with full set of paths
	heap.Push(&commitAndPaths{c, paths, initialHashes})

	for {
		cIn, ok := heap.Pop()
		if !ok {
			break
		}
		current := cIn.(*commitAndPaths)
		currentID := current.commit.ID()

		if seen[currentID] {
			continue
		}
		seen[currentID] = true

		// Load the parent commits for the one we are currently examining
		numParents := index.NumParents(current.commit)
		var parents []object.CommitNode
		for i := 0; i < numParents; i++ {
			parent, err := index.ParentNode(current.commit, i)
			if err != nil {
				break
			}
			parents = append(parents, parent)
		}

		// Optimization: If there is only one parent and a bloom filter can tell us
		// that none of our paths has changed then skip all the change checking
		if numParents == 1 && canSkipCommit(index, current.commit, treePath, current.paths) {
			heap.Push(&commitAndPaths{parents[0], current.paths, current.hashes})
			continue
		}

		// Examine the current commit and set of interesting paths
		numOfParentsWithPath := make([]int, len(current.paths))
		pathChanged := make([]bool, len(current.paths))
		parentHashes := make([]map[string]plumbing.Hash, len(parents))
		for j, parent := range parents {
			parentHashes[j], err = getFileHashes(parent, treePath, current.paths)
			if err != nil {
				break
			}

			for i, path := range current.paths {
				if parentHashes[j][path] != plumbing.ZeroHash {
					numOfParentsWithPath[i]++
					if parentHashes[j][path] != current.hashes[path] {
						pathChanged[i] = true
					}
				}
			}
		}

		var remainingPaths []string
		for i, path := range current.paths {
			switch numOfParentsWithPath[i] {
			case 0:
				// The path didn't exist in any parent, so it must have been created by
				// this commit. The results could already contain some newer change from
				// different path, so don't override that.
				if resultNodes[path] == nil {
					resultNodes[path] = current.commit
				}
			case 1:
				// The file is present on exactly one parent, so check if it was changed
				// and save the revision if it did.
				if pathChanged[i] {
					if resultNodes[path] == nil {
						resultNodes[path] = current.commit
					}
				} else {
					remainingPaths = append(remainingPaths, path)
				}
			default:
				// The file is present on more than one of the parent paths, so this is
				// a merge. We have to examine all the parent trees to find out where
				// the change occured. pathChanged[i] would tell us that the file was
				// changed during the merge, but it wouldn't tell us the relevant commit
				// that introduced it.
				remainingPaths = append(remainingPaths, path)
			}
		}

		if len(remainingPaths) > 0 {
			// Add the parent nodes along with remaining paths to the heap for further
			// processing.
			for j, parent := range parents {
				if seen[parent.ID()] {
					continue
				}

				// Combine remainingPath with paths available on the parent branch
				// and make union of them
				var remainingPathsForParent []string
				for _, path := range remainingPaths {
					if parentHashes[j][path] != plumbing.ZeroHash {
						remainingPathsForParent = append(remainingPathsForParent, path)
					}
				}

				heap.Push(&commitAndPaths{parent, remainingPathsForParent, parentHashes[j]})
			}
		}
	}

	// Post-processing
	result := make(map[string]*object.Commit)
	for path, commitNode := range resultNodes {
		var err error
		result[path], err = index.Commit(commitNode)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}
