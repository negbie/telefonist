package main

import (
	_ "embed"
	"flag"
	"log"

	gobaresip "github.com/negbie/go-baresip"
)

//go:embed zip/sounds.tar.gz
var baresipSounds []byte

//go:embed zip/espeak-ng-data.tar.gz
var espeakNGData []byte

func main() {
	debug := flag.Bool("debug", false, "Set debug mode")
	lokiURL := flag.String("loki_url", "", "URL to remote Loki server like http://localhost:3100")
	guiAddr := flag.String("gui_address", "0.0.0.0:8080", "Local GUI listen address")
	logStd := flag.Bool("log_stderr", true, "Log to stderr")
	maxCalls := flag.String("max_cc_calls", "20", "Max concurrent calls")
	rtpNet := flag.String("rtp_interface", "", "RTP interface like eth0")
	rtpPorts := flag.String("rtp_ports", "10000-20000", "RTP port range")
	sipAddr := flag.String("sip_address", "", "SIP listen address like 0.0.0.0:5060")
	hookURL := flag.String("webhook_url", "", "Mattermost, Slack incoming webhook URL")
	flag.Parse()

	createConfig(*maxCalls, *rtpNet, *rtpPorts, *sipAddr)

	var loki *LokiClient
	var err error

	if *lokiURL != "" {
		loki, err = NewLokiClient(*lokiURL, 20, 4)
		if err != nil {
			log.Fatal(err)
		}
		defer loki.Close()
	}

	var lokiELabels = map[string]string{
		"job":   "go-baresip",
		"level": "info",
	}
	var lokiRLabels = map[string]string{
		"job":   "go-baresip",
		"level": "info",
	}

	gb, err := gobaresip.New(
		gobaresip.SetAudioPath("sounds"),
		gobaresip.SetConfigPath("."),
		gobaresip.SetDebug(*debug),
		gobaresip.SetWsAddr(*guiAddr),
	)
	if err != nil {
		log.Fatal(err)
	}

	eChan := gb.GetEventChan()
	rChan := gb.GetResponseChan()

	go func() {
		for {
			select {
			case e, ok := <-eChan:
				if !ok {
					return
				}
				level := eventLevel(&e)
				msg := string(e.RawJSON)

				if *lokiURL != "" {
					lokiELabels["level"] = level
					loki.Send(lokiELabels, msg)
				}
				if *hookURL != "" && (level == "warning" || level == "error") {
					go func(su string) {
						if err := page(su, level, msg); err != nil {
							log.Println(err)
						}
					}(*hookURL)
				}
				if *logStd {
					log.Println(level, ":", msg)
				}
			case r, ok := <-rChan:
				if !ok {
					return
				}
				msg := string(r.RawJSON)

				if *lokiURL != "" {
					loki.Send(lokiRLabels, msg)
				}
				if *logStd {
					log.Println("info", ":", msg)
				}
			}
		}
	}()

	err = gb.Run()
	if err != nil {
		log.Println(err)
	}
	gb.Close()
}
