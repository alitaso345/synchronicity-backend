package main

import (
	"crypto/tls"
	"fmt"
	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/kelseyhightower/envconfig"
	irc "github.com/thoj/go-ircevent"
	"log"
	"net/http"
	"os"
	"strings"
)

const serverssl = "irc.chat.twitch.tv:6697"

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

var twitchMessages chan *irc.Event = make(chan *irc.Event)
var twitterMessages chan *twitter.Tweet = make(chan *twitter.Tweet)

func main() {
	log.Println("Start main...")

	go startTwitchIrc("#aloonity")
	go startTwitterStreaming("#ガルパピコ大盛り_01")

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
		twitchMessages <- e
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
		twitterMessages <- tweet
	}

	filterParams := &twitter.StreamFilterParams{Track: []string{hashTag}}
	stream, err := client.Streams.Filter(filterParams)
	if err != nil {
		log.Fatal(err)
	}

	demux.HandleChan(stream.Messages)
}

func sse(w http.ResponseWriter, r *http.Request) {
	log.Println("Start sse...")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	quit := make(chan bool)
	flusher, _ := w.(http.Flusher)
	format := "data: {\"user\": \"%s\", \"text\": \"%s\", \"platform\": \"%s\"}\n\n"
	ctx := r.Context()
	
	go func() {
		loop:
			for {
				select {
				case <-ctx.Done():
					log.Println("クライアント/サーバ間のコネクションが閉じました")
					quit <- true
					break loop
				case msg := <-twitchMessages:
					log.Println(msg.Arguments[1])
					fmt.Fprintf(w, format, msg.User, msg.Arguments[1], "twitch")
					flusher.Flush()
				case msg := <-twitterMessages:
					replacedMsg := strings.ReplaceAll(msg.Text, "\n", "")
					replacedMsg = strings.ReplaceAll(replacedMsg, "\r", "")

					log.Println(replacedMsg)
					fmt.Fprintf(w, format, msg.User.ScreenName, replacedMsg, "twitter")
					flusher.Flush()
				}
			}
	}()

	<-quit
}

