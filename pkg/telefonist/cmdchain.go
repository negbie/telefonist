package telefonist

import (
	"context"
	"log"
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

// makeSet creates a map[string]bool from a list of keys.
func makeSet(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// knownCommands is the set of recognized baresip command keywords.
var knownCommands = makeSet(
	"100rel", "about", "accept", "acceptdir", "addcontact", "answermode",
	"apistate", "atransferabort", "atransferexec", "atransferstart", "aubitrate",
	"audio_debug", "aufileinfo", "autocmdinfo", "autodialadd", "autodialdel",
	"autohangupgap", "auplay", "ausrc", "callfind", "callstat", "conf_reload",
	"config", "contact_next", "contact_prev", "contacts", "dial", "dialcontact",
	"dialdir", "dnd", "entransp", "hangup", "hangupall", "help", "hold",
	"insmod", "line", "listcalls", "loglevel", "main", "medialdir", "memstat",
	"message", "modules", "mute", "netchange", "netstat", "options", "play",
	"quit", "refer", "reginfo", "reinvite", "resume", "rmcontact", "rmmod",
	"setadelay", "setansval", "sipstat", "siptrace", "sndcode", "statmode",
	"stopringing", "sysinfo", "timers", "tlsissuer", "tlssubject", "transfer",
	"uaaddheader", "uadel", "uadelall", "uafind", "uanew", "uareg", "uarmheader",
	"uastat", "uuid", "video_debug", "videodir", "vidsrc",
)

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

	if idx := strings.LastIndex(firstWord, ":"); idx != -1 {
		firstWord = firstWord[idx+1:]
	}

	return knownCommands[firstWord] || knownCommands[strings.ToLower(firstWord)]
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
			h.executeSmartCommand(tok.command)
		}
	}
}
