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
	Alias     string
	ConfigDir string
	Baresip   *gobaresip.Baresip
	Cmd       *exec.Cmd
	CtrlAddr  string
	SipPort   int
	RtpPorts  string
}

type BaresipManager struct {
	mu     sync.RWMutex
	agents map[string]*Agent
	master *WsHub

	BaseSipIP    string
	BaseSipPort  int
	BaseRtpPort  int
	MaxCalls     uint
	RtpNet       string
	RtpTimeout   uint
	UseAlsa      bool
	HasSipListen bool

	dataDir string
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
		BaseSipIP:    baseIP,
		BaseSipPort:  basePort,
		BaseRtpPort:  baseRtpPort,
		MaxCalls:     maxCalls,
		RtpNet:       rtpNet,
		RtpTimeout:   rtpTimeout,
		UseAlsa:      useAlsa,
		HasSipListen: hasSipListen,
		dataDir:      dataDir,
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
	sipPort := m.BaseSipPort + (len(m.agents) * 2)
	rtpStart := m.BaseRtpPort + (len(m.agents) * 102)
	rtpEnd := rtpStart + 100
	rtpPorts := fmt.Sprintf("%d-%d", rtpStart, rtpEnd)

	// 1. Proxy listener address (Master Hub connects here)
	lProxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	proxyAddr := lProxy.Addr().String()
	lProxy.Close()

	// 2. Baresip listener address (Internal Agent Proxy connects here)
	lBaresip, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	baresipAddr := lBaresip.Addr().String()
	lBaresip.Close()

	// Write config and accounts
	// We pass baresipAddr to CreateConfig so Baresip listens there
	globalRecordsDir, _ := filepath.Abs(filepath.Join(m.dataDir, "recorded_temp"))
	globalSoundsDir, _ := filepath.Abs(filepath.Join(m.dataDir, "sounds"))
	sipAddr := ""
	if m.HasSipListen {
		sipAddr = fmt.Sprintf("%s:%d", m.BaseSipIP, sipPort)
	}
	CreateConfig(agentDir, m.MaxCalls, m.RtpNet, rtpPorts, m.RtpTimeout, baresipAddr, sipAddr, m.UseAlsa, true, globalRecordsDir, globalSoundsDir)

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
		Alias:     alias,
		ConfigDir: agentDir,
		Baresip:   gb,
		Cmd:       cmd,
		CtrlAddr:  proxyAddr,
		SipPort:   sipPort,
		RtpPorts:  rtpPorts,
	}

	m.agents[alias] = agent

	// Forward agent messages to master hub
	go m.forwardMessages(agent)

	// Explicitly trigger UA registration now that the telemetry bridge is up
	if err := agent.Baresip.CmdWs([]byte("uanew " + accountLine)); err != nil {
		log.Printf("hub: failed to trigger registration for agent %s: %v", alias, err)
	}

	return nil
}

func (m *BaresipManager) forwardMessages(a *Agent) {
	msgChan := a.Baresip.GetMsgChan()
	for msg := range msgChan {
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
				if semi := strings.Index(prefix, ";"); semi != -1 {
					prefix = prefix[:semi]
				}
				if strings.EqualFold(a, prefix) {
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
