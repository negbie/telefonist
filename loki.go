package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type jsonValue [2]string

type jsonStream struct {
	Stream map[string]string `json:"stream"`
	Values []jsonValue       `json:"values"`
}

type jsonMessage struct {
	Streams []jsonStream `json:"streams"`
}

type LokiClient struct {
	url            string
	endpoints      Endpoints
	currentMessage jsonMessage
	streams        chan *jsonStream
	quit           chan struct{}
	maxBatch       int
	maxWaitTime    time.Duration
	wg             sync.WaitGroup
}

type Message struct {
	Message string
	Time    string
}

type Endpoints struct {
	push  string
	query string
	ready string
}

// NewLokiClient creates a new loki client
// The client runs in a goroutine and sends the data either
// once it reaches the maxBatch or when it waited for maxWaitTime
//
// the batch counter is incremented every time add is called
// maxWaitTime uses nanoseconds
func NewLokiClient(url string, maxBatch int, maxWaitSeconds int) (*LokiClient, error) {
	client := LokiClient{
		url:         url,
		maxBatch:    maxBatch,
		maxWaitTime: time.Duration(maxWaitSeconds * int(time.Second)),
		quit:        make(chan struct{}),
		streams:     make(chan *jsonStream),
	}

	client.endpoints.push = "/loki/api/v1/push"
	client.endpoints.query = "/loki/api/v1/query"
	client.endpoints.ready = "/ready"

	_, err := http.Get(client.url + client.endpoints.ready)
	if err != nil {
		return &client, err
	}

	client.wg.Add(1)
	go client.run()
	return &client, nil
}

func (client *LokiClient) Close() {
	close(client.quit)
	client.wg.Wait()
	close(client.streams)
}

func (client *LokiClient) run() {
	batchCounter := 0
	maxWait := time.NewTimer(client.maxWaitTime)
	defer maxWait.Stop()

	defer func() {
		if batchCounter > 0 {
			err := client.send()
			if err != nil {
				log.Println(err)
			}
		}
		client.wg.Done()
	}()

	for {
		select {
		case <-client.quit:
			return
		case stream := <-client.streams:
			client.currentMessage.Streams = append(client.currentMessage.Streams, *stream)
			batchCounter++
			if batchCounter == client.maxBatch {
				err := client.send()
				if err != nil {
					log.Println(err)
				}
				batchCounter = 0
				client.currentMessage.Streams = []jsonStream{}
				maxWait.Reset(client.maxWaitTime)
			}
		case <-maxWait.C:
			if batchCounter > 0 {
				err := client.send()
				if err != nil {
					log.Println(err)
				}
				client.currentMessage.Streams = []jsonStream{}
				batchCounter = 0
			}
			maxWait.Reset(client.maxWaitTime)
		}
	}
}

// The template for the message sent to Loki is:
//{
//  "streams": [
//    {
//      "stream": {
//        "label": "value"
//      },
//      "values": [
//          [ "<unix epoch in nanoseconds>", "<log line>" ],
//          [ "<unix epoch in nanoseconds>", "<log line>" ]
//      ]
//    }
//  ]
//}

// AddStream adds another stream to be sent with the next batch
func (client *LokiClient) AddStream(labels map[string]string, messages []Message) {
	var vals []jsonValue
	for i := range messages {
		var val jsonValue
		val[0] = messages[i].Time
		val[1] = messages[i].Message
		vals = append(vals, val)
	}
	stream := jsonStream{
		Stream: labels,
		Values: vals,
	}
	client.streams <- &stream
}

func (client *LokiClient) Send(labels map[string]string, text string) {

	msg := Message{
		Time:    strconv.FormatInt(time.Now().UnixNano(), 10),
		Message: text,
	}

	msgs := []Message{msg}
	client.AddStream(labels, msgs)
}

// send JSON encoded messages to loki
func (client *LokiClient) send() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	str, err := json.Marshal(client.currentMessage)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", client.url+client.endpoints.push, bytes.NewReader(str))
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, 1024))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = fmt.Errorf("server returned HTTP status %s (%d): %s", resp.Status, resp.StatusCode, line)
	}
	return err
}
