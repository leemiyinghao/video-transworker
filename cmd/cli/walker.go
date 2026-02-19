package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type WalkerOption struct {
	MergeFiles bool // if true, files will be merged into one task flow, otherwise each file will have its own task flow
	RootPath   string
	OnGroupFound func(files []string) error
}

func (w *WalkerOption) Walk() error {
	tasks := make(map[string][]string)

	err := filepath.Walk(w.RootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		key := path
		if w.MergeFiles {
			rel, _ := filepath.Rel(w.RootPath, path)
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) > 1 {
				key = filepath.Join(w.RootPath, parts[0])
			}
		}
		tasks[key] = append(tasks[key], path)
		return nil
	})

	if err != nil {
		return err
	}

	// Sort keys for deterministic group processing
	keys := make([]string, 0, len(tasks))
	for k := range tasks {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, group := range keys {
		files := tasks[group]
		sort.Strings(files) // Requirement: files sorted in alphabetical order

		if w.OnGroupFound != nil {
			if err := w.OnGroupFound(files); err != nil {
				return err
			}
		}
	}

	return nil
}
