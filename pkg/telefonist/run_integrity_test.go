package telefonist

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"github.com/negbie/telefonist/pkg/gobaresip"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJUnitPreservesFailureErrorAndUnverifiedOutcomes(t *testing.T) {
	s := prepareTestStore(t)
	defer s.Close()
	ctx := context.Background()
	h := NewWsHub(t.TempDir(), 1, "", "10000-11000", 0, false, "")
	h.SetTestStore(s)
	for _, status := range []string{"PASS", "FAIL", "ERROR", "ABORTED", "UNVERIFIED", "SKIPPED"} {
		id, err := s.BeginRun(ctx, "case_"+status, "", "source", "batch", 1)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.FinishRun(ctx, id, status, "reason <&>", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	HandleBatchJUnit(h)(w, httptest.NewRequest("GET", "/api/batch/junit?id=batch", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var suite junitSuite
	if err := xml.Unmarshal(w.Body.Bytes(), &suite); err != nil {
		t.Fatal(err)
	}
	if suite.Tests != 6 || suite.Failures != 1 || suite.Errors != 2 || suite.Skipped != 2 {
		t.Fatalf("wrong CI verdicts: %+v", suite)
	}
}

func TestRunnerPersistsCancellationAndTelemetryLoss(t *testing.T) {
	for _, kind := range []string{"cancel", "TELEMETRY_LOSS"} {
		t.Run(kind, func(t *testing.T) {
			s := prepareTestStore(t)
			defer s.Close()
			h := NewWsHub(t.TempDir(), 2, "", "10000-11000", 0, false, "")
			h.SetTestStore(s)
			go h.Run()
			defer func() { h.Stop(); <-h.done }()
			if !h.runner.Start([]TestfileData{{Name: "delayed", Content: "30s"}}) {
				t.Fatal("run rejected")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			for {
				var ready bool
				if err := h.Do(ctx, func() { ready = h.trainSession != nil }); err != nil {
					t.Fatal(err)
				}
				if ready {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(time.Millisecond):
				}
			}
			// Send a non-filtered event to verify evidence is persisted.
			raw := []byte(`{"event":true,"type":"CALL_ESTABLISHED","param":"active"}`)
			h.ForwardAgentMsg("alice", gobaresip.Msg{Event: &gobaresip.EventMsg{Type: "CALL_ESTABLISHED", RawJSON: raw}})
			if err := h.DrainContext(ctx); err != nil {
				t.Fatal(err)
			}
			if kind == "cancel" {
				h.runner.Stop()
			} else {
				raw = []byte(`{"event":true,"type":"TELEMETRY_LOSS","param":"queue overflow"}`)
				h.ForwardAgentMsg("alice", gobaresip.Msg{Event: &gobaresip.EventMsg{Type: kind, RawJSON: raw}})
			}
			h.runner.mu.Lock()
			done := h.runner.done
			h.runner.mu.Unlock()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("runner did not finish", ctx.Err())
			}
			rows, err := s.ListRuns(ctx, "delayed", "")
			if err != nil || len(rows) != 1 {
				t.Fatalf("missing run: %v %v", rows, err)
			}
			run, err := s.GetRun(ctx, rows[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "ABORTED"
			if kind != "cancel" {
				want = "ERROR"
			}
			if run.Status != want || run.FailureReason == "" {
				t.Fatalf("incorrect verdict: %+v", run)
			}
			var event map[string]any
			if err = json.Unmarshal([]byte(strings.Split(run.FlowEvents, "\n\n")[0]), &event); err != nil {
				t.Fatal(err, run.FlowEvents)
			}
			if event["type"] != "CALL_ESTABLISHED" || event["timestamp"] == nil || event["run_id"] == nil {
				t.Fatalf("lost normalized evidence event: %+v", event)
			}
		})
	}
}

func TestStaleSaveDoesNotChangeContentOrHistory(t *testing.T) {
	s := prepareTestStore(t)
	defer s.Close()
	ctx := context.Background()
	if err := s.Save(ctx, "call", "", "first"); err != nil {
		t.Fatal(err)
	}
	row, err := s.Load(ctx, "call", "")
	if err != nil {
		t.Fatal(err)
	}
	revision := row.UpdatedAt.Format(time.RFC3339Nano)
	if err = s.SaveExpected(ctx, "call", "", "second", &revision); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveExpected(ctx, "call", "", "stale third", &revision); !errors.Is(err, ErrSaveConflict) {
		t.Fatalf("stale write: %v", err)
	}
	row, err = s.Load(ctx, "call", "")
	if err != nil || row.Content != "second" {
		t.Fatal("stale write overwrote content", err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM testfile_versions WHERE content='stale third'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed save leaked into history", err)
	}
}

func TestRunArtifactLifecycle(t *testing.T) {
	s := prepareTestStore(t)
	defer s.Close()
	ctx := context.Background()
	if err := s.Save(ctx, "call", "", "saved version"); err != nil {
		t.Fatal(err)
	}
	id, err := s.BeginRun(ctx, "call", "", "unsaved execution", "batch", 1)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Load(ctx, "call", "")
	if err != nil || saved.Content != "saved version" {
		t.Fatalf("run overwrote saved editor: %+v %v", saved, err)
	}
	src := filepath.Join(t.TempDir(), "audio.wav")
	want := []byte("recording evidence")
	if err = os.WriteFile(src, want, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.MoveWav(ctx, id, "audio.wav", src); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("successful transfer retained source: %v", err)
	}
	wavs, err := s.ListWavs(ctx, id)
	if err != nil || len(wavs) != 1 {
		t.Fatalf("metadata: %v %v", wavs, err)
	}
	if err = s.Rename(ctx, "call", "", "renamed", ""); err != nil {
		t.Fatal(err)
	}
	_, path, err := s.GetWavPath(ctx, wavs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(want) {
		t.Fatalf("rename lost audio: %q %v", got, err)
	}
	if err = s.FinishRun(ctx, id, "ABORTED", "user stopped", "", "{}", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, "renamed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("delete leaked recording: %v", err)
	}
}

func TestFailedArtifactMetadataPreservesSource(t *testing.T) {
	s := prepareTestStore(t)
	defer s.Close()
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "source.wav")
	os.WriteFile(src, []byte("irreplaceable"), 0600)
	if err := s.MoveWavWithLocation(ctx, 999, "missing", "", "audio.wav", src); err == nil {
		t.Fatal("expected foreign key failure")
	}
	got, err := os.ReadFile(src)
	if err != nil || string(got) != "irreplaceable" {
		t.Fatalf("lost source: %q %v", got, err)
	}
	for _, path := range []string{"../outside", "/outside"} {
		if _, err = s.artifactPath(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}

func TestRetentionKeepsOtherTestsAndActiveRuns(t *testing.T) {
	s := prepareTestStore(t)
	defer s.Close()
	ctx := context.Background()
	var last int64
	for i := 0; i < 4; i++ {
		id, err := s.BeginRun(ctx, "busy", "", "dial 1", "batch", i)
		if err != nil {
			t.Fatal(err)
		}
		last = id
		if err = s.FinishRun(ctx, id, "PASS", "", "hash", "{}", ""); err != nil {
			t.Fatal(err)
		}
	}
	active, err := s.BeginRun(ctx, "busy", "", "dial 1", "batch", 5)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.BeginRun(ctx, "quiet", "", "dial 2", "batch", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishRun(ctx, other, "FAIL", "assertion", "", "{}", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.PruneRuns(ctx, 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{last, active, other} {
		if _, err = s.GetRun(ctx, int(id)); err != nil {
			t.Fatalf("retention removed protected run %d: %v", id, err)
		}
	}
	rows, err := s.ListRuns(ctx, "busy", "")
	if err != nil || len(rows) != 2 {
		t.Fatalf("retention: %d runs %v", len(rows), err)
	}
}

func TestSyntheticPcapIPv6AndInvalidAddresses(t *testing.T) {
	events := []map[string]interface{}{{"type": "SIP", "param": "RX [2001:db8::1]:5060 -> [2001:db8::2]:5061\nSIP/2.0 200 OK\n", "timestamp": float64(1234567890123)}}
	pcap := generatePcap(events)
	if len(pcap) < 102 || pcap[52] != 0x86 || pcap[53] != 0xdd || pcap[54] != 0x60 {
		t.Fatalf("invalid IPv6 packet %x", pcap)
	}
	if got := binary.LittleEndian.Uint32(pcap[24:28]); got != 1234567890 {
		t.Fatalf("lost event timestamp: %d", got)
	}
	if got := generatePcap([]map[string]interface{}{{"type": "SIP", "param": "invalid address\nSIP/2.0 200 OK"}}); len(got) != 24 {
		t.Fatal("invented addresses for malformed trace")
	}
}

func TestDeleteRunsPreservesRunningTests(t *testing.T) {
	s := prepareTestStore(t)
	defer s.Close()
	ctx := context.Background()

	finishedID, err := s.BeginRun(ctx, "testfileA", "proj", "dial 1", "batch", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishRun(ctx, finishedID, "PASS", "", "hash1", "{}", ""); err != nil {
		t.Fatal(err)
	}
	runningID, err := s.BeginRun(ctx, "testfileA", "proj", "dial 2", "batch", 2)
	if err != nil {
		t.Fatal(err)
	}

	if err = s.DeleteRunsByTestfile(ctx, "testfileA", "proj"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetRun(ctx, int(finishedID)); err == nil {
		t.Fatalf("expected finished run %d to be deleted", finishedID)
	}
	if _, err = s.GetRun(ctx, int(runningID)); err != nil {
		t.Fatalf("running run %d was unexpectedly deleted: %v", runningID, err)
	}

	if err = s.DeleteRun(ctx, int(runningID)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetRun(ctx, int(runningID)); err != nil {
		t.Fatalf("running run %d was unexpectedly deleted by DeleteRun: %v", runningID, err)
	}

	if err = s.Delete(ctx, "testfileA", "proj"); err == nil {
		t.Fatal("expected Delete testfile to fail while test is running")
	}
}

