package telefonist

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	gobaresip "github.com/negbie/telefonist/pkg/gobaresip"
)

type Agent struct {
	Alias         string
	ConfigDir     string
	Baresip       *gobaresip.Baresip
	Cmd           *exec.Cmd
	CtrlAddr      string
	SIPPort       int
	RTPPorts      string
	Done          chan struct{} // Closed when Cmd exits
	RecordingsDir string
	RTPOffset     int
}

type BaresipManager struct {
	mu     sync.RWMutex
	agents map[string]*Agent
	master *WsHub

	baseSIPIP    string
	baseSIPPort  int
	baseRTPPort  int
	maxCalls     uint
	rtpNet       string
	rtpTimeout   uint
	useALSA      bool
	hasSIPListen bool

	dataDir string

	portMu   sync.Mutex
	sipPorts map[int]bool // offset from baseSIPPort -> used
	rtpPorts map[int]bool // offset from baseRTPPort -> used
}

func NewBaresipManager(hub *WsHub, dataDir string, maxCalls uint, rtpNet string, rtpPorts string, rtpTimeout uint, useAlsa bool, sipListen string) *BaresipManager {
	baseIP := "0.0.0.0"
	basePort := 5060
	baseRtpPort := 10000

	hasSipListen := sipListen != ""
	if sipListen != "" {
		if host, portStr, err := net.SplitHostPort(sipListen); err == nil {
			baseIP = host
			if p, err := strconv.Atoi(portStr); err == nil {
				basePort = p
			}
		} else {
			baseIP = sipListen
		}
	}

	if rtpPorts != "" {
		// Expecting "start-end" or just "start"
		parts := strings.Split(rtpPorts, "-")
		if p, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
			baseRtpPort = p
		}
	}

	return &BaresipManager{
		agents:       make(map[string]*Agent),
		master:       hub,
		baseSIPIP:    baseIP,
		baseSIPPort:  basePort,
		baseRTPPort:  baseRtpPort,
		maxCalls:     maxCalls,
		rtpNet:       rtpNet,
		rtpTimeout:   rtpTimeout,
		useALSA:      useAlsa,
		hasSIPListen: hasSipListen,
		dataDir:      dataDir,
		sipPorts:     make(map[int]bool),
		rtpPorts:     make(map[int]bool),
	}
}

func (m *BaresipManager) allocatePorts() (int, string, int) {
	m.portMu.Lock()
	defer m.portMu.Unlock()

	// Find free SIP port offset
	sipOffset := 0
	for ; m.sipPorts[sipOffset]; sipOffset += 2 {
	}
	m.sipPorts[sipOffset] = true
	sipPort := m.baseSIPPort + sipOffset

	// Find free RTP range offset
	rtpOffset := 0
	for ; m.rtpPorts[rtpOffset]; rtpOffset += 102 {
	}
	m.rtpPorts[rtpOffset] = true
	rtpStart := m.baseRTPPort + rtpOffset
	rtpEnd := rtpStart + 100
	rtpPortsStr := fmt.Sprintf("%d-%d", rtpStart, rtpEnd)

	return sipPort, rtpPortsStr, rtpOffset
}

func (m *BaresipManager) releasePorts(sipPort, rtpOffset int) {
	m.portMu.Lock()
	defer m.portMu.Unlock()

	sipOffset := sipPort - m.baseSIPPort
	delete(m.sipPorts, sipOffset)
	delete(m.rtpPorts, rtpOffset)
}

