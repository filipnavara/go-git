package main

import (
	"fmt"
	"os"

	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/_examples"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/cache"
	"gopkg.in/src-d/go-git.v4/plumbing/format/commitgraph"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"

	"gopkg.in/src-d/go-billy.v4/osfs"
)

// Example how to resolve a revision into its commit counterpart
func main() {
	CheckArgs("<path> <command>")

	path := os.Args[1]
	command := os.Args[2]

	// Currently the only command is `write`
	if command != "write" {
		fmt.Println("Unsupported command")
		return
	}

	// We instantiate a new repository targeting the given path (the .git folder)
	fs := osfs.New(path)
	s := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{KeepDescriptors: true})
	r, err := git.Open(s, fs)
	CheckIfError(err)

	h, err := r.Head()
	CheckIfError(err)

	commit, err := r.CommitObject(h.Hash())
	CheckIfError(err)

	idx, err := buildCommitGraph(commit)
	CheckIfError(err)

	err = s.SetCommitGraphIndex(idx)
	CheckIfError(err)

	s.Close()
}

func buildCommitGraph(c *object.Commit) (*commitgraph.MemoryIndex, error) {
	idx := commitgraph.NewMemoryIndex()
	seen := make(map[plumbing.Hash]bool)
	return idx, addCommitToIndex(idx, c, seen)
}

func createBloomFilter(a, b *object.Tree) *commitgraph.BloomPathFilter {
	bloomFilter := commitgraph.NewBloomPathFilter()

	aHashes := make(map[string]plumbing.Hash)
	aWalker := object.NewTreeWalker(a, true, nil)
	for {
		if name, entry, err := aWalker.Next(); err != nil {
			break
		} else {
			aHashes[name] = entry.Hash
		}
	}

	bWalker := object.NewTreeWalker(b, true, nil)
	for {
		if name, entry, err := bWalker.Next(); err != nil {
			break
		} else {
			if aHashes[name] != entry.Hash {
				// File from 'b' didn't exist in 'a', or it has different has than in 'a'
				bloomFilter.Add(name)
			}
			delete(aHashes, name)
		}
	}

	for name := range aHashes {
		// File from 'a' is removed in 'b'
		bloomFilter.Add(name)
	}

	return bloomFilter
}

func addCommitToIndex(idx *commitgraph.MemoryIndex, c *object.Commit, seen map[plumbing.Hash]bool) error {
	if seen[c.Hash] {
		return nil
	}
	seen[c.Hash] = true

	// Recursively add parents first
	err := c.Parents().ForEach(func(parent *object.Commit) error {
		return addCommitToIndex(idx, parent, seen)
	})
	if err != nil {
		return err
	}

	// Calculate file difference to first parent commit
	var bloomFilter *commitgraph.BloomPathFilter
	if parent, err := c.Parent(0); err == nil {
		if tree, err := c.Tree(); err == nil {
			if parentTree, err := parent.Tree(); err == nil {
				bloomFilter = createBloomFilter(parentTree, tree)
			}
		}
	}

	// Add this commit if it hasn't been done already
	node := &commitgraph.Node{
		TreeHash:     c.TreeHash,
		ParentHashes: c.ParentHashes,
		When:         c.Committer.When,
	}
	return idx.AddWithBloom(c.Hash, node, bloomFilter)
}
