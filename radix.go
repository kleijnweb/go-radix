package radix

import (
	"sort"
	"strings"
)

// WalkFn is used when walking the tree. Takes a
// key and value, returning if iteration should
// be terminated.
type WalkFn[T any] func(s string, v T) bool

// leafNode is used to represent a value.
type leafNode[T any] struct {
	key string
	val T
}

// edge is used to represent an edge node.
type edge[T any] struct {
	label byte
	node  *node[T]
}

type node[T any] struct {
	// leaf is used to store possible leaf
	leaf *leafNode[T]

	// prefix is the common prefix we ignore
	prefix string

	// Edges should be stored in-order for iteration.
	// We avoid a fully materialized slice to save memory,
	// since in most cases we expect to be sparse
	edges edges[T]
}

func (n *node[T]) isLeaf() bool {
	return n.leaf != nil
}

func (n *node[T]) addEdge(e edge[T]) {
	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= e.label
	})

	n.edges = append(n.edges, edge[T]{})
	copy(n.edges[idx+1:], n.edges[idx:])
	n.edges[idx] = e
}

func (n *node[T]) updateEdge(label byte, node *node[T]) {
	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= label
	})
	if idx < num && n.edges[idx].label == label {
		n.edges[idx].node = node
		return
	}
	panic("replacing missing edge")
}

func (n *node[T]) getEdge(label byte) *node[T] {
	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= label
	})
	if idx < num && n.edges[idx].label == label {
		return n.edges[idx].node
	}
	return nil
}

func (n *node[T]) delEdge(label byte) {
	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= label
	})
	if idx < num && n.edges[idx].label == label {
		copy(n.edges[idx:], n.edges[idx+1:])
		n.edges[len(n.edges)-1] = edge[T]{}
		n.edges = n.edges[:len(n.edges)-1]
	}
}

type edges[T any] []edge[T]

func (e edges[T]) Len() int {
	return len(e)
}

func (e edges[T]) Less(i, j int) bool {
	return e[i].label < e[j].label
}

func (e edges[T]) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
}

func (e edges[T]) Sort() {
	sort.Sort(e)
}

// Tree implements a radix tree. This can be treated as a
// Dictionary abstract data type. The main advantage over
// a standard hash map is prefix-based lookups and
// ordered iteration,.
type Tree[T any] struct {
	root *node[T]
	size int
}

// New returns an empty Tree.
func New[T any]() *Tree[T] {
	return NewFromMap[T](nil)
}

// NewFromMap returns a new tree containing the keys
// from an existing map.
func NewFromMap[T any](m map[string]T) *Tree[T] {
	t := &Tree[T]{root: &node[T]{}}
	for k, v := range m {
		t.Insert(k, v)
	}
	return t
}

// Len is used to return the number of elements in the tree.
func (t *Tree[T]) Len() int {
	return t.size
}

// longestPrefix finds the length of the shared prefix
// of two strings.
func longestPrefix(a, b string) int {
	longest := len(a)
	if l := len(b); l < longest {
		longest = l
	}
	var i int
	for i = 0; i < longest; i++ { //nolint: intrange
		if a[i] != b[i] {
			break
		}
	}
	return i
}

// Insert is used to add a newentry or update
// an existing entry. Returns true if an existing record is updated.
func (t *Tree[T]) Insert(path string, value T) (existing T, updated bool) { //nolint: funlen
	var parent *node[T]
	current := t.root
	search := path
	for {
		// Handle key exhaution
		if len(search) == 0 {
			if current.isLeaf() {
				old := current.leaf.val
				current.leaf.val = value
				return old, true
			}

			current.leaf = &leafNode[T]{
				key: path,
				val: value,
			}
			t.size++
			return
		}

		// Look for the edge
		parent = current
		current = current.getEdge(search[0])

		// No edge, create one
		if current == nil {
			e := edge[T]{
				label: search[0],
				node: &node[T]{
					leaf: &leafNode[T]{
						key: path,
						val: value,
					},
					prefix: search,
				},
			}
			parent.addEdge(e)
			t.size++
			return
		}

		// Determine longest prefix of the search key on match
		commonPrefix := longestPrefix(search, current.prefix)
		if commonPrefix == len(current.prefix) {
			search = search[commonPrefix:]
			continue
		}

		// Split the node
		t.size++
		child := &node[T]{
			prefix: search[:commonPrefix],
		}
		parent.updateEdge(search[0], child)

		// Restore the existing node
		child.addEdge(edge[T]{
			label: current.prefix[commonPrefix],
			node:  current,
		})
		current.prefix = current.prefix[commonPrefix:]

		// Create a new leaf node
		leaf := &leafNode[T]{
			key: path,
			val: value,
		}

		// If the new key is a subset, add to this node
		search = search[commonPrefix:]
		if len(search) == 0 {
			child.leaf = leaf
			return
		}

		// Create a new edge for the node
		child.addEdge(edge[T]{
			label: search[0],
			node: &node[T]{
				leaf:   leaf,
				prefix: search,
			},
		})
		return
	}
}

