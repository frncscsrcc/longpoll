[![Go Report Card](https://goreportcard.com/badge/github.com/frncscsrcc/longpoll)](https://goreportcard.com/report/github.com/frncscsrcc/longpoll)

# longpoll

Experimental package (use it at your risk).
It implements a simple library to handle longpolls.

A simple server it something similar to:

```
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/frncscsrcc/longpoll"
)

func main() {
  // Create a longpoll instance
	longpoll := longpoll.New()
  
  // Add 3 feeds
	longpoll.AddFeeds([]string{"feed1", "feed2", "feed3"})

  // Simulate new events in those feeds
  go addEvent(longpoll)
  
  // Register the subscription end-point
	http.HandleFunc("/subscribe", longpoll.SubscribeHandler)
	
  // Register the listen end-point
  http.HandleFunc("/listen", longpoll.ListenHandler)
	
  // Start the server
  log.Println("Listening on port 8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// Simulate new events (1 every 20 seconds)
func addEvent(lp *longpoll.LongPoll) {
	for {
		time.Sleep(20 * time.Second)
		type genericObject struct {
			Data1 string
			Data2 string
		}
		lp.NewEvent("feed1", genericObject{"A", "B"})
		lp.NewEvent("feed2", genericObject{"C", "D"})
		lp.NewEvent("feed3", genericObject{"E", "F"})
	}
}

```


