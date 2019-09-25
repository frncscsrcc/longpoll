package longpoll

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

var seededRand *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))

type clientExist map[string]bool
type feedToClients map[string]clientExist
type event struct {
	Data      interface{}
	Feed      string
	Timestamp int32
}
type events map[int]event
type clientToNewEvents map[string][]int
type clientToConnection map[string]int
type connectionChannel map[int]chan string

var globalFeedToClients feedToClients
var globalClients clientExist
var globalEvents events
var globalClientToNewEvents clientToNewEvents

var globalConnectionChannel connectionChannel
var globalClientToConnection clientToConnection
var globalLastConnection int

type LongPoll struct {
	globalClients            clientExist
	globalEvents             events
	globalClientToNewEvents  clientToNewEvents
	globalFeedToClients      feedToClients
	globalClientToConnection clientToConnection
	globalConnectionChannel  connectionChannel
	globalLastConnection     int
}

type NewOptions struct {
	Feeds []string
}

func New(opt NewOptions) *LongPoll {
	lp := LongPoll{
		globalClients:            make(clientExist),
		globalEvents:             make(events),
		globalClientToNewEvents:  make(clientToNewEvents),
		globalFeedToClients:      make(map[string]clientExist),
		globalClientToConnection: make(clientToConnection),
		globalConnectionChannel:  make(connectionChannel),
	}

	if opt.Feeds != nil && len(opt.Feeds) > 0 {
		for _, feed := range opt.Feeds {
			lp.globalFeedToClients[feed] = make(clientExist)
		}
	}

	return &lp
}

type SubscriptionResponse struct {
	Token string
	Feeds []string
}

type EventResponse struct {
	Events []event
}

type ErrorResponse struct {
	Code    int
	Message string
}

func (lp *LongPoll) SubscribeHandler(w http.ResponseWriter, r *http.Request) {
	feeds, ok := r.URL.Query()["feed"]
	if ok != true {
		sendError(w, 400, "Missing feed")
		return
	}
	token := GetNewToken(32)

	// Client is not pending
	lp.globalClients[token] = false

	// Feeds validation
	for _, feed := range feeds {
		if _, ok := lp.globalFeedToClients[feed]; ok == false {
			sendError(w, 500, fmt.Sprintf("Feed %s is not available", feed))
			return
		}
	}
	// Client subscription
	for _, feed := range feeds {
		lp.globalFeedToClients[feed][token] = true
	}

	sendResponse(w, SubscriptionResponse{token, feeds})
}

func (lp *LongPoll) ListenHandler(w http.ResponseWriter, r *http.Request) {
	tokens, ok := r.URL.Query()["token"]
	if ok != true {
		sendError(w, 400, "Missing token")
		return
	}
	token := tokens[0]

	// Check if token exists
	if _, clientExists := lp.globalClients[token]; clientExists == false {
		sendError(w, 401, "Unauthorized")
		return
	}

	fmt.Printf("Received request from %s\n", token)

	// Protect with mutex
	lp.globalLastConnection = lp.globalLastConnection + 1
	currentConnection := lp.globalLastConnection
	// Check if there is a previous listen connection, in this case
	if previousConnectionIndex, ok := lp.globalClientToConnection[token]; ok == true {
		// Send a ABORT signal to previous connection
		fmt.Printf("Closing previous connection (%d) from the same client (%s)\n", previousConnectionIndex, token)
		fmt.Printf("%+v\n", lp.globalConnectionChannel[previousConnectionIndex])
		lp.globalConnectionChannel[previousConnectionIndex] <- "ABORT"
		fmt.Printf("Closed previous connection (%d) from the same client (%s)\n", previousConnectionIndex, token)
	}

	// Save the active connection for this client
	lp.globalClientToConnection[token] = currentConnection

	// Create a comunication channel to receive async events
	comunicationChannel := make(chan string)
	lp.globalConnectionChannel[currentConnection] = comunicationChannel

	// If they are no event, wait for the next one
	if len(lp.globalClientToNewEvents[token]) == 0 {
		// Client is pending
		lp.globalClients[token] = true

		// Set a timeout every 5 seconds
		go lp.notifyTimeout(comunicationChannel, 5)

		fmt.Printf("Client %s (%d) waits for connection\n", token, currentConnection)
		operation := <-comunicationChannel
		fmt.Printf("Client %s (%d) received signal %s\n", token, currentConnection, operation)

		// Another connection from the same client, this one should be disharged
		if operation == "ABORT" {
			sendError(w, 204, "Connection aborted")
			fmt.Printf("Sent abort signal to %s (%d)\n", token, currentConnection)
			return
		}
		// Timeout
		if operation == "TIMEOUT" {
			sendError(w, 408, "Request timeout")
			fmt.Printf("Sent timeout signal to %s (%d)\n", token, currentConnection)
			// Delete the connection, or next client will try to closed this one
			// but it does not exist anymore and it would lock
			delete(lp.globalClientToConnection, token)
			return
		}
	}

	// Fetch the events
	var eventResponse EventResponse
	eventResponse.Events = make([]event, 0)
	for _, eventID := range lp.globalClientToNewEvents[token] {
		eventResponse.Events = append(eventResponse.Events, lp.globalEvents[eventID])
	}

	// Clean the event list
	lp.globalClientToNewEvents[token] = make([]int, 0)

	sendResponse(w, eventResponse)
	delete(lp.globalClientToConnection, token)

}

func (lp *LongPoll) NewEvent(feed string, object interface{}) error {
	newIndex := len(lp.globalEvents)
	lp.globalEvents[newIndex] = event{
		Feed:      feed,
		Data:      object,
		Timestamp: int32(time.Now().Unix()),
	}

	// Find listening clients
	waitingClients := make(map[string]bool)
	for client, _ := range lp.globalFeedToClients[feed] {
		lp.globalClientToNewEvents[client] = append(lp.globalClientToNewEvents[client], newIndex)
		waitingClients[client] = true
	}

	for client, _ := range waitingClients {
		go lp.notifyEvent(client)
	}

	return nil
}

func (lp *LongPoll) notifyEvent(client string) {
	if lp.globalClients[client] == true {
		connection, ok := lp.globalClientToConnection[client]
		if ok != true {
			return
		}
		lp.globalConnectionChannel[connection] <- "DONE"
		lp.globalClients[client] = false
	}
}

func (lp *LongPoll) notifyTimeout(comunicationChanel chan string, seconds int) {
	time.Sleep(time.Duration(seconds) * time.Second)
	comunicationChanel <- "TIMEOUT"
}

func sendError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json, err := toJSON(ErrorResponse{code, message})
	if err != nil {
		sendError(w, 500, err.Error())
		return
	}
	fmt.Fprintf(w, json)
}

func sendResponse(w http.ResponseWriter, object interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	json, err := toJSON(object)
	if err != nil {
		sendError(w, 500, err.Error())
		return
	}
	fmt.Fprintf(w, json)
}

func toJSON(object interface{}) (string, error) {
	json, err := json.Marshal(object)
	if err != nil {
		return "", err
	}
	return string(json), nil
}

func GetNewToken(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}
