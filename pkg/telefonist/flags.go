package telefonist

import (
	"flag"
	"fmt"
	"os"
)

var Version = "0.7.1"

// AppFlags mirrors the CLI flags for the telefonist process.
// Keep these fields aligned with the flags defined in parseFlags()
// to avoid accidental behavior changes.
type AppFlags struct {
	CtrlAddr        string
	BaresipCtrlAddr string
	UiAddr          string
	UiAdminPassword string
	MaxCalls        uint
	RtpNet          string
	RtpPorts        string
	RtpTimeout      uint
	SipAddr         string
	TlsCert         string
	TlsKey          string
	UseAlsa         bool
	Version         bool
	DataDir         string
	SkipSipMsg      string
	Agent           bool
	Alias           string
}

// ParseFlags defines and parses CLI flags, returning the collected values.
// Defaults and flag names must remain stable for compatibility.
func ParseFlags() AppFlags {
	var f AppFlags

	flag.StringVar(&f.CtrlAddr, "ctrl_address", "127.0.0.1:4444", "Local control listen address")
	flag.StringVar(&f.UiAddr, "ui_address", "0.0.0.0:8080", "UI listen address")
	flag.StringVar(&f.UiAdminPassword, "ui_admin_password", "telefonist", "UI admin password")
	flag.UintVar(&f.MaxCalls, "max_calls", 10, "Maximum number of incoming calls")
	flag.StringVar(&f.RtpNet, "rtp_interface", "", "RTP interface like eth0")
	flag.StringVar(&f.RtpPorts, "rtp_ports", "10000-11000", "RTP port range")
	flag.UintVar(&f.RtpTimeout, "rtp_timeout", 10, "Seconds after which a call with no incoming RTP packets will be terminated")
	flag.StringVar(&f.SipAddr, "sip_address", "", "SIP listen address like 0.0.0.0:5060")
	flag.StringVar(&f.TlsCert, "tls_cert", "", "Path to TLS certificate file")
	flag.StringVar(&f.TlsKey, "tls_key", "", "Path to TLS key file")
	flag.BoolVar(&f.UseAlsa, "use_alsa", false, "Use ALSA for audio (uncomments alsa lines in config)")
	flag.BoolVar(&f.Version, "version", false, "Print version")
	flag.StringVar(&f.DataDir, "data_dir", "data", "Directory for configuration files and data")
	flag.StringVar(&f.SkipSipMsg, "skip_sip_msg", "OPTIONS", "Comma separated list of SIP methods to ignore")

	flag.Parse()
	return f
}

func MaybePrintVersionAndExit(version bool) {
	if version {
		fmt.Println(Version)
		os.Exit(0)
	}
}
