package telefonist

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const logFileName = "telefonist.log"

func SetupLogging(dataDir string) (*os.File, error) {
	if dataDir == "" {
		dataDir = "."
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	logPath := filepath.Join(dataDir, logFileName)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", logPath, err)
	}

	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.LUTC | log.Lshortfile)
	log.Printf("logging initialized: %s", logPath)

	return f, nil
}

func CloseLogging(f *os.File) {
	if f == nil {
		return
	}
	if err := f.Close(); err != nil {
		log.SetOutput(io.Discard)
		return
	}
	log.SetOutput(io.Discard)
}
