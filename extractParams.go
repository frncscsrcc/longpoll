package longpoll

import "net/http"

// ContextStruct is a struct that could be used to inject parameters in the
// client request
type ContextStruct struct {
	SubscriptionID string
	Feeds          []string
	SessionID      string
}

func getFeeds(r *http.Request) (feeds []string) {
	var ok bool

	// Search in the context
	contextStruct, assertOK := r.Context().Value(ContextStructIdentifier).(ContextStruct)
	if assertOK && len(contextStruct.Feeds) > 0 {
		return contextStruct.Feeds
	}

	// Search in URL
	feeds, ok = r.URL.Query()["feed"]
	if ok == true {
		return feeds
	}
	return feeds

	// Search in body
	// TODO
}

func getSubscriptionID(r *http.Request) (subscriptionID string) {
	var ok bool

	// Search in the context
	contextStruct, assertOK := r.Context().Value("contextStruct").(ContextStruct)
	if assertOK && len(contextStruct.SubscriptionID) > 0 {
		return contextStruct.SubscriptionID
	}

	// Search in URL
	subscriptionIDs, ok := r.URL.Query()["subscriptionID"]
	if ok == true && len(subscriptionIDs) > 0 {
		return subscriptionIDs[0]
	}
	return subscriptionID

	// Search in body
	// TODO
}
