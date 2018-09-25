package filesystem

import (
	"golang.org/x/exp/mmap"
	"gopkg.in/src-d/go-git.v4/plumbing/format/commitgraph"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/dotgit"
)

type CommitGraphStorage struct {
	dir         *dotgit.DotGit
	file        *mmap.ReaderAt
	commitGraph commitgraph.Index
}

func (s *CommitGraphStorage) CommitGraphIndex() (commitgraph.Index, error) {
	if s.commitGraph != nil {
		return s.commitGraph, nil
	}

	path := s.dir.Fs().Join(s.dir.Fs().Root(), s.dir.CommitGraphPath())

	file, err := mmap.Open(path)
	if err != nil {
		return nil, err
	}

	index, err := commitgraph.OpenFileIndex(file)
	if err == nil {
		s.commitGraph = index
		s.file = file
	} else {
		file.Close()
	}

	return index, err
}

func (s *CommitGraphStorage) SetCommitGraphIndex(index commitgraph.Index) error {
	// Throw away existing commit graph if we already loaded it
	if s.commitGraph != nil {
		if err := s.file.Close(); err != nil {
			return err
		}
		s.commitGraph = nil
	}

	f, err := s.dir.Fs().Create(s.dir.CommitGraphPath())
	if err != nil {
		return err
	}

	// FIXME: Error handling
	encoder := commitgraph.NewEncoder(f)
	return encoder.Encode(index)
}
