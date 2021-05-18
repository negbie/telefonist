package main

import (
	"flag"
	"log"
	"strings"

	gobaresip "github.com/negbie/go-baresip"
)

func main() {
	lokiURL := flag.String("loki_url", "http://localhost:3100", "URL to remote Loki server")
	guiAddr := flag.String("gui_address", "0.0.0.0:8080", "Local GUI listen address")
	debug := flag.Bool("debug", false, "Set debug mode")
	flag.Parse()

	gb, err := gobaresip.New(
		gobaresip.SetConfigPath("."),
		gobaresip.SetAudioPath("sounds"),
		gobaresip.SetDebug(*debug),
		gobaresip.SetWsAddr(*guiAddr),
	)
	if err != nil {
		log.Println(err)
		return
	}

	loki, lerr := NewLokiClient(*lokiURL, 10, 4)
	if lerr != nil {
		log.Println(lerr)
	}
	defer loki.Close()

	var lokiELabels = map[string]string{
		"job":   "go-baresip",
		"level": "info",
	}
	var lokiRLabels = map[string]string{
		"job":   "go-baresip",
		"level": "info",
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
				if lerr == nil {
					cc := e.Type == "CALL_CLOSED"
					if cc && e.ID == "" {
						lokiELabels["level"] = "warning"
					} else if cc && strings.HasPrefix(e.Param, "4") {
						lokiELabels["level"] = "warning"
					} else if cc && strings.HasPrefix(e.Param, "5") {
						lokiELabels["level"] = "error"
					} else if cc && strings.HasPrefix(e.Param, "6") {
						lokiELabels["level"] = "error"
					} else if cc && strings.Contains(e.Param, "error") {
						lokiELabels["level"] = "error"
					} else if strings.Contains(e.Type, "FAIL") {
						lokiELabels["level"] = "warning"
					} else if strings.Contains(e.Type, "ERROR") {
						lokiELabels["level"] = "error"
					} else {
						lokiELabels["level"] = "info"
					}

					loki.Send(lokiELabels, string(e.RawJSON))
				} else {
					log.Println(string(e.RawJSON))
				}

			case r, ok := <-rChan:
				if !ok {
					return
				}
				if lerr == nil {
					loki.Send(lokiRLabels, string(r.RawJSON))
				} else {
					log.Println(string(r.RawJSON))
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
