package tree

import (
	"iter"
)

func PreOrder[T any](roots iter.Seq[T], getChildren func(T) iter.Seq[T]) iter.Seq[T] {
	return func(yield func(T) bool) {
		var walk func(T) bool
		walk = func(n T) bool {
			if !yield(n) {
				return false
			}
			for child := range getChildren(n) {
				if !walk(child) {
					return false
				}
			}
			return true
		}

		for root := range roots {
			if !walk(root) {
				return
			}
		}
	}
}