func (m *BaresipManager) SpawnAgent(ctx context.Context, alias string, accountLine string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !isSafeAlias(alias) {
		return fmt.Errorf("invalid agent alias %q: only alphanumeric, underscores, and dashes allowed", alias)
	}

	if _, ok := m.agents[alias]; ok {
		return fmt.Errorf("agent %s already exists", alias)
	}

	// Create unique data dir for agent
	agentDir := filepath.Join(m.dataDir, "agents", alias)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return err
	}

	// Allocate ports from the pool
	sipPort, rtpPorts, rtpOffset := m.allocatePorts()

	// 1. Proxy listener address (Master Hub connects here)
	lProxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	proxyAddr := lProxy.Addr().String()
	if err := lProxy.Close(); err != nil {
		log.Printf("hub: failed to close proxy listener for agent %s: %v", alias, err)
	}

	// 2. Baresip listener address (Internal Agent Proxy connects here)
	lBaresip, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	baresipAddr := lBaresip.Addr().String()
	if err := lBaresip.Close(); err != nil {
		log.Printf("hub: failed to close baresip listener for agent %s: %v", alias, err)
	}

	// Write config and accounts
	// We pass baresipAddr to CreateConfig so Baresip listens there
	agentRecordsDir, err := filepath.Abs(filepath.Join(agentDir, "recorded_temp"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(agentRecordsDir, 0755); err != nil {
		return err
	}
	globalSoundsDir, err := filepath.Abs(filepath.Join(m.dataDir, "sounds"))
	if err != nil {
		return err
	}
	sipAddr := ""
	if m.hasSIPListen {
		sipAddr = fmt.Sprintf("%s:%d", m.baseSIPIP, sipPort)
	}
	if err := CreateConfig(agentDir, m.maxCalls, m.rtpNet, rtpPorts, m.rtpTimeout, baresipAddr, sipAddr, m.useALSA, agentRecordsDir, globalSoundsDir); err != nil {
		return err
	}

	// Write empty accounts file to prevent initial registration before bridge is ready.
	// Baresip will load UAs via explicit uanew command later.
	accountsFile := filepath.Join(agentDir, "accounts")
	if err := os.WriteFile(accountsFile, []byte(""), 0644); err != nil {
		return err
	}

	// Start agent process
	// We pass both addresses to the agent:
	// -ctrl_address: where the proxy listens for the Master
	// -baresip_ctrl_address: where Baresip is listening locally
	self, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, self,
		"-data_dir", agentDir,
		"-sounds_dir", globalSoundsDir,
		"-ctrl_address", proxyAddr)
	cmd.Env = append(os.Environ(),
		"TELEFONIST_AGENT=1",
		"TELEFONIST_ALIAS="+alias,
		"TELEFONIST_BARESIP_CTRL="+baresipAddr,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return err
	}

	// Connect to agent's PROXY
	var gb *gobaresip.Baresip
	maxRetries := 30
	retryInterval := 20 * time.Millisecond
	for i := 0; i < maxRetries; i++ {
		gb, err = gobaresip.New(
			gobaresip.SetRemote(true),
			gobaresip.SetCtrlTCPAddr(proxyAddr),
			gobaresip.SetContext(ctx),
		)
		if err == nil {
			break
		}
		time.Sleep(retryInterval)
		if retryInterval < 100*time.Millisecond {
			retryInterval += 20 * time.Millisecond
		}
	}

	if err != nil {
		if killErr := cmd.Process.Kill(); killErr != nil {
			log.Printf("hub: failed to kill agent process %s: %v", alias, killErr)
		}
		return fmt.Errorf("failed to connect to agent %s: %w", alias, err)
	}

	agent := &Agent{
		Alias:         alias,
		ConfigDir:     agentDir,
		Baresip:       gb,
		Cmd:           cmd,
		CtrlAddr:      proxyAddr,
		SIPPort:       sipPort,
		RTPPorts:      rtpPorts,
		RTPOffset:     rtpOffset,
		Done:          make(chan struct{}),
		RecordingsDir: agentRecordsDir,
	}

	m.agents[alias] = agent

	// Monitor agent process exit
	go func() {
		err := cmd.Wait()
		if err != nil && !strings.Contains(err.Error(), "signal: killed") {
			log.Printf("hub: agent %s exited with error: %v", alias, err)
		} else {
			log.Printf("hub: agent %s exited", alias)
		}
		close(agent.Done)
		m.StopAgent(alias)
	}()

	// Forward agent messages to master hub
	go m.forwardMessages(m.master.ctx, agent)

	// Explicitly trigger UA registration now that the telemetry bridge is up
	if err := agent.Baresip.CmdWs([]byte("uanew " + accountLine)); err != nil {
		log.Printf("hub: failed to trigger registration for agent %s: %v", alias, err)
	}

	return nil
}

func (m *BaresipManager) forwardMessages(ctx context.Context, a *Agent) {
	msgChan := a.Baresip.GetMsgChan()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgChan:
			if !ok {
				return
			}
			m.master.ForwardAgentMsg(a.Alias, msg)
		}
	}
}

func (m *BaresipManager) stopAgent(a *Agent) {
	log.Printf("stopping agent %s", a.Alias)
	// Try a graceful shutdown first to allow SIP deregistrations
	if err := a.Baresip.CmdWs([]byte("quit")); err != nil {
		log.Printf("hub: failed to send quit to agent %s: %v", a.Alias, err)
	}

	// Wait for process to exit
	select {
	case <-a.Done:
		// Process exited naturally or via quit
	case <-time.After(2 * time.Second):
		// Process did not exit in time, force kill
		if a.Cmd.Process != nil {
			if err := a.Cmd.Process.Kill(); err != nil {
				log.Printf("hub: failed to kill agent process %s: %v", a.Alias, err)
			}
		}
		<-a.Done // Wait for monitor goroutine to finish Wait()
	}

	a.Baresip.Close()
	m.releasePorts(a.SIPPort, a.RTPOffset)
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

func (m *BaresipManager) ResolveTarget(cmd string, fallbackTarget string) (target string, finalCmd string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return fallbackTarget, cmd
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var bestAlias string
	var bestColonIdx int
	for a := range m.agents {
		for i := len(parts[0]) - 1; i >= 0; i-- {
			if parts[0][i] == ':' {
				prefix := parts[0][:i]
				if strings.EqualFold(a, ExtractAlias(prefix)) {
					if len(a) > len(bestAlias) {
						bestAlias = a
						bestColonIdx = i
					}
				}
			}
		}
	}

	if bestAlias != "" {
		target = bestAlias
		finalCmd = parts[0][bestColonIdx+1:] + " " + strings.Join(parts[1:], " ")
		return target, strings.TrimSpace(finalCmd)
	}

	return fallbackTarget, cmd
}

func isSafeAlias(alias string) bool {
	if alias == "" || strings.HasPrefix(alias, ".") || strings.Contains(alias, "..") ||
		strings.ContainsAny(alias, "/\\ \t\n\r") {
		return false
	}
	return true
}
