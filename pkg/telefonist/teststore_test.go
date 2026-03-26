package telefonist

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func prepareTestStore(t *testing.T) *TestStore {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "teststore_test_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	wavDir := filepath.Join(tmpDir, "recorded_wavs")
	if err := migrateTestStore(ctx, db); err != nil {
		t.Fatal(err)
	}

	return &TestStore{db: db, dbPath: dbPath, wavDir: wavDir}
}

func TestStore_DataIntegrity(t *testing.T) {
	ctx := context.Background()
	s := prepareTestStore(t)
	defer s.Close()

	testName := "testfile1"
	projectName := "project1"
	content := "some content"

	// 1. Save a project
	if err := s.SaveProject(ctx, projectName); err != nil {
		t.Fatalf("SaveProject failed: %v", err)
	}

	// 2. Save a test file with project
	if err := s.Save(ctx, testName, projectName, content); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 3. Save some test runs for it
	for i := 1; i <= 3; i++ {
		if _, err := s.SaveRun(ctx, testName, projectName, i, "hash", "pass", "{}"); err != nil {
			t.Fatalf("SaveRun %d failed: %v", i, err)
		}
	}

	// Verify runs exist
	runs, err := s.ListRuns(ctx, testName, projectName)
	if err != nil || len(runs) != 3 {
		t.Fatalf("Expected 3 runs, got %d (err: %v)", len(runs), err)
	}

	t.Run("Rename Integrity", func(t *testing.T) {
		newName := "renamed_testfile"
		newProject := projectName // Keep project the same for this subtest
		if err := s.Rename(ctx, testName, projectName, newName, newProject); err != nil {
			t.Fatalf("Rename failed: %v", err)
		}

		// Verify runs are renamed
		runs, err := s.ListRuns(ctx, newName, newProject)
		if err != nil || len(runs) != 3 {
			t.Errorf("Expected 3 runs for new name, got %d (err: %v)", len(runs), err)
		}
		for _, r := range runs {
			if r.TestfileName != newName || r.ProjectName != newProject {
				t.Errorf("Expected run testfile_name %q project %q, got %q project %q", newName, newProject, r.TestfileName, r.ProjectName)
			}
		}

		// Verify no runs for old name
		oldRuns, err := s.ListRuns(ctx, testName, projectName)
		if err == nil && len(oldRuns) != 0 {
			t.Errorf("Expected 0 runs for old name, got %d", len(oldRuns))
		}

		testName = newName // Use new name for next test
	})

	t.Run("Delete Integrity", func(t *testing.T) {
		if err := s.Delete(ctx, testName, projectName); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Verify runs are deleted
		runs, err := s.ListRuns(ctx, testName, projectName)
		if err == nil && len(runs) != 0 {
			t.Errorf("Expected 0 runs after delete, got %d", len(runs))
		}

		// Also check ListAllRuns to be sure they are gone from the table
		allRuns, err := s.ListAllRuns(ctx)
		if err != nil {
			t.Fatalf("ListAllRuns failed: %v", err)
		}
		for _, r := range allRuns {
			if r.TestfileName == testName && r.ProjectName == projectName {
				t.Errorf("Found orphaned run for %q project %q after delete", testName, projectName)
			}
		}
	})
}

func TestStore_Projects(t *testing.T) {
	ctx := context.Background()
	s := prepareTestStore(t)
	defer s.Close()

	// 1. List empty projects
	projects, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}

	// 2. Save projects
	if err := s.SaveProject(ctx, "P1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProject(ctx, "P2"); err != nil {
		t.Fatal(err)
	}

	projects, err = s.ListProjects(ctx)
	if err != nil || len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d (err: %v)", len(projects), err)
	}

	// 3. Delete project and check testfile re-assignment
	if err := s.Save(ctx, "T1", "P1", "content"); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteProject(ctx, "P1"); err != nil {
		t.Fatal(err)
	}

	tf, err := s.Load(ctx, "T1", "")
	if err != nil {
		t.Fatal(err)
	}
	if tf.ProjectName != "" {
		t.Errorf("expected empty project name after P1 delete, got %q", tf.ProjectName)
	}
}