// Delete is used to delete a key, returning the previous
// value and if it was deleted.
func (t *Tree[T]) Delete(s string) (existing T, deleted bool) { //nolint: cyclop
	var parent *node[T]
	var label byte
	current := t.root
	search := s
	for {
		// Check for key exhaution
		if len(search) == 0 {
			if !current.isLeaf() {
				break
			}
			goto DELETE
		}

		// Look for an edge
		parent = current
		label = search[0]
		current = current.getEdge(label)
		if current == nil {
			break
		}

		// Consume the search prefix
		if strings.HasPrefix(search, current.prefix) {
			search = search[len(current.prefix):]
		} else {
			break
		}
	}
	return

DELETE:
	// Delete the leaf
	leaf := current.leaf
	current.leaf = nil
	t.size--

	// Check if we should delete this node from the parent
	if parent != nil && len(current.edges) == 0 {
		parent.delEdge(label)
	}

	// Check if we should merge this node
	if current != t.root && len(current.edges) == 1 {
		current.mergeChild()
	}

	// Check if we should merge the parent's other child
	if parent != nil && parent != t.root && len(parent.edges) == 1 && !parent.isLeaf() {
		parent.mergeChild()
	}

	return leaf.val, true
}

// DeletePrefix is used to delete the subtree under a prefix
// Returns how many nodes were deleted
// Use this to delete large subtrees efficiently.
func (t *Tree[T]) DeletePrefix(s string) int {
	return t.deletePrefix(nil, t.root, s)
}

// delete does a recursive deletion.
func (t *Tree[T]) deletePrefix(parent, current *node[T], prefix string) int { //nolint: cyclop
	// Check for key exhaustion
	if len(prefix) == 0 {
		// Remove the leaf node
		subTreeSize := 0
		// recursively walk from all edges of the node to be deleted
		recursiveWalk(current, func(string, T) bool {
			subTreeSize++
			return false
		})
		if current.isLeaf() {
			current.leaf = nil
		}
		current.edges = nil // deletes the entire subtree

		// Check if we should merge the parent's other child
		if parent != nil && parent != t.root && len(parent.edges) == 1 && !parent.isLeaf() {
			parent.mergeChild()
		}
		t.size -= subTreeSize
		return subTreeSize
	}

	// Look for an edge
	label := prefix[0]
	child := current.getEdge(label)
	if child == nil || (!strings.HasPrefix(child.prefix, prefix) && !strings.HasPrefix(prefix, child.prefix)) {
		return 0
	}

	// Consume the search prefix
	if len(child.prefix) > len(prefix) {
		prefix = prefix[len(prefix):]
	} else {
		prefix = prefix[len(child.prefix):]
	}
	return t.deletePrefix(current, child, prefix)
}

func (n *node[T]) mergeChild() {
	e := n.edges[0]
	child := e.node
	n.prefix += child.prefix
	n.leaf = child.leaf
	n.edges = child.edges
}

// Get is used to lookup a specific key, returning
// the value and if it was found.
func (t *Tree[T]) Get(s string) (value T, found bool) {
	current := t.root
	search := s
	for {
		// Check for key exhaution
		if len(search) == 0 {
			if current.isLeaf() {
				return current.leaf.val, true
			}
			break
		}

		// Look for an edge
		current = current.getEdge(search[0])
		if current == nil {
			break
		}

		// Consume the search prefix
		if strings.HasPrefix(search, current.prefix) {
			search = search[len(current.prefix):]
		} else {
			break
		}
	}
	return
}

