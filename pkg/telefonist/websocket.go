package telefonist

import (
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 8 * 1024 * 1024
)

var (
	newline  = []byte{'\n'}
	upgrader = websocket.Upgrader{
		ReadBufferSize:  maxMessageSize,
		WriteBufferSize: maxMessageSize,
	}
)

type client struct {
	hub  *WsHub
	conn *websocket.Conn
	send chan []byte
}

func (c *client) readPump() {
	defer func() {
		c.hub.unregister <- c
		logWebsocketCloseError(c.conn.Close())
	}()

	go func() {
		<-c.hub.ctx.Done()
		logWebsocketCloseError(c.conn.Close())
	}()

	c.conn.SetReadLimit(maxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Printf("websocket set read deadline error: %v", err)
	}
	c.conn.SetPongHandler(func(string) error {
		if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			log.Printf("websocket set pong read deadline error: %v", err)
			return err
		}
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("websocket read error: %v", err)
			}
			return
		}

		select {
		case c.hub.command <- message:
		case <-c.hub.ctx.Done():
			return
		}
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		logWebsocketCloseError(c.conn.Close())
	}()

	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Printf("websocket set write deadline error: %v", err)
			}
			if !ok {
				if err := c.conn.WriteMessage(websocket.CloseMessage, nil); err != nil {
					log.Printf("websocket close message write error: %v", err)
				}
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}

			if _, err := w.Write(message); err != nil {
				if closeErr := w.Close(); closeErr != nil {
					log.Printf("websocket writer close error after write failure: %v", closeErr)
				}
				log.Printf("websocket write error: %v", err)
				return
			}
			if err := writeQueuedMessages(w, c.send); err != nil {
				if closeErr := w.Close(); closeErr != nil {
					log.Printf("websocket writer close error after queued write failure: %v", closeErr)
				}
				log.Printf("websocket queued write error: %v", err)
				return
			}
			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Printf("websocket set ping write deadline error: %v", err)
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("websocket ping write error: %v", err)
				return
			}

		case <-c.hub.ctx.Done():
			return
		}
	}
}

func writeQueuedMessages(w io.Writer, send chan []byte) error {
	for {
		select {
		case msg, ok := <-send:
			if !ok {
				return nil
			}
			if _, err := w.Write(newline); err != nil {
				return err
			}
			if _, err := w.Write(msg); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func logWebsocketCloseError(err error) {
	if err == nil || errors.Is(err, net.ErrClosed) {
		return
	}
	log.Printf("websocket close error: %v", err)
}

func ServeWs(hub *WsHub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade error: %v", err)
		return
	}

	client := &client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, 8192),
	}
	client.hub.register <- client

	go client.writePump()
	go client.readPump()
}
