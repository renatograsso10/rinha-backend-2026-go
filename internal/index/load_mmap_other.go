//go:build !linux

package index

func tryLoadMMap(path string) (*Index, bool, error) {
	return nil, false, nil
}
