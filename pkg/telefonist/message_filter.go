package telefonist

import "strings"

const (
	messageFiltersSettingKey = "message_filters"
	maxMessageFilters        = 200
	maxMessageFilterLength   = 512
)

// normalizeMessageFilters trims empty entries, caps overly long entries, and
// removes duplicate filters case-insensitively while preserving display casing.
func normalizeMessageFilters(filters []string) []string {
	seen := make(map[string]bool, len(filters))
	out := make([]string, 0, len(filters))

	for _, filter := range filters {
		filter = strings.TrimSpace(filter)
		if filter == "" {
			continue
		}
		if len(filter) > maxMessageFilterLength {
			filter = filter[:maxMessageFilterLength]
		}
		key := strings.ToLower(filter)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, filter)
		if len(out) >= maxMessageFilters {
			break
		}
	}

	return out
}

func messageMatchesFilters(message string, filters []string) bool {
	if message == "" || len(filters) == 0 {
		return false
	}

	message = strings.ToLower(message)
	for _, filter := range filters {
		filter = strings.TrimSpace(filter)
		if filter == "" {
			continue
		}
		if strings.Contains(message, strings.ToLower(filter)) {
			return true
		}
	}
	return false
}

func (h *WsHub) SetMessageFilters(filters []string) {
	if h == nil {
		return
	}
	filters = normalizeMessageFilters(filters)
	lowerFilters := make([]string, len(filters))
	for i, filter := range filters {
		lowerFilters[i] = strings.ToLower(filter)
	}

	h.messageFilterMu.Lock()
	h.messageFilters = lowerFilters
	h.messageFilterMu.Unlock()
}

func (h *WsHub) GetMessageFilters() []string {
	if h == nil {
		return nil
	}
	h.messageFilterMu.RLock()
	defer h.messageFilterMu.RUnlock()
	return append([]string(nil), h.messageFilters...)
}

func messagePartMatchesLowerFilters(part string, lowerFilters []string) bool {
	if part == "" {
		return false
	}
	part = strings.ToLower(part)
	for _, filter := range lowerFilters {
		if strings.Contains(part, filter) {
			return true
		}
	}
	return false
}

func (h *WsHub) ShouldFilterMessage(parts ...string) bool {
	if h == nil {
		return false
	}

	h.messageFilterMu.RLock()
	defer h.messageFilterMu.RUnlock()

	if len(h.messageFilters) == 0 {
		return false
	}
	for _, part := range parts {
		if messagePartMatchesLowerFilters(part, h.messageFilters) {
			return true
		}
	}
	return false
}

func (h *WsHub) ShouldFilterMessageWithRaw(raw []byte, parts ...string) bool {
	if h == nil {
		return false
	}

	h.messageFilterMu.RLock()
	defer h.messageFilterMu.RUnlock()

	if len(h.messageFilters) == 0 {
		return false
	}
	for _, part := range parts {
		if messagePartMatchesLowerFilters(part, h.messageFilters) {
			return true
		}
	}
	if len(raw) == 0 {
		return false
	}
	return messagePartMatchesLowerFilters(string(raw), h.messageFilters)
}
