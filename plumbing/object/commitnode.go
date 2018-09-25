package object

import (
	"fmt"
	"io"
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing/format/commitgraph"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

// CommitNode is generic interface encapsulating either Commit object or
// graphCommitNode object
type CommitNode interface {
	ID() plumbing.Hash
	Tree() (*Tree, error)
	CommitTime() time.Time
}

// CommitNodeIndex is generic interface encapsulating an index of CommitNode objects
// and accessor methods for walking it as a directed graph
type CommitNodeIndex interface {
	NumParents(node CommitNode) int
	ParentNodes(node CommitNode) CommitNodeIter
	ParentNode(node CommitNode, i int) (CommitNode, error)
	ParentHashes(node CommitNode) []plumbing.Hash

	// Commit returns the full commit object from the node
	Commit(node CommitNode) (*Commit, error)
}

// CommitNodeIter is a generic closable interface for iterating over commit nodes.
type CommitNodeIter interface {
	Next() (CommitNode, error)
	ForEach(func(CommitNode) error) error
	Close()
}

// graphCommitNode is a reduced representation of Commit as presented in the commit
// graph file (commitgraph.Node). It is merely useful as an optimization for walking
// the commit graphs.
//
// graphCommitNode implements the CommitNode interface.
type graphCommitNode struct {
	// Hash for the Commit object
	hash plumbing.Hash
	// Index of the node in the commit graph file
	index int

	node *commitgraph.Node
	gci  *graphCommitNodeIndex
}

// graphCommitNodeIndex is an index that can load CommitNode objects from both the commit
// graph files and the object store.
//
// graphCommitNodeIndex implements the CommitNodeIndex interface
type graphCommitNodeIndex struct {
	commitGraph commitgraph.Index
	s           storer.EncodedObjectStorer
}

// objectCommitNodeIndex is an index that can load CommitNode objects only from the
// object store.
//
// objectCommitNodeIndex implements the CommitNodeIndex interface
type objectCommitNodeIndex struct {
	s storer.EncodedObjectStorer
}

// ID returns the Commit object id referenced by the commit graph node.
func (c *graphCommitNode) ID() plumbing.Hash {
	return c.hash
}

// Tree returns the Tree referenced by the commit graph node.
func (c *graphCommitNode) Tree() (*Tree, error) {
	return GetTree(c.gci.s, c.node.TreeHash)
}

// CommitTime returns the Commiter.When time of the Commit referenced by the commit graph node.
func (c *graphCommitNode) CommitTime() time.Time {
	return c.node.When
}

func (c *graphCommitNode) String() string {
	return fmt.Sprintf(
		"%s %s\nDate:   %s",
		plumbing.CommitObject, c.ID(),
		c.CommitTime().Format(DateFormat),
	)
}

func NewGraphCommitNodeIndex(commitGraph commitgraph.Index, s storer.EncodedObjectStorer) CommitNodeIndex {
	return &graphCommitNodeIndex{commitGraph, s}
}

// NumParents returns the number of parents in a commit.
func (gci *graphCommitNodeIndex) NumParents(node CommitNode) int {
	if cgn, ok := node.(*graphCommitNode); ok {
		return len(cgn.node.ParentIndexes)
	}
	co := node.(*Commit)
	return co.NumParents()
}

// ParentNodes return a CommitNodeIter for parents of specified node.
func (gci *graphCommitNodeIndex) ParentNodes(node CommitNode) CommitNodeIter {
	return newParentgraphCommitNodeIter(gci, node)
}

// ParentNode returns the ith parent of a commit.
func (gci *graphCommitNodeIndex) ParentNode(node CommitNode, i int) (CommitNode, error) {
	if cgn, ok := node.(*graphCommitNode); ok {
		if len(cgn.node.ParentIndexes) == 0 || i >= len(cgn.node.ParentIndexes) {
			return nil, ErrParentNotFound
		}

		parent, err := gci.commitGraph.GetNodeByIndex(cgn.node.ParentIndexes[i])
		if err != nil {
			return nil, err
		}

		return &graphCommitNode{
			hash:  cgn.node.ParentHashes[i],
			index: cgn.node.ParentIndexes[i],
			node:  parent,
			gci:   gci,
		}, nil
	}

	co := node.(*Commit)
	if len(co.ParentHashes) == 0 || i >= len(co.ParentHashes) {
		return nil, ErrParentNotFound
	}

	// Check the commit graph first
	parentHash := co.ParentHashes[i]
	parentIndex, err := gci.commitGraph.GetIndexByHash(parentHash)
	if err == nil {
		parent, err := gci.commitGraph.GetNodeByIndex(parentIndex)
		if err != nil {
			return nil, err
		}

		return &graphCommitNode{
			hash:  parentHash,
			index: parentIndex,
			node:  parent,
			gci:   gci,
		}, nil
	}

	// Fallback to loading full commit object
	return GetCommit(gci.s, parentHash)
}

// ParentHashes returns hashes of the parent commits for a specified node
func (gci *graphCommitNodeIndex) ParentHashes(node CommitNode) []plumbing.Hash {
	if cgn, ok := node.(*graphCommitNode); ok {
		return cgn.node.ParentHashes
	}
	co := node.(*Commit)
	return co.ParentHashes
}

// Commit returns the full Commit object representing the commit graph node.
func (gci *graphCommitNodeIndex) Commit(node CommitNode) (*Commit, error) {
	if cgn, ok := node.(*graphCommitNode); ok {
		return GetCommit(gci.s, cgn.ID())
	}
	co := node.(*Commit)
	return co, nil
}

func NewObjectCommitNodeIndex(s storer.EncodedObjectStorer) CommitNodeIndex {
	return &objectCommitNodeIndex{s}
}

// NumParents returns the number of parents in a commit.
func (oci *objectCommitNodeIndex) NumParents(node CommitNode) int {
	co := node.(*Commit)
	return co.NumParents()
}

// ParentNodes return a CommitNodeIter for parents of specified node.
func (oci *objectCommitNodeIndex) ParentNodes(node CommitNode) CommitNodeIter {
	return newParentgraphCommitNodeIter(oci, node)
}

// ParentNode returns the ith parent of a commit.
func (oci *objectCommitNodeIndex) ParentNode(node CommitNode, i int) (CommitNode, error) {
	co := node.(*Commit)
	return co.Parent(i)
}

// ParentHashes returns hashes of the parent commits for a specified node
func (oci *objectCommitNodeIndex) ParentHashes(node CommitNode) []plumbing.Hash {
	co := node.(*Commit)
	return co.ParentHashes
}

// Commit returns the full Commit object representing the commit graph node.
func (oci *objectCommitNodeIndex) Commit(node CommitNode) (*Commit, error) {
	co := node.(*Commit)
	return co, nil
}

// parentCommitNodeIter provides an iterator for parent commits from associated CommitNodeIndex.
type parentCommitNodeIter struct {
	gci  CommitNodeIndex
	node CommitNode
	i    int
}

func newParentgraphCommitNodeIter(gci CommitNodeIndex, node CommitNode) CommitNodeIter {
	return &parentCommitNodeIter{gci, node, 0}
}

// Next moves the iterator to the next commit and returns a pointer to it. If
// there are no more commits, it returns io.EOF.
func (iter *parentCommitNodeIter) Next() (CommitNode, error) {
	obj, err := iter.gci.ParentNode(iter.node, iter.i)
	if err == ErrParentNotFound {
		return nil, io.EOF
	}
	if err == nil {
		iter.i++
	}

	return obj, err
}

// ForEach call the cb function for each commit contained on this iter until
// an error appends or the end of the iter is reached. If ErrStop is sent
// the iteration is stopped but no error is returned. The iterator is closed.
func (iter *parentCommitNodeIter) ForEach(cb func(CommitNode) error) error {
	for {
		obj, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			return err
		}

		if err := cb(obj); err != nil {
			if err == storer.ErrStop {
				return nil
			}

			return err
		}
	}
}

func (iter *parentCommitNodeIter) Close() {
}
