package gobaresip

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/goccy/go-json"
)

// ErrIgnoredCommand is returned when a command is silently ignored
// (e.g. empty input or "quit" via CmdWs).
var ErrIgnoredCommand = errors.New("command ignored")

/*
  /about                           About box
  /accept                 a        Accept incoming call
  /acceptdir ..                    Accept incoming call with audio and videodirection.
  /answermode ..                   Set answer mode
  /aubitrate ..                    Set audio bitrate
  /audio_debug            A        Audio stream
  /auplay ..                       Switch audio player
  /ausrc ..                        Switch audio source
  /callfind ..                     Find call
  /callstat               c        Call status
  /contact_next           >        Set next contact
  /contact_prev           <        Set previous contact
  /contacts               C        List contacts
  /dial ..                d ..     Dial
  /dialcontact            D        Dial current contact
  /dialdir ..                      Dial with audio and videodirection.
  /dnd ..                          Set Do not Disturb
  /hangup                 b        Hangup call
  /hangupall ..                    Hangup all calls with direction
  /help                   h        Help menu
  /hold                   x        Call hold
  /insmod ..                       Load module
  /line ..                @ ..     Set current call
  /listcalls              l        List active calls
  /medialdir ..                    Set local media direction
  /message ..             M ..     Message current contact
  /mute                   m        Call mute/un-mute
  /options ..             o ..     Options
  /quit                   q        Quit
  /reginfo                r        Registration info
  /reinvite               I        Send re-INVITE
  /resume                 X        Call resume
  /rmmod ..                        Unload module
  /setadelay ..                    Set answer delay for outgoing call
  /sndcode ..                      Send Code
  /statmode               S        Statusmode toggle
  /tlsissuer                       TLS certificate issuer
  /tlssubject                      TLS certificate subject
  /transfer ..            t ..     Transfer call
  /uadel ..                        Delete User-Agent
  /uadelall ..                     Delete all User-Agents
  /uafind ..                       Find User-Agent
  /uanew ..                        Create User-Agent
  /uareg ..                        UA register  [index]
  /video_debug            V        Video stream
  /videodir ..                     Set video direction
  /vidsrc ..                       Switch video source
*/

// CommandMsg struct for ctrl_tcp
type CommandMsg struct {
	Command string `json:"command,omitempty"`
	Params  string `json:"params,omitempty"`
	Token   string `json:"token,omitempty"`
}

func buildCommand(command, params, token string) *CommandMsg {
	return &CommandMsg{
		Command: command,
		Params:  params,
		Token:   token,
	}
}

// Cmd will send a raw baresip command over ctrl_tcp.
func (b *Baresip) Cmd(command, params, token string) error {
	if command == "dial" {
		params = stripSIP(params)
	}

	msg, err := json.Marshal(buildCommand(command, params, token))
	if err != nil {
		return err
	}

	b.writeMu.Lock()
	defer b.writeMu.Unlock()

	if atomic.LoadUint32(&b.ctrlConnAlive) == 0 {
		return fmt.Errorf("can't write command to closed tcp_ctrl connection")
	}

	// Build netstring without fmt.Sprintf: "<len>:<msg>,"
	lenStr := strconv.Itoa(len(msg))
	buf := make([]byte, 0, len(lenStr)+1+len(msg)+1)
	buf = append(buf, lenStr...)
	buf = append(buf, ':')
	buf = append(buf, msg...)
	buf = append(buf, ',')

	deadline := time.Now().Add(2 * time.Second)
	if b.ctx != nil {
		if d, ok := b.ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
	}
	b.ctrlConn.SetWriteDeadline(deadline)
	_, err = b.ctrlConn.Write(buf)
	return err
}

func stripSIP(s string) string {
	if s == "" {
		return s
	}

	s = strings.ReplaceAll(s, "sip:", "")

	// Find first '@' and truncate there
	if i := strings.IndexByte(s, '@'); i >= 0 {
		s = s[:i]
	}
	return s
}

// CmdAccept will accept incoming call
func (b *Baresip) CmdAccept() error {
	c := "accept"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdAcceptdir will accept incoming call with audio and videodirection.
func (b *Baresip) CmdAcceptdir(s string) error {
	c := "acceptdir"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdAnswermode will set answer mode
func (b *Baresip) CmdAnswermode(s string) error {
	c := "answermode"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdAuplay will switch audio player
func (b *Baresip) CmdAuplay(s string) error {
	c := "auplay"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdAusrc will switch audio source
func (b *Baresip) CmdAusrc(s string) error {
	c := "ausrc"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdCallstat will show call status
func (b *Baresip) CmdCallstat() error {
	c := "callstat"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdContactNext will set next contact
func (b *Baresip) CmdContactNext() error {
	c := "contact_next"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdContactPrev will set previous contact
func (b *Baresip) CmdContactPrev() error {
	c := "contact_prev"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdDial will dial number
func (b *Baresip) CmdDial(s string) error {
	c := "dial"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdDialcontact will dial current contact
func (b *Baresip) CmdDialcontact() error {
	c := "dialcontact"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdDialdir will dial with audio and videodirection
func (b *Baresip) CmdDialdir(s string) error {
	c := "dialdir"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdHangup will hangup call
func (b *Baresip) CmdHangup() error {
	c := "hangup"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdHangupID will hangup call with Call-ID
func (b *Baresip) CmdHangupID(callID string) error {
	c := "hangup"
	return b.Cmd(c, callID, "cmd_"+c+"_"+callID)
}

// CmdHangupall will hangup all calls with direction
func (b *Baresip) CmdHangupall(s string) error {
	c := "hangupall"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdInsmod will load module
func (b *Baresip) CmdInsmod(s string) error {
	c := "insmod"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdListcalls will list active calls
func (b *Baresip) CmdListcalls() error {
	c := "listcalls"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdReginfo will list registration info
func (b *Baresip) CmdReginfo() error {
	c := "reginfo"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdRmmod will unload module
func (b *Baresip) CmdRmmod(s string) error {
	c := "rmmod"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdSetadelay will set answer delay for outgoing call
func (b *Baresip) CmdSetadelay(n int) error {
	c := "setadelay"
	return b.Cmd(c, strconv.Itoa(n), "cmd_"+c+"_"+strconv.Itoa(n))
}

// CmdUadel will delete User-Agent
func (b *Baresip) CmdUadel(s string) error {
	c := "uadel"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdUadelall will delete all User-Agents
func (b *Baresip) CmdUadelall() error {
	c := "uadelall"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdUafind will find User-Agent <aor>
func (b *Baresip) CmdUafind(s string) error {
	c := "uafind"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdUanew will create User-Agent
func (b *Baresip) CmdUanew(s string) error {
	c := "uanew"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdUareg will register <regint> [index]
func (b *Baresip) CmdUareg(s string) error {
	c := "uareg"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdQuit will quit baresip
func (b *Baresip) CmdQuit() error {
	c := "quit"
	return b.Cmd(c, "", "cmd_"+c)
}

func (b *Baresip) CmdWs(raw []byte) error {
	m := strings.SplitN(string(bytes.TrimSpace(bytes.Join(bytes.Fields(raw), []byte(" ")))), " ", 2)
	if len(m) < 1 || m[0] == "" {
		return ErrIgnoredCommand
	}

	m[0] = strings.ToLower(m[0])
	if m[0] == "quit" {
		return ErrIgnoredCommand
	}

	if len(m) == 1 {
		return b.Cmd(m[0], "", "cmd_"+m[0])
	}
	return b.Cmd(m[0], m[1], "cmd_"+m[0]+"_"+m[1])
}
