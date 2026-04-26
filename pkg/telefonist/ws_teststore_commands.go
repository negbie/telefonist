package telefonist

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func handleTestfilesCommand(h *WsHub, input string) {
	args := strings.TrimSpace(strings.TrimPrefix(input, "testfiles"))
	if args == "" {
		broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfiles", "message": "usage: testfiles run ..."}))
		return
	}
	if h == nil || h.testStore == nil {
		broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfiles", "message": "persistent test store is not enabled"}))
		return
	}

	cmd, rest, _ := strings.Cut(args, " ")
	if cmd == "run" {
		handleTestfilesRun(h, rest)
		return
	}
	broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfiles", "message": "subcommand " + cmd + " moved to REST API"}))
}

func handleTestfilesRun(h *WsHub, rest string) {
	arg := strings.TrimSpace(rest)
	if arg == "" {
		broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfiles", "message": "usage: testfiles run <all | name1 name2 ...>"}))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var batch []TestfileData
	if arg == "all" {
		rows, err := h.testStore.List(ctx, true)
		if err != nil {
			broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfiles", "message": "failed to list testfiles: " + err.Error()}))
			return
		}
		for _, r := range rows {
			batch = append(batch, TestfileData{Name: r.Name, ProjectName: r.ProjectName, Content: r.Content})
		}
	} else {
		args := strings.Fields(arg)
		if len(args) < 2 || len(args)%2 != 0 {
			broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfiles", "message": "usage: testfiles run <project1|''> <name1> ..."}))
			return
		}
		for i := 0; i < len(args); i += 2 {
			projectName := strings.ReplaceAll(args[i], "''", "")
			name := args[i+1]
			r, err := h.testStore.Load(ctx, name, projectName)
			if err != nil {
				broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfiles", "message": fmt.Sprintf("failed to load %q (project %q): %v", name, projectName, err)}))
				return
			}
			batch = append(batch, TestfileData{Name: r.Name, ProjectName: r.ProjectName, Content: r.Content})
		}
	}

	if len(batch) == 0 {
		broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfiles", "message": "no testfiles found to run"}))
		return
	}
	runTestfilesBatch(h, batch)
}
