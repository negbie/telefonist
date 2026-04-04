package telefonist

import (
	"flag"
	"fmt"
	"os"
)

const Version = "0.8.4"

// AppFlags mirrors the CLI flags for the telefonist process.
type AppFlags struct {
	CtrlAddr        string
	BaresipCtrlAddr string
	UIAddr          string
	UIAdminPassword string
	MaxCalls        uint
	RTPNet          string
	RTPPorts        string
	RTPTimeout      uint
	SIPListen       string
	TLSCert         string
	TLSKey          string
	UseALSA         bool
	Version         bool
	DataDir         string
	SoundsDir       string
	Agent           bool
	Alias           string
	UIAPIKey        string
}

func ParseFlags() AppFlags {
	var f AppFlags

	flag.StringVar(&f.CtrlAddr, "ctrl_address", "127.0.0.1:4444", "Local control listen address")
	flag.StringVar(&f.UIAddr, "ui_address", "0.0.0.0:8080", "UI listen address")
	flag.StringVar(&f.UIAdminPassword, "ui_admin_password", "telefonist", "UI admin password")
	flag.UintVar(&f.MaxCalls, "max_calls", 10, "Maximum number of incoming calls")
	flag.StringVar(&f.RTPNet, "rtp_interface", "", "RTP interface like eth0")
	flag.StringVar(&f.RTPPorts, "rtp_ports", "10000-11000", "RTP port range")
	flag.UintVar(&f.RTPTimeout, "rtp_timeout", 10, "Seconds after which a call with no incoming RTP packets will be terminated")
	flag.StringVar(&f.SIPListen, "sip_listen", "", "SIP listen address and base port for agents (e.g., 127.0.0.1:5060)")
	flag.StringVar(&f.TLSCert, "tls_cert", "", "Path to TLS certificate file")
	flag.StringVar(&f.TLSKey, "tls_key", "", "Path to TLS key file")
	flag.BoolVar(&f.UseALSA, "use_alsa", false, "Use ALSA for audio (uncomments alsa lines in config)")
	flag.BoolVar(&f.Version, "version", false, "Print version")
	flag.StringVar(&f.DataDir, "data_dir", "data", "Directory for configuration files and data")
	flag.StringVar(&f.SoundsDir, "sounds_dir", "", "Directory for sound files (optional, defaults to data_dir/sounds)")
	flag.StringVar(&f.UIAPIKey, "ui_api_key", "", "API Key for X-API-Key header (optional, defaults to a random string if empty)")

	flag.Parse()
	return f
}

func MaybePrintVersionAndExit(version bool) {
	if !version {
		return
	}
	fmt.Println(Version)
	os.Exit(0)
}
