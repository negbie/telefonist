//go:build !linux || !cgo

package gobaresip

import "errors"

// The control protocol is portable so the orchestrator can be tested without
// native libraries. Actual SIP agents remain Linux/cgo-only.
var errNativeUnavailable = errors.New("SIP agents require Linux with cgo and the staged baresip libraries")

func (b *Baresip) setup() error                                  { return errNativeUnavailable }
func (b *Baresip) Run() error                                    { return errNativeUnavailable }
func (b *Baresip) CmdDirect(command, params, token string) error { return errNativeUnavailable }
