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

func TestStore_SIPAccounts(t *testing.T) {
	ctx := context.Background()
	s := prepareTestStore(t)
	defer s.Close()

	// 1. List empty
	accounts, err := s.ListSIPAccounts(ctx)
	if err != nil {
		t.Fatalf("ListSIPAccounts failed: %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("Expected 0 accounts, got %d", len(accounts))
	}

	// 2. Save new account
	err = s.SaveSIPAccount(ctx, "", "alice", "sip:alice@sip.domain.com", "alicepassword", ";transport=tls", ";mediaenc=srtp-mand")
	if err != nil {
		t.Fatalf("SaveSIPAccount failed: %v", err)
	}

	// 3. Save another account (numeric username)
	err = s.SaveSIPAccount(ctx, "", "ua1", "sip:+123456@sip.domain.com", "trunkpassword", ";transport=tls", "")
	if err != nil {
		t.Fatalf("SaveSIPAccount failed: %v", err)
	}

	// 3b. Verify duplicate name check on creation
	err = s.SaveSIPAccount(ctx, "", "alice", "sip:alice-duplicate@sip.domain.com", "otherpass", "", "")
	if err == nil {
		t.Errorf("Expected error when saving duplicate account name, got nil")
	}

	// 4. List and check values
	accounts, err = s.ListSIPAccounts(ctx)
	if err != nil {
		t.Fatalf("ListSIPAccounts failed: %v", err)
	}
	if len(accounts) != 2 {
		t.Errorf("Expected 2 accounts, got %d", len(accounts))
	}

	foundAlice := false
	foundUA1 := false
	for _, a := range accounts {
		if a.Name == "alice" {
			foundAlice = true
			if a.SIPURI != "sip:alice@sip.domain.com" || a.Password != "alicepassword" || a.URIParams != ";transport=tls" || a.AddrParams != ";mediaenc=srtp-mand" {
				t.Errorf("Alice data mismatch: %+v", a)
			}
		}
		if a.Name == "ua1" {
			foundUA1 = true
			if a.SIPURI != "sip:+123456@sip.domain.com" || a.Password != "trunkpassword" || a.URIParams != ";transport=tls" || a.AddrParams != "" {
				t.Errorf("UA1 data mismatch: %+v", a)
			}
		}
	}
	if !foundAlice || !foundUA1 {
		t.Errorf("Did not find both expected accounts")
	}

	// 5. Update existing account (and test renaming alias)
	err = s.SaveSIPAccount(ctx, "alice", "alice-newname", "sip:alice-new@sip.domain.com", "newpassword", ";transport=tcp", ";mediaenc=none")
	if err != nil {
		t.Fatalf("Update SIPAccount failed: %v", err)
	}

	// Verify update
	accounts, err = s.ListSIPAccounts(ctx)
	if err != nil {
		t.Fatalf("ListSIPAccounts failed: %v", err)
	}
	for _, a := range accounts {
		if a.Name == "alice-newname" {
			if a.SIPURI != "sip:alice-new@sip.domain.com" || a.Password != "newpassword" || a.URIParams != ";transport=tcp" || a.AddrParams != ";mediaenc=none" {
				t.Errorf("Alice data not updated correctly: %+v", a)
			}
		}
	}

	// 5b. Verify clearing parameters
	err = s.SaveSIPAccount(ctx, "alice-newname", "alice-newname", "sip:alice-new@sip.domain.com", "newpassword", "", "")
	if err != nil {
		t.Fatalf("Clearing parameters failed: %v", err)
	}
	accounts, err = s.ListSIPAccounts(ctx)
	if err != nil {
		t.Fatalf("ListSIPAccounts failed: %v", err)
	}
	for _, a := range accounts {
		if a.Name == "alice-newname" {
			if a.URIParams != "" || a.AddrParams != "" {
				t.Errorf("Expected cleared parameters, got uri_params=%q addr_params=%q", a.URIParams, a.AddrParams)
			}
		}
	}

	// 6. Delete account
	err = s.DeleteSIPAccount(ctx, "ua1")
	if err != nil {
		t.Fatalf("DeleteSIPAccount failed: %v", err)
	}

	// Verify delete
	accounts, err = s.ListSIPAccounts(ctx)
	if err != nil {
		t.Fatalf("ListSIPAccounts failed: %v", err)
	}
	if len(accounts) != 1 {
		t.Errorf("Expected 1 account, got %d", len(accounts))
	}
	if accounts[0].Name != "alice-newname" {
		t.Errorf("Expected only alice-newname left, got %q", accounts[0].Name)
	}
}

func TestStore_VersionControl(t *testing.T) {
	s := prepareTestStore(t)
	defer s.Close()

	ctx := context.Background()
	testName := "versioned_test"
	projectName := "projectV"

	// 1. Save first version
	err := s.Save(ctx, testName, projectName, "first version content")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 2. Save same content (should NOT create a duplicate version)
	err = s.Save(ctx, testName, projectName, "first version content")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 3. Save second version with different content
	err = s.Save(ctx, testName, projectName, "second version content")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 4. List versions and verify
	versions, err := s.ListVersions(ctx, testName, projectName)
	if err != nil {
		t.Fatalf("ListVersions failed: %v", err)
	}

	if len(versions) != 2 {
		t.Fatalf("Expected exactly 2 versions in history, got %d", len(versions))
	}

	// The newest version (second version) should be first (descending order)
	if versions[0].Content != "second version content" {
		t.Errorf("Expected first element in list to be 'second version content', got %q", versions[0].Content)
	}
	if versions[1].Content != "first version content" {
		t.Errorf("Expected second element in list to be 'first version content', got %q", versions[1].Content)
	}
}
