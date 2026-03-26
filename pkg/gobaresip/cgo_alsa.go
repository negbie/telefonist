//go:build linux && cgo && alsa
// +build linux,cgo,alsa

package gobaresip

/*
#cgo linux LDFLAGS: -lasound -lz
*/
import "C"
