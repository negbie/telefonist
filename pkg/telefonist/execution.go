package telefonist

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func (h *WsHub) executeSmartCommand(ctx context.Context, cmd string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sounds, _ := filepath.Abs(filepath.Join(h.DataDir, "sounds"))
	cmd = ExpandShortcuts(cmd, sounds, "")
	target, cmd := h.bm.ResolveTarget(cmd, h.getActiveAgent())
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}
	first := strings.ToLower(parts[0])
	if strings.Contains(first, ":") {
		return fmt.Errorf("unknown agent target in %q", parts[0])
	}
	switch first {
	case "uafind":
		if len(parts) != 2 {
			return fmt.Errorf("uafind requires an agent")
		}
		if _, ok := h.bm.GetAgent(parts[1]); !ok {
			return fmt.Errorf("unknown agent %q", parts[1])
		}
		h.setActiveAgent(parts[1])
		return nil
	case "uadelall":
		h.bm.CloseAll()
		h.setActiveAgent("")
		return nil
	case "uanew":
		if len(parts) < 2 {
			return fmt.Errorf("uanew requires an account")
		}
		account := strings.TrimSpace(cmd[len(parts[0]):])
		alias := ExtractAlias(account)
		if _, ok := h.bm.GetAgent(alias); ok {
			h.bm.StopAgent(alias)
		}
		h.BroadcastCommandHint(cmd, alias)
		if err := h.bm.SpawnAgent(ctx, alias, account); err != nil {
			return fmt.Errorf("start agent %s: %w", alias, err)
		}
		h.setActiveAgent(alias)
		return nil
	}
	agent, ok := h.bm.GetAgent(target)
	if !ok {
		return fmt.Errorf("no active agent for %q", parts[0])
	}
	h.BroadcastCommandHint(cmd, target)
	return agent.Baresip.CmdWsContext(ctx, []byte(cmd))
}
