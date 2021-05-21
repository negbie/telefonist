<img src="https://user-images.githubusercontent.com/20154956/118413627-970a7680-b6a0-11eb-8ca1-f0d241736ffc.png">

# About
telefonist let's you automate your SIP test calls and send status information to Grafana Loki. It has minimal dependencies so can run it on a default Debian installation without installing any additional packages.

# Build
To build telefonist you either need to install Go 1.16 or Docker. To compile it with Go run:
```
go build -ldflags="-s -w" -o telefonist *.go
```
To compile it with Docker run:
```
sudo docker run --rm=true -itv $PWD:/mnt golang:buster /mnt/build_bin_docker.sh
```

# Releases
For linux-amd64 I will upload the latest binary [here](https://github.com/negbie/telefonist/releases).

# Setup
If you run telefonist it will write a baresip accounts, config, contacts and uuid file. The config file will be generated on each start.
The [accounts](https://github.com/baresip/baresip/wiki/Accounts) file will be generated once and won't be touched if it exist. Please add a SIP account to your accounts file an restart telefonist.
# Flags
You can start telefonist with following flags:
```
Usage of ./telefonist:
  -debug
        Set debug mode
  -gui_address string
        Local GUI listen address (default "0.0.0.0:8080")
  -log_stderr
        Log to stderr (default true)
  -loki_url string
        URL to remote Loki server like http://localhost:3100
  -max_cc_calls string
        Max concurrent calls (default "20")
  -rtp_interface string
        RTP interface like eth0
  -rtp_ports string
        RTP port range (default "10000-20000")
  -sip_address string
        SIP listen address like 0.0.0.0:5060
  -slack_url string
        Slack webhook URL
```

# GUI
<img src="https://user-images.githubusercontent.com/20154956/118876907-15a82380-b8ee-11eb-9fee-0264db099cb8.png">
