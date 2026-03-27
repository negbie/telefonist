package gobaresip

import (
	"context"
	"time"
)

// SetOption takes one or more option function and applies them in order to Baresip.
func (b *Baresip) SetOption(options ...func(*Baresip) error) error {
	for _, opt := range options {
		if err := opt(b); err != nil {
			return err
		}
	}
	return nil
}

// SetCtrlTCPAddr sets the ctrl_tcp modules address.
func SetCtrlTCPAddr(opt string) func(*Baresip) error {
	return func(b *Baresip) error {
		b.ctrlAddr = opt
		return nil
	}
}

// SetConfigPath sets the config path.
func SetConfigPath(opt string) func(*Baresip) error {
	return func(b *Baresip) error {
		b.configPath = opt
		return nil
	}
}

// SetAudioPath sets the audio path.
func SetAudioPath(opt string) func(*Baresip) error {
	return func(b *Baresip) error {
		b.audioPath = opt
		return nil
	}
}

// SetUserAgent sets the UserAgent.
func SetUserAgent(opt string) func(*Baresip) error {
	return func(b *Baresip) error {
		b.userAgent = opt
		return nil
	}
}

// SetContext sets the context for cancellation and deadline propagation.
// If not set, context.Background() is used.
func SetContext(ctx context.Context) func(*Baresip) error {
	return func(b *Baresip) error {
		b.ctx = ctx
		return nil
	}
}

// SetReconnect enables automatic reconnection to ctrl_tcp with
// the given initial backoff duration. Backoff grows with factor 1.5
// up to a maximum of 30s. Pass 0 to use the default backoff of 2s.
func SetReconnect(backoff time.Duration) func(*Baresip) error {
	return func(b *Baresip) error {
		b.reconnect = true
		if backoff > 0 {
			b.reconnectBackoff = backoff
		}
		return nil
	}
}

// SetRemote sets the baresip instance to remote mode.
func SetRemote(opt bool) func(*Baresip) error {
	return func(b *Baresip) error {
		b.remote = opt
		return nil
	}
}

// SetMsgRecvHandler sets the message receive handler.
func SetMsgRecvHandler(handler func(Msg)) func(*Baresip) error {
	return func(b *Baresip) error {
		b.msgRecvHandler = handler
		return nil
	}
}