// LongestPrefix is like Get, but instead of an
// exact match, it will return the longest prefix match.
func (t *Tree[T]) LongestPrefix(s string) (key string, value T, found bool) {
	var last *leafNode[T]
	current := t.root
	search := s
	for {
		// Look for a leaf node
		if current.isLeaf() {
			last = current.leaf
		}

		// Check for key exhaution
		if len(search) == 0 {
			break
		}

		// Look for an edge
		current = current.getEdge(search[0])
		if current == nil {
			break
		}

		// Consume the search prefix
		if strings.HasPrefix(search, current.prefix) {
			search = search[len(current.prefix):]
		} else {
			break
		}
	}
	if last != nil {
		return last.key, last.val, true
	}
	return
}

// Minimum is used to return the minimum value in the tree.
func (t *Tree[T]) Minimum() (key string, value T, found bool) {
	current := t.root
	for {
		if current.isLeaf() {
			return current.leaf.key, current.leaf.val, true
		}
		if len(current.edges) > 0 {
			current = current.edges[0].node
		} else {
			break
		}
	}
	return
}

// Maximum is used to return the maximum value in the tree.
func (t *Tree[T]) Maximum() (key string, value T, found bool) {
	current := t.root
	for {
		if num := len(current.edges); num > 0 {
			current = current.edges[num-1].node
			continue
		}
		if current.isLeaf() {
			return current.leaf.key, current.leaf.val, true
		}
		break
	}
	return
}

// Walk is used to walk the tree.
func (t *Tree[T]) Walk(fn WalkFn[T]) {
	recursiveWalk(t.root, fn)
}

// WalkPrefix is used to walk the tree under a prefix.
func (t *Tree[T]) WalkPrefix(prefix string, fn WalkFn[T]) {
	current := t.root
	search := prefix
	for {
		// Check for key exhaustion
		if len(search) == 0 {
			recursiveWalk(current, fn)
			return
		}

		// Look for an edge
		current = current.getEdge(search[0])
		if current == nil {
			return
		}

		// Consume the search prefix
		if strings.HasPrefix(search, current.prefix) {
			search = search[len(current.prefix):]
			continue
		}
		if strings.HasPrefix(current.prefix, search) {
			// Child may be under our search prefix
			recursiveWalk(current, fn)
		}
		return
	}
}

// WalkPath is used to walk the tree, but only visiting nodes
// from the root down to a given leaf. Where WalkPrefix walks
// all the entries *under* the given prefix, this walks the
// entries *above* the given prefix.
func (t *Tree[T]) WalkPath(path string, fn WalkFn[T]) {
	current := t.root
	search := path
	for {
		// Visit the leaf values if any
		if current.leaf != nil && fn(current.leaf.key, current.leaf.val) {
			return
		}

		// Check for key exhaution
		if len(search) == 0 {
			return
		}

		// Look for an edge
		current = current.getEdge(search[0])
		if current == nil {
			return
		}

		// Consume the search prefix
		if strings.HasPrefix(search, current.prefix) {
			search = search[len(current.prefix):]
		} else {
			break
		}
	}
}

// recursiveWalk is used to do a pre-order walk of a node
// recursively. Returns true if the walk should be aborted.
func recursiveWalk[T any](current *node[T], fn WalkFn[T]) bool {
	// Visit the leaf values if any
	if current.leaf != nil && fn(current.leaf.key, current.leaf.val) {
		return true
	}

	// Recurse on the children
	i := 0
	numEdges := len(current.edges) // keeps track of number of edges in previous iteration
	for i < numEdges {
		e := current.edges[i]
		if recursiveWalk(e.node, fn) {
			return true
		}
		// It is a possibility that the WalkFn modified the node we are
		// iterating on. If there are no more edges, mergeChild happened,
		// so the last edge became the current node n, on which we'll
		// iterate one last time.
		if len(current.edges) == 0 {
			return recursiveWalk(current, fn)
		}
		// If there are now less edges than in the previous iteration,
		// then do not increment the loop index, since the current index
		// points to a new edge. Otherwise, get to the next index.
		if len(current.edges) >= numEdges {
			i++
		}
		numEdges = len(current.edges)
	}
	return false
}

// ToMap is used to walk the tree and convert it into a map.
func (t *Tree[T]) ToMap() map[string]T {
	out := make(map[string]T, t.size)
	t.Walk(func(k string, v T) bool {
		out[k] = v
		return false
	})
	return out
}
