package main

import (
	"log"
	"net/http"
	"time"

	"github.com/frncscsrcc/longpoll"
)

func main() {
	longpoll := longpoll.New()
	longpoll.AddFeeds([]string{"feed1", "feed2", "feed3"})
	go addEvent(longpoll)
	http.HandleFunc("/subscribe", longpoll.SubscribeHandler)
	http.HandleFunc("/listen", longpoll.ListenHandler)
	log.Println("Listening on port 8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// Simulate new events (1 every 10 seconds)
func addEvent(lp *longpoll.LongPoll) {
	for {
		time.Sleep(20 * time.Second)
		type genericObject struct {
			A string
			B string
		}
		lp.NewEvent("feed1", genericObject{"A", "B"})
		lp.NewEvent("feed2", genericObject{"C", "D"})
		lp.NewEvent("feed3", genericObject{"E", "F"})
	}
}
