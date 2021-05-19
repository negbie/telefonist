package main

import (
	"bytes"
	"io/ioutil"
	"strings"

	"github.com/negbie/telefonist/zip"
)

func createConfig(maxCalls, rtpNet, rtpPorts, sipAddr string) {
	if sipAddr != "" {
		config = strings.Replace(
			config,
			"#sip_listen             0.0.0.0:5060",
			"sip_listen              "+sipAddr,
			1)
	}

	if rtpNet != "" {
		config = strings.Replace(
			config,
			"#net_interface          eth0",
			"net_interface           "+rtpNet,
			1)
	}

	if rtpPorts != "" {
		config = strings.Replace(
			config,
			"#rtp_ports              10000-20000",
			"rtp_ports               "+rtpPorts,
			1)
	}

	if maxCalls != "" {
		config = strings.Replace(
			config,
			"call_max_calls          1",
			"call_max_calls          "+maxCalls,
			1)
	}

	if err := zip.Decompress(bytes.NewReader(baresipSounds), "."); err != nil {
		panic(err)
	}
	if err := zip.Decompress(bytes.NewReader(espeakNGData), "."); err != nil {
		panic(err)
	}
	if err := ioutil.WriteFile("config", []byte(config), 0644); err != nil {
		panic(err)
	}
}

