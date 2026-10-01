package telefonist
 
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActiveRunnerIdentityGuard(t *testing.T) {
	ctx := context.Background()
	store := prepareTestStore(t)
	defer store.Close()

	if err := store.SaveProject(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "test1", "alpha", "dial 100"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "test2", "alpha", "dial 200"); err != nil {
		t.Fatal(err)
	}

	versions, err := store.ListVersions(ctx, "test1", "alpha")
	if err != nil || len(versions) == 0 {
		t.Fatalf("expected versions for test1, got err: %v, len: %d", err, len(versions))
	}
	vID := versions[0].ID

	hub := &WsHub{
		testStore: store,
	}
	hub.runner = newRunner(hub)
	// Simulate an active test run
	hub.runner.state.Active = true
	hub.runner.state.File = "test1"
	hub.runner.state.Project = "alpha"

	// 1. Saving an unrelated testfile (or any testfile) while a test is active should SUCCEED.
	t.Run("Save testfile allowed while test active", func(t *testing.T) {
		handler := HandleAPITestfile(hub)
		body := `{"name":"test2","project":"alpha","content_b64":"` + base64.StdEncoding.EncodeToString([]byte("dial 201")) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/testfile", strings.NewReader(body))
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for saving testfile, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 2. Deleting a git history version entry while a test is active should SUCCEED.
	t.Run("Delete version allowed while test active", func(t *testing.T) {
		handler := HandleAPITestfileVersions(hub)
		req := httptest.NewRequest(http.MethodDelete, "/api/testfile/versions?id="+strings.TrimSpace(string(rune('0'+vID))), nil)
		req = httptest.NewRequest(http.MethodDelete, "/api/testfile/versions?id="+json.Number(string(rune('0'+vID))).String(), nil)
		req = httptest.NewRequest(http.MethodDelete, "/api/testfile/versions?id="+formatInt(vID), nil)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for deleting version, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 3. Deleting testfile while test is active should be REJECTED with JSON 409 Conflict.
	t.Run("Delete testfile blocked with JSON error while test active", func(t *testing.T) {
		handler := HandleAPITestfile(hub)
		req := httptest.NewRequest(http.MethodDelete, "/api/testfile?name=test1&project=alpha", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict for deleting testfile, got %d: %s", w.Code, w.Body.String())
		}
		var resp apiResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid JSON error response, got error: %v, body: %s", err, w.Body.String())
		}
		if resp.Status != "error" || !strings.Contains(resp.Message, "stop the active run") {
			t.Fatalf("unexpected JSON response: %+v", resp)
		}
	})

	// 4. Renaming testfile while test is active should be REJECTED with JSON 409 Conflict.
	t.Run("Rename testfile blocked with JSON error while test active", func(t *testing.T) {
		handler := HandleAPITestfileRename(hub)
		body := `{"old_name":"test1","old_project":"alpha","new_name":"test1_renamed","new_project":"alpha"}`
		req := httptest.NewRequest(http.MethodPost, "/api/testfile/rename", strings.NewReader(body))
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict for renaming testfile, got %d: %s", w.Code, w.Body.String())
		}
		var resp apiResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid JSON error response, got error: %v, body: %s", err, w.Body.String())
		}
		if resp.Status != "error" {
			t.Fatalf("unexpected JSON response: %+v", resp)
		}
	})

	// 5. Deleting project while test is active should be REJECTED with JSON 409 Conflict.
	t.Run("Delete project blocked with JSON error while test active", func(t *testing.T) {
		handler := HandleAPIProjects(hub)
		req := httptest.NewRequest(http.MethodDelete, "/api/projects?name=alpha", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict for deleting project, got %d: %s", w.Code, w.Body.String())
		}
		var resp apiResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid JSON error response, got error: %v, body: %s", err, w.Body.String())
		}
		if resp.Status != "error" {
			t.Fatalf("unexpected JSON response: %+v", resp)
		}
	})

	// 6. Renaming project while test is active should be REJECTED with JSON 409 Conflict.
	t.Run("Rename project blocked with JSON error while test active", func(t *testing.T) {
		handler := HandleAPIProjectRename(hub)
		body := `{"old_name":"alpha","new_name":"beta"}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/rename", strings.NewReader(body))
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict for renaming project, got %d: %s", w.Code, w.Body.String())
		}
		var resp apiResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid JSON error response, got error: %v, body: %s", err, w.Body.String())
		}
		if resp.Status != "error" {
			t.Fatalf("unexpected JSON response: %+v", resp)
		}
	})
}

func formatInt(n int64) string {
	return strconvFormatInt(n)
}

func strconvFormatInt(n int64) string {
	var b [32]byte
	i := len(b) - 1
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n >= 10 {
		q := n / 10
		b[i] = byte('0' + n - q*10)
		n = q
		i--
	}
	b[i] = byte('0' + n)
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
