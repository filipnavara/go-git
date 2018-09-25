package storer

import "gopkg.in/src-d/go-git.v4/plumbing/format/commitgraph"

// CommitGraphStorer generic storage of CommitGraph object
type CommitGraphStorer interface {
	CommitGraphIndex() (commitgraph.Index, error)
	SetCommitGraphIndex(commitgraph.Index) error
}