var config string = `
#
# baresip configuration
#

#------------------------------------------------------------------------------

# Core
poll_method             epoll           # poll, select, epoll ..

# SIP
#sip_listen             0.0.0.0:5060
#sip_certificate        cert.pem
sip_cafile              /etc/ssl/certs/ca-certificates.crt
#sip_trans_def          udp
#sip_verify_server      yes
sip_tos                 160

# Call
call_local_timeout      120
call_max_calls          1
call_hold_other_calls   no

# Audio
audio_path              sounds
#audio_player           alsa,default
#audio_source           alsa,default
#audio_alert            alsa,default
audio_source		    aufile,sounds/monorobo.wav
audio_player		    aubridge,nil
#ausrc_srate            48000
#auplay_srate           48000
#ausrc_channels         0
#auplay_channels        0
#audio_txmode           poll            # poll, thread
audio_level             no
ausrc_format            s16             # s16, float, ..
auplay_format           s16             # s16, float, ..
auenc_format            s16             # s16, float, ..
audec_format            s16             # s16, float, ..
audio_buffer            20-160          # ms

# Video
#video_source           v4l2,/dev/video0
#video_display          x11,nil
video_size              640x480
video_bitrate           1000000
video_fps               30.00
video_fullscreen        no
videnc_format           yuv420p

# AVT - Audio/Video Transport
rtp_tos                 184
rtp_video_tos           136
#rtp_ports              10000-20000
#rtp_bandwidth          512-1024 # [kbit/s]
rtcp_mux                no
jitter_buffer_type      fixed           # off, fixed, adaptive
jitter_buffer_delay     5-10            # frames
#jitter_buffer_wish     6               # frames for start
rtp_stats               no
#rtp_timeout            60

# Network
#dns_server             1.1.1.1:53
#dns_server             1.0.0.1:53
#dns_fallback           8.8.8.8:53
#net_interface          eth0

# Play tones
#file_ausrc             aufile
#file_srate             16000
#file_channels          1

#------------------------------------------------------------------------------
# Modules

#module_path            /usr/local/lib/baresip/modules

# UI Modules
#module                 stdio.so
#module                 cons.so
#module                 evdev.so
#module                 httpd.so

# Audio codec Modules (in order)
#module                 opus.so
#module                 amr.so
#module                 g7221.so
#module                 g722.so
#module                 g726.so
module                  g711.so
#module                 gsm.so
#module                 l16.so
#module                 mpa.so
#module                 codec2.so
#module                 ilbc.so

# Audio filter Modules (in encoding order)
#module                 vumeter.so
#module                 sndfile.so
#module                 speex_pp.so
#module                 plc.so
#module                 webrtc_aec.so

# Audio driver Modules
#module                 alsa.so
#module                 pulse.so
#module                 jack.so
#module                 portaudio.so
module                  aubridge.so
module                  aufile.so
#module                 ausine.so

# Video codec Modules (in order)
#module                 avcodec.so
#module                 vp8.so
#module                 vp9.so

# Video filter Modules (in encoding order)
#module                 selfview.so
#module                 snapshot.so
#module                 swscale.so
#module                 vidinfo.so
#module                 avfilter.so

# Video source modules
#module                 v4l2.so
#module                 v4l2_codec.so
#module                 x11grab.so
#module                 vidbridge.so

# Video display modules
#module                 directfb.so
#module                 x11.so
#module                 sdl.so
#module                 fakevideo.so

# Audio/Video source modules
#module                 avformat.so
#module                 rst.so
#module                 gst.so
#module                 gst_video.so

# Compatibility modules
#module                 ebuacip.so

# Media NAT modules
module                  stun.so
module                  turn.so
module                  ice.so
#module                 natpmp.so
#module                 pcp.so

# Media encryption modules
module                  srtp.so
#module                 dtls_srtp.so
#module                 zrtp.so


#------------------------------------------------------------------------------
# Temporary Modules (loaded then unloaded)

module_tmp              uuid.so
module_tmp              account.so


#------------------------------------------------------------------------------
# Application Modules

module_app              auloop.so
#module_app             b2bua.so
module_app              contact.so
#module_app             debug_cmd.so
#module_app             echo.so
#module_app             gtk.so
module_app              menu.so
#module_app             mwi.so
#module_app             presence.so
module_app              serreg.so
#module_app             syslog.so
#module_app             mqtt.so
module_app              ctrl_tcp.so
#module_app             vidloop.so
#module_app             ctrl_dbus.so
#module_app             httpreq.so
#module_app             multicast.so


#------------------------------------------------------------------------------
# Module parameters

# DTLS SRTP parameters
#dtls_srtp_use_ec       prime256v1

# UI Modules parameters
cons_listen             127.0.0.1:5555 # cons - Console UI UDP/TCP sockets
http_listen             127.0.0.1:8000 # httpd - HTTP Server
ctrl_tcp_listen         127.0.0.1:4444 # ctrl_tcp - TCP interface JSON
evdev_device            /dev/input/event0

# Opus codec parameters
opus_bitrate            28000 # 6000-510000
#opus_stereo            yes
#opus_sprop_stereo      yes
#opus_cbr               no
#opus_inbandfec         no
#opus_dtx               no
#opus_mirror            no
#opus_complexity        10
#opus_application       audio   # {voip,audio}
#opus_samplerate        48000
#opus_packet_loss       10      # 0-100 percent (expected packet loss)

# Opus Multistream codec parameters
#opus_ms_channels       2       #total channels (2 or 4)
#opus_ms_streams        2       #number of streams
#opus_ms_c_streams      2       #number of coupled streams

vumeter_stderr          yes

#jack_connect_ports     yes

# Selfview
video_selfview          window # {window,pip}
#selfview_size          64x64

# ZRTP
#zrtp_hash              no  # Disable SDP zrtp-hash (not recommended)

# Menu
#redial_attempts        0 # Num or <inf>
#redial_delay           5 # Delay in seconds
#ringback_disabled      no
#statmode_default       off
#menu_clean_number      no
#sip_autoanswer_beep    yes
#sip_autoanswer_method  rfc5373 # {rfc5373,call-info,alert-info}
#ring_aufile            ring.wav
#callwaiting_aufile     callwaiting.wav
#ringback_aufile        ringback.wav
#notfound_aufile        notfound.wav
#busy_aufile            busy.wav
#error_aufile           error.wav
#sip_autoanswer_aufile  autoanswer.wav

# GTK
#gtk_clean_number       no

# avcodec
#avcodec_h264enc        libx264
#avcodec_h264dec        h264
#avcodec_h265enc        libx265
#avcodec_h265dec        hevc
#avcodec_hwaccel        vaapi

# ctrl_dbus
#ctrl_dbus_use  system          # system, session

# mqtt
#mqtt_broker_host       sollentuna.example.com
#mqtt_broker_port       1883
#mqtt_broker_cafile     /path/to/broker-ca.crt  # set this to enforce TLS
#mqtt_broker_clientid   baresip01       # has to be unique
#mqtt_broker_user       user
#mqtt_broker_password   pass
#mqtt_basetopic         baresip/01

# sndfile
#snd_path               /tmp

# EBU ACIP
#ebuacip_jb_type        fixed   # auto,fixed

# HTTP request module
#httpreq_ca             trusted1.pem
#httpreq_ca             trusted2.pem
#httpreq_dns            1.1.1.1
#httpreq_dns            8.8.8.8
#httpreq_hostname       myserver
#httpreq_cert           cert.pem
#httpreq_key            key.pem

# multicast receivers (in priority order)- port number must be even
#multicast_call_prio    0
#multicast_listener     224.0.2.21:50000
#multicast_listener     224.0.2.21:50002

# avformat
#avformat_hwaccel         vaapi
#avformat_inputformat     mjpeg
#avformat_decoder         mjpeg
#avformat_pass_through    yes
#avformat_rtsp_transport  udp

`
