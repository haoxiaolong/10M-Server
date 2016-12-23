package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/gorilla/websocket"
)

const (
	maxMessageSize = 256
	pingPeriod     = 5 * time.Minute
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  16,
	WriteBufferSize: maxMessageSize,
}

var (
	subscriptions      = map[string][]chan []byte{}
	subscriptionsMutex sync.Mutex
)

var (
	connected int64
	failed    int64
)

func main() {
	go func() {
		start := time.Now()
		for {
			fmt.Printf("server elapsed=%0.0fs connected=%d failed=%d\n", time.Now().Sub(start).Seconds(), atomic.LoadInt64(&connected), atomic.LoadInt64(&failed))
			time.Sleep(1 * time.Second)
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		channel := r.URL.Path[1:]
		// launch a new goroutine so that this function can return and the http server can free up
		// buffers associated with this connection
		go handleConnection(ws, channel)
	})

	for i := 0; i < 1000; i++ {
		i := i
		go func() {
			if err := http.ListenAndServe(":"+strconv.Itoa(10000+i), nil); err != nil {
				log.Fatal(err)
			}
		}()
	}

	receiveRedisMessages()
}

func handleConnection(ws *websocket.Conn, channel string) {
	sub := subscribe(channel)
	atomic.AddInt64(&connected, 1)
	t := time.NewTicker(pingPeriod)

	var message []byte

	for {
		select {
		case <-t.C:
			message = nil
		case message = <-sub:
		}

		ws.SetWriteDeadline(time.Now().Add(30 * time.Second))
		err := ws.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			break
		}
	}
	atomic.AddInt64(&connected, -1)
	atomic.AddInt64(&failed, 1)

	t.Stop()
	ws.Close()
	unsubscribe(channel, sub)
}

func receiveRedisMessages() {
	c, err := redis.Dial("tcp", ":6379")
	if err != nil {
		log.Fatal(err)
	}

	psc := redis.PubSubConn{c}
	if err := psc.PSubscribe("*"); err != nil {
		log.Fatal(err)
	}

	for {
		switch v := psc.Receive().(type) {
		case redis.PMessage:
			subscriptionsMutex.Lock()
			subs := subscriptions[v.Channel]
			subscriptionsMutex.Unlock()
			for _, s := range subs {
				select {
				case s <- v.Data:
				default:
					// drop the message if nobody is ready to receive it
				}
			}
		case error:
			log.Fatal(err)
		}
	}
}

func subscribe(channel string) chan []byte {
	sub := make(chan []byte)
	subscriptionsMutex.Lock()
	subscriptions[channel] = append(subscriptions[channel], sub)
	subscriptionsMutex.Unlock()
	return sub
}

func unsubscribe(channel string, sub chan []byte) {
	subscriptionsMutex.Lock()
	newSubs := []chan []byte{}
	subs := subscriptions[channel]
	for _, s := range subs {
		if s != sub {
			newSubs = append(newSubs, s)
		}
	}
	subscriptions[channel] = newSubs
	subscriptionsMutex.Unlock()
}
