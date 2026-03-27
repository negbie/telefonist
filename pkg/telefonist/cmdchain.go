package telefonist

import (
	"context"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// chainToken represents either a command to execute or a delay to wait.
type chainToken struct {
	command string
	delay   time.Duration
	isDelay bool
}

// durationPattern matches duration strings like "100ms", "10s", "2m".
var durationPattern = regexp.MustCompile(`^\d+(ms|s|m)$`)

// wavPattern matches the shortcut ;wav=NAME/
var wavPattern = regexp.MustCompile(`;wav=([^/]+)/`)

// knownCommands is the set of recognized baresip command keywords.
var knownCommands = map[string]bool{
	"100rel":         true,
	"about":          true,
	"accept":         true,
	"acceptdir":      true,
	"addcontact":     true,
	"answermode":     true,
	"apistate":       true,
	"atransferabort": true,
	"atransferexec":  true,
	"atransferstart": true,
	"aubitrate":      true,
	"audio_debug":    true,
	"aufileinfo":     true,
	"autocmdinfo":    true,
	"autodialadd":    true,
	"autodialdel":    true,
	"autohangupgap":  true,
	"auplay":         true,
	"ausrc":          true,
	"callfind":       true,
	"callstat":       true,
	"conf_reload":    true,
	"config":         true,
	"contact_next":   true,
	"contact_prev":   true,
	"contacts":       true,
	"dial":           true,
	"dialcontact":    true,
	"dialdir":        true,
	"dnd":            true,
	"entransp":       true,
	"hangup":         true,
	"hangupall":      true,
	"help":           true,
	"hold":           true,
	"insmod":         true,
	"line":           true,
	"listcalls":      true,
	"loglevel":       true,
	"main":           true,
	"medialdir":      true,
	"memstat":        true,
	"message":        true,
	"modules":        true,
	"mute":           true,
	"netchange":      true,
	"netstat":        true,
	"options":        true,
	"play":           true,
	"quit":           true,
	"refer":          true,
	"reginfo":        true,
	"reinvite":       true,
	"resume":         true,
	"rmcontact":      true,
	"rmmod":          true,
	"setadelay":      true,
	"setansval":      true,
	"sipstat":        true,
	"siptrace":       true,
	"sndcode":        true,
	"statmode":       true,
	"stopringing":    true,
	"sysinfo":        true,
	"timers":         true,
	"tlsissuer":      true,
	"tlssubject":     true,
	"transfer":       true,
	"uaaddheader":    true,
	"uadel":          true,
	"uadelall":       true,
	"uafind":         true,
	"uanew":          true,
	"uareg":          true,
	"uarmheader":     true,
	"uastat":         true,
	"uuid":           true,
	"video_debug":    true,
	"videodir":       true,
	"vidsrc":         true,
	// telefonist-specific commands
}

// defaultTrailingDelay is the delay added after the last command in a chain
// if the chain does not end with an explicit delay.
const defaultTrailingDelay = 1 * time.Second

// isChained checks whether an input string contains chained commands
// (i.e. pipes that separate known commands or durations).
func isChained(input string) bool {
	if !strings.Contains(input, "|") {
		return false
	}

	parts := splitByPipe(input)
	if len(parts) < 2 {
		return false
	}

	// At least one token after a pipe must be a duration or known command
	for _, part := range parts[1:] {
		trimmed := strings.TrimSpace(part)
		if isDuration(trimmed) || isKnownCommand(trimmed) {
			return true
		}
	}

	return false
}

func splitByPipe(input string) []string {
	var parts []string
	var current strings.Builder
	inSecret := false

	for i := 0; i < len(input); i++ {
		// Case insensitive check for auth_pass=
		if !inSecret && i+10 <= len(input) && strings.ToLower(input[i:i+10]) == "auth_pass=" {
			inSecret = true
		}

		char := input[i]
		// End secret on ; or whitespace
		if inSecret && (char == ';' || char == ' ' || char == '\t' || char == '\n' || char == '\r') {
			inSecret = false
		}

		if char == '|' && !inSecret {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteByte(char)
		}
	}
	parts = append(parts, current.String())
	return parts
}

// isDuration checks if a string matches a duration pattern like "10s", "500ms", "2m".
func isDuration(s string) bool {
	return durationPattern.MatchString(s)
}

// isKnownCommand checks if the first word of a string is a known command keyword.
// It also handles optional agent prefix like "ua1:dial".
func isKnownCommand(s string) bool {
	if s == "" {
		return false
	}
	firstWord := strings.SplitN(s, " ", 2)[0]
	
	// Check for agent: prefix (e.g. ua1:dial)
	if idx := strings.Index(firstWord, ":"); idx != -1 {
		// Ensure it's not a SIP URI (which starts with sip:)
		if !strings.HasPrefix(strings.ToLower(firstWord), "sip:") {
			firstWord = firstWord[idx+1:]
		} else {
			// Even for SIP URIs, there might be a SECOND colon for the command
			// e.g. sip:test1@host:dial
			// We look for the colon that's NOT part of sip:
			remaining := firstWord[4:]
			if idx2 := strings.Index(remaining, ":"); idx2 != -1 {
				firstWord = remaining[idx2+1:]
			} else {
				// It's just a SIP URI, probably an argument, not a command prefix
				return false
			}
		}
	}

	return knownCommands[firstWord] || knownCommands[strings.ToLower(firstWord)]
}

// expandCommand expands shortcuts like ;input_wav=NAME/ into full audio configuration.
func expandCommand(cmd string, dataDir string) string {
	soundsDir := filepath.Join(dataDir, "sounds")
	recordsDir := filepath.Join(dataDir, "recorded_temp")

	cmd = wavPattern.ReplaceAllString(cmd, ";audio_source=aufile,"+soundsDir+"/$1;audio_player=aufile,"+recordsDir+"/")
	cmd = strings.ReplaceAll(cmd, ";input_wav=", ";audio_source=aufile,"+soundsDir+"/")
	return cmd
}

// parseChain parses a pipe-separated input into a slice of chainTokens.
// Only '|' is treated as the chaining separator.
func parseChain(input string) []chainToken {
	parts := splitByPipe(input)
	if len(parts) == 0 {
		return nil
	}

	var tokens []chainToken
	for _, part := range parts {
		m := strings.TrimSpace(part)
		if m == "" {
			continue
		}
		if isDuration(m) {
			d, err := time.ParseDuration(m)
			if err != nil {
				log.Printf("cmdchain: invalid duration %q: %v", m, err)
				continue
			}
			tokens = append(tokens, chainToken{delay: d, isDelay: true})
		} else {
			tokens = append(tokens, chainToken{command: m})
		}
	}

	return tokens
}

// executeChain runs a sequence of chain tokens, executing commands via the
// hub and sleeping for delays.
func executeChain(ctx context.Context, h *WsHub, tokens []chainToken) {
	for _, tok := range tokens {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if tok.isDelay {
			select {
			case <-time.After(tok.delay):
			case <-ctx.Done():
				return
			}
		} else {
			cmd := expandCommand(tok.command, h.DataDir)
			h.executeSmartCommand(cmd)
		}
	}
}
