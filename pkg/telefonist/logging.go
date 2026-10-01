package telefonist

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const logFileName = "telefonist.log"

func SetupLogging(dataDir string) (*os.File, error) {
	if dataDir == "" {
		dataDir = "."
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	logPath := filepath.Join(dataDir, logFileName)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", logPath, err)
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}

	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	log.Printf("logging initialized: %s", logPath)

	return f, nil
}

