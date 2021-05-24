package main

import (
	"flag"
	"log"

	"github.com/coocood/freecache"
	gobaresip "github.com/negbie/go-baresip"
)

func main() {
	debug := flag.Bool("debug", false, "Set debug mode")
	guiAddr := flag.String("gui_address", "0.0.0.0:8080", "Local GUI listen address")
	logStd := flag.Bool("log_stderr", true, "Log to stderr")
	lokiURL := flag.String("loki_url", "", "URL to remote Loki server like http://localhost:3100")
	maxCalls := flag.Uint("max_calls", 40, "Maximum number of incoming calls")
	rtpNet := flag.String("rtp_interface", "", "RTP interface like eth0")
	rtpPorts := flag.String("rtp_ports", "10000-11000", "RTP port range")
	rtpTimeout := flag.Uint("rtp_timeout", 5, "Seconds after which a call with no incoming RTP packets will be terminated")
	sipAddr := flag.String("sip_address", "", "SIP listen address like 0.0.0.0:5060")
	webhookDelay := flag.Uint("webhook_delay", 600, "Webhook resend delay of warnings and errors in seconds")
	webhookURL := flag.String("webhook_url", "", "Send warnings and errors to this Mattermost or Slack webhook URL")
	flag.Parse()

	createConfig(*maxCalls, *rtpNet, *rtpPorts, *rtpTimeout, *sipAddr)

	if *webhookDelay == 0 {
		*webhookDelay = 1
	}

	var cache = freecache.NewCache(10 * 1024 * 1024)
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
				if *webhookURL != "" && (level == "warning" || level == "error") {
					go func() {
						key := []byte(e.AccountAOR + e.PeerURI + e.Param)
						if _, err := cache.Get(key); err != nil {
							if err := page(*webhookURL, level, msg); err != nil {
								log.Println(err)
							}
							cache.Set(key, nil, int(*webhookDelay))
						}
					}()
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
