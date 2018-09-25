package commitgraph

import (
	"gopkg.in/src-d/go-git.v4/plumbing"
)

type MemoryIndex struct {
	commitData []*Node
	indexMap   map[plumbing.Hash]int
}

// NewMemoryIndex creates in-memory commit graph representation
func NewMemoryIndex() *MemoryIndex {
	return &MemoryIndex{
		indexMap: make(map[plumbing.Hash]int),
	}
}

// GetIndexByHash gets the index in the commit graph from commit hash, if available
func (mi *MemoryIndex) GetIndexByHash(h plumbing.Hash) (int, error) {
	i, ok := mi.indexMap[h]
	if ok {
		return i, nil
	}

	return 0, plumbing.ErrObjectNotFound
}

// GetNodeByIndex gets the commit node from the commit graph using index
// obtained from child node, if available
func (mi *MemoryIndex) GetNodeByIndex(i int) (*Node, error) {
	if int(i) >= len(mi.commitData) {
		return nil, plumbing.ErrObjectNotFound
	}

	return mi.commitData[i], nil
}

// Hashes returns all the hashes that are available in the index
func (mi *MemoryIndex) Hashes() []plumbing.Hash {
	hashes := make([]plumbing.Hash, 0, len(mi.indexMap))
	for k := range mi.indexMap {
		hashes = append(hashes, k)
	}
	return hashes
}

// Add adds new node to the memory index
func (mi *MemoryIndex) Add(hash plumbing.Hash, node *Node) error {
	// Map parent hashes to parent indexes
	parentIndexes := make([]int, len(node.ParentHashes))
	for i, parentHash := range node.ParentHashes {
		var err error
		if parentIndexes[i], err = mi.GetIndexByHash(parentHash); err != nil {
			return err
		}
	}
	node.ParentIndexes = parentIndexes
	mi.indexMap[hash] = len(mi.commitData)
	mi.commitData = append(mi.commitData, node)
	return nil
}
