// Package edit implements atomic exact-match file replacement.
package edit

import (
	"bytes"
	"fmt"
	"sort"
)

type notFoundError struct {
	index int
}

func (e *notFoundError) Error() string {
	return fmt.Sprintf("edit %d: oldText not found", e.index)
}

type notUniqueError struct {
	index int
}

func (e *notUniqueError) Error() string {
	return fmt.Sprintf("edit %d: oldText is not unique", e.index)
}

type overlapError struct {
	index int
}

func (e *overlapError) Error() string {
	return fmt.Sprintf("edit %d: oldText overlaps another edit", e.index)
}

// Change is one exact-match replacement.
type Change struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// Apply performs all-or-nothing exact-match replacement. Every Change's
// OldText must occur exactly once in data before any replacement is applied.
// If any validation fails, data is returned unchanged and the error names the
// failing Change and why it failed. On success the replacements are applied
// atomically in a single pass over the original data.
func Apply(data []byte, changes []Change) ([]byte, error) {
	if len(changes) == 0 {
		return data, nil
	}
	positions := make([]int, len(changes))
	for index, change := range changes {
		if change.OldText == "" {
			return data, fmt.Errorf("edit %d: oldText is empty", index+1)
		}
		old := []byte(change.OldText)
		position := bytes.Index(data, old)
		if position < 0 {
			return data, &notFoundError{index: index + 1}
		}
		if bytes.Contains(data[position+len(old):], old) {
			return data, &notUniqueError{index: index + 1}
		}
		positions[index] = position
	}

	order := sortedOrder(positions)
	for i := 1; i < len(order); i++ {
		prev := order[i-1]
		cur := order[i]
		prevEnd := positions[prev] + len(changes[prev].OldText)
		if positions[cur] < prevEnd {
			return data, &overlapError{index: cur + 1}
		}
	}

	result := make([]byte, 0, len(data))
	cursor := 0
	for _, index := range order {
		position := positions[index]
		result = append(result, data[cursor:position]...)
		result = append(result, changes[index].NewText...)
		cursor = position + len(changes[index].OldText)
	}
	result = append(result, data[cursor:]...)
	return result, nil
}

// sortedOrder returns the change indexes ordered by their recorded position,
// with ties broken by earlier change order to keep diagnostics stable.
func sortedOrder(positions []int) []int {
	order := make([]int, len(positions))
	for index := range positions {
		order[index] = index
	}
	sort.SliceStable(order, func(a, b int) bool {
		pa, pb := positions[order[a]], positions[order[b]]
		if pa != pb {
			return pa < pb
		}
		return order[a] < order[b]
	})
	return order
}
