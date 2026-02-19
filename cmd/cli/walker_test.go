package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWalker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "walker_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create structure:
	// tmpDir/dir1/file1.txt
	// tmpDir/dir1/file2.txt
	// tmpDir/dir1/subdir/file3.txt (second-level dir)
	// tmpDir/dir2/file4.txt
	filesToCreate := []string{
		filepath.Join("dir1", "file1.txt"),
		filepath.Join("dir1", "file2.txt"),
		filepath.Join("dir1", "subdir", "file3.txt"),
		filepath.Join("dir2", "file4.txt"),
	}

	for _, f := range filesToCreate {
		path := filepath.Join(tmpDir, f)
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte("content"), 0644)
	}

	t.Run("MergeFiles=false", func(t *testing.T) {
		var groups [][]string
		w := &WalkerOption{
			RootPath:   tmpDir,
			MergeFiles: false,
			OnGroupFound: func(files []string) error {
				groups = append(groups, files)
				return nil
			},
		}

		if err := w.Walk(); err != nil {
			t.Fatalf("Walk failed: %v", err)
		}

		if len(groups) != 4 {
			t.Errorf("Expected 4 groups (one per file), got %d", len(groups))
		}
	})

	t.Run("MergeFiles=true", func(t *testing.T) {
		var groups [][]string
		w := &WalkerOption{
			RootPath:   tmpDir,
			MergeFiles: true,
			OnGroupFound: func(files []string) error {
				groups = append(groups, files)
				return nil
			},
		}

		if err := w.Walk(); err != nil {
			t.Fatalf("Walk failed: %v", err)
		}

		// Expected: 2 groups (dir1 and dir2). 
		// file3.txt in dir1/subdir should be merged into dir1 group.
		if len(groups) != 2 {
			t.Errorf("Expected 2 groups (merged by first-level dir), got %d", len(groups))
		}

		foundDir1Merged := false
		for _, g := range groups {
			if len(g) == 3 {
				foundDir1Merged = true
			}
		}
		if !foundDir1Merged {
			t.Error("Expected to find a merged group with 3 files for dir1 (including second-level dir file)")
		}
	})

	t.Run("Alphabetical Sorting", func(t *testing.T) {
		sortDir := filepath.Join(tmpDir, "sort_test")
		subDir := filepath.Join(sortDir, "a")
		os.MkdirAll(subDir, 0755)
		os.WriteFile(filepath.Join(subDir, "z.txt"), []byte("z"), 0644)
		os.WriteFile(filepath.Join(subDir, "a.txt"), []byte("a"), 0644)
		os.WriteFile(filepath.Join(subDir, "m.txt"), []byte("m"), 0644)

		w := &WalkerOption{
			RootPath:   sortDir,
			MergeFiles: true,
			OnGroupFound: func(files []string) error {
				if len(files) != 3 {
					t.Errorf("Expected 3 files, got %d", len(files))
				}
				if filepath.Base(files[0]) != "a.txt" || filepath.Base(files[2]) != "z.txt" {
					t.Errorf("Files not sorted alphabetically: %v", files)
				}
				return nil
			},
		}
		w.Walk()
	})

	t.Run("Non-existent Path", func(t *testing.T) {
		w := &WalkerOption{RootPath: filepath.Join(tmpDir, "does_not_exist")}
		if err := w.Walk(); err == nil {
			t.Error("Expected error for non-existent path, got nil")
		}
	})

	t.Run("Files in Root Directory", func(t *testing.T) {
		rootFile1 := filepath.Join(tmpDir, "root_only_1.txt")
		rootFile2 := filepath.Join(tmpDir, "root_only_2.txt")
		os.WriteFile(rootFile1, []byte("root1"), 0644)
		os.WriteFile(rootFile2, []byte("root2"), 0644)

		rootFileGroups := 0
		w := &WalkerOption{
			RootPath:   tmpDir,
			MergeFiles: true,
			OnGroupFound: func(files []string) error {
				if len(files) == 1 && strings.HasPrefix(filepath.Base(files[0]), "root_only_") {
					rootFileGroups++
				}
				return nil
			},
		}
		w.Walk()
		if rootFileGroups != 2 {
			t.Errorf("Expected root files to be in separate groups, found %d groups", rootFileGroups)
		}
	})

	t.Run("Deeply Nested Directories", func(t *testing.T) {
		deepPath := filepath.Join(tmpDir, "level1", "level2", "level3", "deep.txt")
		os.MkdirAll(filepath.Dir(deepPath), 0755)
		os.WriteFile(deepPath, []byte("deep"), 0644)

		w := &WalkerOption{
			RootPath:   tmpDir,
			MergeFiles: true,
			OnGroupFound: func(files []string) error {
				for _, f := range files {
					if filepath.Base(f) == "deep.txt" {
						rel, _ := filepath.Rel(tmpDir, f)
						if !strings.HasPrefix(filepath.ToSlash(rel), "level1/") {
							t.Errorf("Deep file should be grouped under level1, got %s", rel)
						}
					}
				}
				return nil
			},
		}
		w.Walk()
	})
}
