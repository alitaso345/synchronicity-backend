package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/kelseyhightower/envconfig"
	irc "github.com/thoj/go-ircevent"
)

const serverssl = "irc.chat.twitch.tv:6697"

var twitchMessageMap sync.Map

type MessageChannels struct {
	twitch  chan *irc.Event
	twitter chan *twitter.Tweet
}

type TwitchConfig struct {
	Nick     string
	Password string
}

type TwitterConfig struct {
	ConsumerKey       string `envconfig:"CONSUMER_KEY"`
	ConsumerSecret    string `envconfig:"CONSUMER_SECRET"`
	AccessToken       string `envconfig:"ACCESS_TOKEN"`
	AccessTokenSecret string `envconfig:"ACCESS_TOKEN_SECRET"`
}

func main() {
	log.Println("Start main...")

	go startTwitchIrc("#porterrobinson")
	go startTwitterStreaming("#SecretSky")

	http.HandleFunc("/events", sse)

	port := os.Getenv("PORT")
	if len(port) == 0 {
		port = "5000"
	}
	http.ListenAndServe(":"+port, nil)
}

func startTwitchIrc(channelName string) {
	if channelName == "" {
		return
	}

	var config TwitchConfig
	envconfig.Process("TWITCH", &config)

	nick := config.Nick
	con := irc.IRC(nick, nick)

	con.Password = config.Password
	con.UseTLS = true
	con.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	con.AddCallback("001", func(e *irc.Event) { con.Join(channelName) })
	con.AddCallback("PRIVMSG", func(e *irc.Event) {
		twitchMessageMap.Range(func(key, val interface{}) bool {
			ch, ok := val.(MessageChannels)
			if ok {
				ch.twitch <- e
			} else {
				log.Fatalf("Error %v", e)
			}
			return true
		})
	})
	err := con.Connect(serverssl)
	if err != nil {
		fmt.Printf("Err %s", err)
		return
	}

	con.Loop()
}

func startTwitterStreaming(hashTag string) {
	if hashTag == "" {
		return
	}

	var c TwitterConfig
	envconfig.Process("TWITTER", &c)
	config := oauth1.NewConfig(c.ConsumerKey, c.ConsumerSecret)
	token := oauth1.NewToken(c.AccessToken, c.AccessTokenSecret)
	httpClient := config.Client(oauth1.NoContext, token)

	client := twitter.NewClient(httpClient)

	demux := twitter.NewSwitchDemux()
	demux.Tweet = func(tweet *twitter.Tweet) {
		twitchMessageMap.Range(func(key, val interface{}) bool {
			ch, ok := val.(MessageChannels)
			if ok {
				ch.twitter <- tweet
			} else {
				log.Fatalf("Error %v", tweet)
			}
			return true
		})
	}

	filterParams := &twitter.StreamFilterParams{Track: []string{hashTag}}
	stream, err := client.Streams.Filter(filterParams)
	if err != nil {
		log.Fatal(err)
	}

	demux.HandleChan(stream.Messages)
}

func sse(w http.ResponseWriter, r *http.Request) {
	log.Printf("Start sse for %v\n", &w)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	messageChannels := MessageChannels{twitch: make(chan *irc.Event), twitter: make(chan *twitter.Tweet)}
	twitchMessageMap.Store(&w, messageChannels)

	flusher, _ := w.(http.Flusher)
	format := "data: {\"user\": \"%s\", \"text\": \"%s\", \"platform\": \"%s\"}\n\n"
	ctx := r.Context()

loop:
	for {
		select {
		case <-ctx.Done():
			log.Println("イベントの送信を終了します")
			break loop
		case msg := <-messageChannels.twitch:
			fmt.Fprintf(w, format, msg.User, msg.Arguments[1], "twitch")
			flusher.Flush()
		case msg := <-messageChannels.twitter:
			replacedMsg := strings.ReplaceAll(msg.Text, "\n", "")
			replacedMsg = strings.ReplaceAll(replacedMsg, "\r", "")
			fmt.Fprintf(w, format, msg.User.ScreenName, replacedMsg, "twitter")
			flusher.Flush()
		}
	}
	twitchMessageMap.Delete(&w)
}
