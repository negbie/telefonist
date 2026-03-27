package telefonist

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	gobaresip "github.com/negbie/telefonist/pkg/gobaresip"
)

type Agent struct {
	Alias      string
	ConfigDir  string
	Baresip    *gobaresip.Baresip
	Cmd        *exec.Cmd
	CtrlAddr   string
	SipPort    int
	RtpPorts   string
}

type BaresipManager struct {
	mu     sync.RWMutex
	agents map[string]*Agent
	master *WsHub
	
	nextSipPort int
	nextRtpPort int

	dataDir string
}

func NewBaresipManager(hub *WsHub, dataDir string) *BaresipManager {
	return &BaresipManager{
		agents:      make(map[string]*Agent),
		master:      hub,
		nextSipPort: 5060,
		nextRtpPort: 10000,
		dataDir:     dataDir,
	}
}

func (m *BaresipManager) SpawnAgent(ctx context.Context, alias string, accountLine string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.agents[alias]; ok {
		return fmt.Errorf("agent %s already exists", alias)
	}

	// Create unique data dir for agent
	agentDir := filepath.Join(m.dataDir, "agents", alias)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return err
	}

	// Allocate ports
	sipPort := m.nextSipPort
	m.nextSipPort += 2
	rtpStart := m.nextRtpPort
	rtpEnd := rtpStart + 100
	m.nextRtpPort += 102
	rtpPorts := fmt.Sprintf("%d-%d", rtpStart, rtpEnd)

	// find a free control port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	ctrlAddr := l.Addr().String()
	l.Close()

	// Write config and accounts
	// We reuse CreateConfig but we might need to adjust it to be more flexible for agents
	globalRecordsDir, _ := filepath.Abs(filepath.Join(m.dataDir, "recorded_temp"))
	CreateConfig(agentDir, 10, "", rtpPorts, 10, ctrlAddr, fmt.Sprintf("0.0.0.0:%d", sipPort), false, true, globalRecordsDir)

	// Write accounts file
	// The accountLine is something like "<sip:test1@...>;..."
	accountsFile := filepath.Join(agentDir, "accounts")
	if err := os.WriteFile(accountsFile, []byte(accountLine+"\n"), 0644); err != nil {
		return err
	}

	// Start agent process
	self, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, self, "-agent", "-data_dir", agentDir, "-ctrl_address", ctrlAddr)
	cmd.Stdout = nil
	cmd.Stderr = nil
	
	if err := cmd.Start(); err != nil {
		return err
	}

	// Connect to agent
	var gb *gobaresip.Baresip
	maxRetries := 20
	for i := 0; i < maxRetries; i++ {
		time.Sleep(500 * time.Millisecond)
		log.Printf("hub: connecting to agent %s at %s (attempt %d)", alias, ctrlAddr, i+1)
		gb, err = gobaresip.New(
			gobaresip.SetRemote(true),
			gobaresip.SetCtrlTCPAddr(ctrlAddr),
			gobaresip.SetContext(ctx),
		)
		if err == nil {
			break
		}
	}

	if err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("failed to connect to agent %s: %v", alias, err)
	}

	agent := &Agent{
		Alias:     alias,
		ConfigDir: agentDir,
		Baresip:   gb,
		Cmd:       cmd,
		CtrlAddr:  ctrlAddr,
		SipPort:   sipPort,
		RtpPorts:  rtpPorts,
	}

	m.agents[alias] = agent

	// Forward agent messages to master hub
	go m.forwardMessages(agent)

	return nil
}

func (m *BaresipManager) forwardMessages(a *Agent) {
	msgChan := a.Baresip.GetMsgChan()
	for msg := range msgChan {
		// We could enrich the message with the agent alias here if needed
		// But for now, we just pass it to the master hub's message logic
		// Wait, WsHub.Run() currently reads from ONE channel.
		// We need to either make WsHub listen to multiple channels or use a central channel.
		m.master.ForwardAgentMsg(a.Alias, msg)
	}
}

func (m *BaresipManager) stopAgent(a *Agent) {
	log.Printf("stopping agent %s", a.Alias)
	// Try a graceful shutdown first to allow SIP deregistrations
	a.Baresip.CmdWs([]byte("quit"))

	// Wait for process to exit naturally
	done := make(chan error, 1)
	go func() {
		done <- a.Cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited naturally
	case <-time.After(2 * time.Second):
		// Process did not exit in time, force kill
		if a.Cmd.Process != nil {
			a.Cmd.Process.Kill()
		}
	}

	a.Baresip.Close()
	delete(m.agents, a.Alias)
}

func (m *BaresipManager) StopAgent(alias string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.agents[alias]; ok {
		m.stopAgent(a)
	}
}

func (m *BaresipManager) GetAgent(alias string) (*Agent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.agents[alias]
	return a, ok
}

func (m *BaresipManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.agents {
		m.stopAgent(a)
	}
}
