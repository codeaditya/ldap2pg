package lists

func And[T any](s []T, fn func(T) bool) bool {
	for _, i := range s {
		if !fn(i) {
			return false
		}
	}
	return true
}

// Filter items from a slice
//
// Keep items for which fn returns true.
func Filter[T any](s []T, fn func(T) bool) (out []T) {
	for _, i := range s {
		if fn(i) {
			out = append(out, i)
		}
	}
	return
}
