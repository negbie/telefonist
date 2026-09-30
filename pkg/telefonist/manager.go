package telefonist

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	RegChan       chan error
	ForwardDone   chan struct{}
	SocketDir     string
	stopping      atomic.Bool
	nativeDone    atomic.Bool
}

type BaresipManager struct {
	mu         sync.RWMutex
	spawnMu    sync.Mutex
	stoppingWG sync.WaitGroup
	runID      int64
	maxRTPPort int
	configErr  error
	agents     map[string]*Agent
	master     *WsHub

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
	maxRtpPort := 11000
	var configErr error

	hasSipListen := sipListen != ""
	if sipListen != "" {
		if host, portStr, err := net.SplitHostPort(sipListen); err == nil {
			baseIP = host
			if p, err := strconv.Atoi(portStr); err == nil {
				basePort = p
			} else {
				configErr = fmt.Errorf("invalid SIP listen port: %w", err)
			}
		} else {
			configErr = fmt.Errorf("invalid SIP listen address: %w", err)
		}
	}

	if rtpPorts != "" {
		// Expecting "start-end" or just "start"
		parts := strings.Split(rtpPorts, "-")
		if p, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
			baseRtpPort = p
		} else {
			configErr = err
		}
		if len(parts) == 2 {
			p, err := strconv.Atoi(parts[1])
			if err != nil {
				configErr = err
			} else {
				maxRtpPort = p
			}
		} else {
			maxRtpPort = baseRtpPort + 100
		}
	}
	if baseRtpPort < 1024 || maxRtpPort > 65535 || maxRtpPort < baseRtpPort+100 || basePort < 1 || basePort > 65534 {
		configErr = fmt.Errorf("invalid SIP/RTP port range")
	}

	return &BaresipManager{
		agents:      make(map[string]*Agent),
		master:      hub,
		baseSIPIP:   baseIP,
		baseSIPPort: basePort,
		baseRTPPort: baseRtpPort,
		maxRTPPort:  maxRtpPort, configErr: configErr,
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

func (m *BaresipManager) SetRun(id int64) { m.mu.Lock(); m.runID = id; m.mu.Unlock() }

func (m *BaresipManager) allocatePorts() (int, string, int, error) {
	m.portMu.Lock()
	defer m.portMu.Unlock()

	// Find free SIP port offset
	sipOffset := 0
	for ; m.sipPorts[sipOffset]; sipOffset += 2 {
	}
	if m.configErr != nil {
		return 0, "", 0, m.configErr
	}
	sipPort := m.baseSIPPort + sipOffset

	// Find free RTP range offset
	rtpOffset := 0
	for ; m.rtpPorts[rtpOffset]; rtpOffset += 102 {
	}
	rtpStart := m.baseRTPPort + rtpOffset
	rtpEnd := rtpStart + 100
	if rtpEnd > m.maxRTPPort || sipPort > 65534 {
		return 0, "", 0, fmt.Errorf("agent port capacity exhausted")
	}
	m.sipPorts[sipOffset] = true
	m.rtpPorts[rtpOffset] = true
	rtpPortsStr := fmt.Sprintf("%d-%d", rtpStart, rtpEnd)

	return sipPort, rtpPortsStr, rtpOffset, nil
}

func (m *BaresipManager) releasePorts(sipPort, rtpOffset int) {
	m.portMu.Lock()
	defer m.portMu.Unlock()

	sipOffset := sipPort - m.baseSIPPort
	delete(m.sipPorts, sipOffset)
	delete(m.rtpPorts, rtpOffset)
}

func (m *BaresipManager) SpawnAgent(ctx context.Context, alias string, accountLine string) error {
	m.spawnMu.Lock()
	defer m.spawnMu.Unlock()

	if alias == "" || len(alias) > 2048 || strings.ContainsAny(alias, "\r\n\x00") {
		return fmt.Errorf("invalid agent alias %q: only alphanumeric, underscores, and dashes allowed", alias)
	}

	if _, ok := m.GetAgent(alias); ok {
		return fmt.Errorf("agent %s already exists", alias)
	}

	// Create unique data dir for agent
	m.mu.RLock()
	runID := m.runID
	m.mu.RUnlock()
	agentID, err := GenerateSessionToken()
	if err != nil {
		return err
	}
	agentDir := filepath.Join(m.dataDir, "agents", fmt.Sprintf("run-%d", runID), agentID[:12])
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return err
	}

	// Allocate ports from the pool
	sipPort, rtpPorts, rtpOffset, err := m.allocatePorts()
	if err != nil {
		return err
	}

	// 1. Proxy listener address (Master Hub connects here).
	// Use a Unix socket for the Telefonist master ↔ agent proxy hop to avoid
	// loopback TCP port allocation and reduce local connection overhead.
	socketDir, err := os.MkdirTemp("", "telefonist-")
	if err != nil {
		m.releasePorts(sipPort, rtpOffset)
		return err
	}
	keepSocket := false
	defer func() {
		if !keepSocket {
			_ = os.Remove(filepath.Join(socketDir, "proxy.sock"))
			_ = os.Remove(socketDir)
		}
	}()
	proxySock := filepath.Join(socketDir, "proxy.sock")
	if err := os.Remove(proxySock); err != nil && !os.IsNotExist(err) {
		m.releasePorts(sipPort, rtpOffset)
		return err
	}
	proxyAddr := "unix:" + proxySock

	// 2. Baresip listener address (Internal Agent Proxy connects here)
	lBaresip, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		m.releasePorts(sipPort, rtpOffset)
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
		m.releasePorts(sipPort, rtpOffset)
		return err
	}
	if err := os.MkdirAll(agentRecordsDir, 0755); err != nil {
		m.releasePorts(sipPort, rtpOffset)
		return err
	}
	globalSoundsDir, err := filepath.Abs(filepath.Join(m.dataDir, "sounds"))
	if err != nil {
		m.releasePorts(sipPort, rtpOffset)
		return err
	}
	sipAddr := ""
	if m.hasSIPListen {
		sipAddr = net.JoinHostPort(m.baseSIPIP, strconv.Itoa(sipPort))
	}
	if err := CreateConfig(agentDir, m.maxCalls, m.rtpNet, rtpPorts, m.rtpTimeout, baresipAddr, sipAddr, m.useALSA, agentRecordsDir, globalSoundsDir); err != nil {
		m.releasePorts(sipPort, rtpOffset)
		return err
	}

	// Write empty accounts file to prevent initial registration before bridge is ready.
	// Baresip will load UAs via explicit uanew command later.
	accountsFile := filepath.Join(agentDir, "accounts")
	if err := os.WriteFile(accountsFile, []byte(""), 0644); err != nil {
		m.releasePorts(sipPort, rtpOffset)
		return err
	}

	// Write empty contacts file to prevent baresip from generating default template contacts.
	contactsFile := filepath.Join(agentDir, "contacts")
	if err := os.WriteFile(contactsFile, []byte(""), 0644); err != nil {
		m.releasePorts(sipPort, rtpOffset)
		return err
	}

	// Start agent process
	// We pass both addresses to the agent:
	// -ctrl_address: where the proxy listens for the Master
	// -baresip_ctrl_address: where Baresip is listening locally
	self, err := os.Executable()
	if err != nil {
		m.releasePorts(sipPort, rtpOffset)
		return err
	}

	cmd := exec.Command(self,
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
		m.releasePorts(sipPort, rtpOffset)
		return err
	}

	// Connect to agent's PROXY
	var gb *gobaresip.Baresip
	maxRetries := 60
	retryInterval := 20 * time.Millisecond
	for i := 0; i < maxRetries; i++ {
		gb, err = gobaresip.New(
			gobaresip.SetRemote(true),
			gobaresip.SetCtrlTCPAddr(proxyAddr),
			gobaresip.SetContext(m.master.ctx),
		)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(retryInterval):
		}
		if err != nil && err == ctx.Err() {
			break
		}

		if retryInterval < 100*time.Millisecond {
			retryInterval += 20 * time.Millisecond
		}
	}

	if err != nil {
		if killErr := cmd.Process.Kill(); killErr != nil {
			log.Printf("hub: failed to kill agent process %s: %v", alias, killErr)
		}
		m.releasePorts(sipPort, rtpOffset)
		_ = cmd.Wait()
		return fmt.Errorf("failed to connect to agent %s: %w", alias, err)
	}

	regChan := make(chan error, 1)
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
		RegChan:       regChan,
		ForwardDone:   make(chan struct{}), SocketDir: socketDir,
	}

	m.mu.Lock()
	m.agents[alias] = agent
	m.mu.Unlock()
	keepSocket = true

	// Monitor agent process exit
	go func() {
		err := cmd.Wait()
		if err != nil && !strings.Contains(err.Error(), "signal: killed") {
			log.Printf("hub: agent %s exited with error: %v", alias, err)
		} else {
			log.Printf("hub: agent %s exited", alias)
		}
		close(agent.Done)
		if !agent.stopping.Load() {
			raw, _ := json.Marshal(map[string]any{"event": true, "type": "AGENT_EXIT", "param": "agent exited unexpectedly"})
			m.master.ForwardAgentMsg(alias, gobaresip.Msg{Event: &gobaresip.EventMsg{Type: "AGENT_EXIT", RawJSON: raw}})
		}
		m.stopIfCurrent(agent)
	}()

	// Forward agent messages to master hub
	go m.forwardMessages(m.master.ctx, agent)

	// Explicitly trigger UA registration now that the telemetry bridge is up
	if err := agent.Baresip.CmdWsContext(ctx, []byte("uanew "+accountLine)); err != nil {
		log.Printf("hub: failed to trigger registration for agent %s: %v", alias, err)
		m.StopAgent(alias)
		return err
	}

	// Only wait for registration if it is not disabled via regint=0
	needsRegister := !strings.Contains(";"+accountLine+";", ";regint=0;")
	if needsRegister {
		log.Printf("hub: waiting for SIP registration of agent %s...", alias)
		var regErr error
		select {
		case err := <-regChan:
			if err != nil {
				regErr = err
			} else {
				log.Printf("hub: agent %s registered successfully", alias)
			}
		case <-agent.Done:
			regErr = fmt.Errorf("agent process exited while waiting for registration")
		case <-time.After(10 * time.Second):
			regErr = fmt.Errorf("timeout waiting for SIP registration of agent %s", alias)
		case <-ctx.Done():
			regErr = ctx.Err()
		}

		if regErr != nil {
			m.StopAgent(alias)
			return regErr
		}
	}

	return nil
}