func TestStore_ScopedNames(t *testing.T) {
	ctx := context.Background()
	s := prepareTestStore(t)
	defer s.Close()

	// 1. Save same testfile name in different projects
	if err := s.Save(ctx, "smoke", "A", "content A"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, "smoke", "B", "content B"); err != nil {
		t.Fatal(err)
	}

	// 2. Verify both exist and have correct content
	tfA, err := s.Load(ctx, "smoke", "A")
	if err != nil || tfA.Content != "content A" {
		t.Errorf("Failed to load smoke A or wrong content: %v", err)
	}
	tfB, err := s.Load(ctx, "smoke", "B")
	if err != nil || tfB.Content != "content B" {
		t.Errorf("Failed to load smoke B or wrong content: %v", err)
	}

	// 3. List should show both
	list, err := s.List(ctx, true)
	if err != nil || len(list) != 2 {
		t.Errorf("List should have 2 items, got %d (err: %v)", len(list), err)
	}

	// 4. Rename scoped
	if err := s.Rename(ctx, "smoke", "A", "smoke_v2", "A"); err != nil {
		t.Fatal(err)
	}
	list, err = s.List(ctx, false)
	if err != nil || len(list) != 2 {
		t.Errorf("List after rename should still have 2 items, got %d", len(list))
	}

	// Verify B still has "smoke"
	if _, err := s.Load(ctx, "smoke", "B"); err != nil {
		t.Error("smoke B should still exist")
	}
}
func TestStore_RenameCycle(t *testing.T) {
	ctx := context.Background()
	s := prepareTestStore(t)
	defer s.Close()

	testName := "cyclic"
	projectName := ""

	s.SaveProject(ctx, projectName)
	s.Save(ctx, testName, projectName, "content")
	_, _ = s.SaveRun(ctx, testName, projectName, 1, "h1", "pass", "{}")

	// 1. Rename once
	if err := s.Rename(ctx, testName, projectName, "cyclic_new", projectName); err != nil {
		t.Fatal(err)
	}
	runs, _ := s.ListRuns(ctx, "cyclic_new", projectName)
	if len(runs) != 1 {
		t.Errorf("Expected 1 run after first rename, got %d", len(runs))
	}

	// 2. Rename back
	if err := s.Rename(ctx, "cyclic_new", projectName, testName, projectName); err != nil {
		t.Fatal(err)
	}
	runs, _ = s.ListRuns(ctx, testName, projectName)
	if len(runs) != 1 {
		t.Errorf("Expected 1 run after renaming back, got %d", len(runs))
	}
}

func TestStore_Security(t *testing.T) {
	ctx := context.Background()
	s := prepareTestStore(t)
	defer s.Close()

	// 1. SQL injection attempt in project name
	err := s.SaveProject(ctx, "project'; DROP TABLE projects; --")
	if err == nil {
		t.Error("FAIL: SQL injection attempt in SaveProject accepted!")
	}

	// 2. Invalid characters in testfile name
	err = s.Save(ctx, "test file!", "", "content")
	if err == nil {
		t.Error("FAIL: Invalid testfile name 'test file!' accepted!")
	}

	// 3. Valid characters but too long
	longName := "this_is_a_very_long_name_that_exceeds_sixty_four_characters_limit_12345678"
	err = s.Save(ctx, longName, "", "content")
	if err == nil {
		t.Error("FAIL: Overly long name accepted!")
	}

	// 4. Empty names (where not allowed)
	err = s.Save(ctx, "", "proj", "content")
	if err == nil {
		t.Error("FAIL: Empty testfile name accepted!")
	}

	err = s.SaveProject(ctx, "  ")
	if err == nil {
		t.Error("FAIL: Empty project name accepted in SaveProject!")
	}
}
