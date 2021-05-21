package main

import (
	"strings"

	gobaresip "github.com/negbie/go-baresip"
)

func eventLevel(e *gobaresip.EventMsg) string {
	cc := e.Type == "CALL_CLOSED"
	if cc && e.ID == "" {
		return "warning"
	} else if cc && strings.HasPrefix(e.Param, "4") {
		return "warning"
	} else if cc && strings.HasPrefix(e.Param, "5") {
		return "error"
	} else if cc && strings.HasPrefix(e.Param, "6") {
		return "error"
	} else if cc && strings.Contains(e.Param, "error") {
		return "error"
	} else if strings.Contains(e.Type, "FAIL") {
		return "warning"
	} else if strings.Contains(e.Type, "ERROR") {
		return "error"
	}
	return "info"
}