func (m *BaresipManager) forwardMessages(ctx context.Context, a *Agent) {
	defer close(a.ForwardDone)
	msgChan := a.Baresip.GetMsgChan()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgChan:
			if !ok {
				return
			}
			if msg.Event != nil {
				e := msg.Event
				if e.Type == "NATIVE_DONE" {
					a.nativeDone.Store(true)
				}
				if e.Type == "REGISTER_OK" && a.RegChan != nil {
					select {
					case a.RegChan <- nil:
					default:
					}
				} else if e.Type == "REGISTER_FAIL" && a.RegChan != nil {
					select {
					case a.RegChan <- fmt.Errorf("SIP registration failed: %s", e.Param):
					default:
					}
				}
			}
			m.master.ForwardAgentMsg(a.Alias, msg)
		}
	}
}

func (m *BaresipManager) stopAgent(a *Agent) {
	a.stopping.Store(true)
	log.Printf("stopping agent %s", a.Alias)
	// Try a graceful shutdown first to allow SIP deregistrations and WAV file flushes
	if err := a.Baresip.CmdQuit(); err != nil {
		log.Printf("hub: failed to send quit to agent %s: %v", a.Alias, err)
	}

	// Wait for process to exit
	select {
	case <-a.Done:
		// Process exited naturally or via quit
	case <-time.After(5 * time.Second):
		// Process did not exit in time, force kill
		if a.Cmd.Process != nil {
			if err := a.Cmd.Process.Kill(); err != nil {
				log.Printf("hub: failed to kill agent process %s: %v", a.Alias, err)
			}
		}
		<-a.Done // Wait for monitor goroutine to finish Wait()
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = a.Baresip.WaitReadDrain(drainCtx)
	drainCancel()
	a.Baresip.Close()
	<-a.ForwardDone
	if !a.nativeDone.Load() {
		raw, _ := json.Marshal(map[string]any{"event": true, "type": "TELEMETRY_LOSS", "param": "agent exited without complete telemetry flush"})
		m.master.ForwardAgentMsg(a.Alias, gobaresip.Msg{Event: &gobaresip.EventMsg{Type: "TELEMETRY_LOSS", RawJSON: raw}})
	}
	_ = os.Remove(filepath.Join(a.SocketDir, "proxy.sock"))
	_ = os.Remove(a.SocketDir)
	m.releasePorts(a.SIPPort, a.RTPOffset)
}

func (m *BaresipManager) stopIfCurrent(a *Agent) {
	m.mu.Lock()
	current := m.agents[a.Alias] == a
	if current {
		m.stoppingWG.Add(1)
		delete(m.agents, a.Alias)
	}
	m.mu.Unlock()
	if current {
		defer m.stoppingWG.Done()
		m.stopAgent(a)
	}
}

func (m *BaresipManager) StopAgent(alias string) {
	m.mu.Lock()
	a, ok := m.agents[alias]
	if ok {
		m.stoppingWG.Add(1)
		delete(m.agents, alias)
	}
	m.mu.Unlock()

	if ok {
		defer m.stoppingWG.Done()
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
	m.spawnMu.Lock()
	defer m.spawnMu.Unlock()
	m.mu.Lock()
	agentsToStop := make([]*Agent, 0, len(m.agents))
	for _, a := range m.agents {
		agentsToStop = append(agentsToStop, a)
	}
	m.agents = make(map[string]*Agent)
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, a := range agentsToStop {
		wg.Add(1)
		go func(agent *Agent) {
			defer wg.Done()
			m.stopAgent(agent)
		}(a)
	}
	wg.Wait()
	m.stoppingWG.Wait()
}

func (m *BaresipManager) ResolveTarget(cmd string, fallbackTarget string) (target string, finalCmd string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return fallbackTarget, cmd
	}

	// Check for "alias:command" prefix (e.g., "ua1:dial alice")
	if idx := strings.LastIndex(parts[0], ":"); idx > 0 {
		prefix := ExtractAlias(parts[0][:idx])
		m.mu.RLock()
		_, ok := m.agents[prefix]
		m.mu.RUnlock()
		if ok {
			rest := parts[0][idx+1:] + " " + strings.Join(parts[1:], " ")
			return prefix, strings.TrimSpace(rest)
		}
	}

	return fallbackTarget, cmd
}

func isSafeAlias(alias string) bool {
	if len(alias) == 0 || len(alias) > 64 {
		return false
	}
	for _, c := range alias {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
