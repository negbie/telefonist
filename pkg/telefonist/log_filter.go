package telefonist

import "strings"

const (
	logFiltersSettingKey = "log_filters"
	maxLogFilters        = 200
	maxLogFilterLength   = 512
)

// normalizeLogFilters trims empty entries, caps overly long entries, and
// removes duplicate filters case-insensitively while preserving display casing.
func normalizeLogFilters(filters []string) []string {
	seen := make(map[string]bool, len(filters))
	out := make([]string, 0, len(filters))

	for _, filter := range filters {
		filter = strings.TrimSpace(filter)
		if filter == "" {
			continue
		}
		if len(filter) > maxLogFilterLength {
			filter = filter[:maxLogFilterLength]
		}
		key := strings.ToLower(filter)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, filter)
		if len(out) >= maxLogFilters {
			break
		}
	}

	return out
}

func logMessageMatchesFilters(message string, filters []string) bool {
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

func (h *WsHub) SetLogFilters(filters []string) {
	if h == nil {
		return
	}
	filters = normalizeLogFilters(filters)
	lowerFilters := make([]string, len(filters))
	for i, filter := range filters {
		lowerFilters[i] = strings.ToLower(filter)
	}

	h.logFilterMu.Lock()
	h.logFilters = lowerFilters
	h.logFilterMu.Unlock()
}

func (h *WsHub) GetLogFilters() []string {
	if h == nil {
		return nil
	}
	h.logFilterMu.RLock()
	defer h.logFilterMu.RUnlock()
	return append([]string(nil), h.logFilters...)
}

func logPartMatchesLowerFilters(part string, lowerFilters []string) bool {
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

func (h *WsHub) ShouldFilterLog(parts ...string) bool {
	if h == nil {
		return false
	}

	h.logFilterMu.RLock()
	defer h.logFilterMu.RUnlock()

	if len(h.logFilters) == 0 {
		return false
	}
	for _, part := range parts {
		if logPartMatchesLowerFilters(part, h.logFilters) {
			return true
		}
	}
	return false
}
