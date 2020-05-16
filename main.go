package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
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

var messageMap sync.Map

var isChanged chan bool = make(chan bool)
var isChangedTwitchChannel chan bool = make(chan bool)

var hashTag string = "#mogra"
var twitchChannel string = "#mogra"
var isDisplayRT bool = false

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

	go startTwitchIrc()
	go startTwitterStreaming()

	http.HandleFunc("/events", sse)
	http.HandleFunc("/settings", settings)

	port := os.Getenv("PORT")
	if len(port) == 0 {
		port = "5000"
	}
	http.ListenAndServe(":"+port, nil)
}

func changeHashTag(newHashTag string) {
	hashTag = newHashTag
	isChanged <- true
}

func changeChannel(newChannel string) {
	twitchChannel = newChannel
	isChangedTwitchChannel <- true
}

func changeIsDisplayRT(newIsDisplayRT bool) {
	isDisplayRT = newIsDisplayRT
}

func startTwitchIrc() {
	var config TwitchConfig
	envconfig.Process("TWITCH", &config)

	nick := config.Nick

	for {
		fmt.Printf("接続するTwitch IRCは %s です\n", twitchChannel)

		con := irc.IRC(nick, nick)

		con.Password = config.Password
		con.UseTLS = true
		con.TLSConfig = &tls.Config{InsecureSkipVerify: true}

		con.AddCallback("001", func(e *irc.Event) { con.Join(twitchChannel) })
		con.AddCallback("PRIVMSG", func(e *irc.Event) {
			messageMap.Range(func(key, val interface{}) bool {
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
		go con.Loop()

		<-isChangedTwitchChannel
		con.ClearCallback("001")
		con.ClearCallback("PRIVMSG")
		con.Quit()
		log.Println("Twitch IRCに再接続します")
	}
}

func startTwitterStreaming() {
	var c TwitterConfig
	envconfig.Process("TWITTER", &c)
	config := oauth1.NewConfig(c.ConsumerKey, c.ConsumerSecret)
	token := oauth1.NewToken(c.AccessToken, c.AccessTokenSecret)
	httpClient := config.Client(oauth1.NoContext, token)

	client := twitter.NewClient(httpClient)

	demux := twitter.NewSwitchDemux()
	demux.Tweet = func(tweet *twitter.Tweet) {
		messageMap.Range(func(key, val interface{}) bool {
			ch, ok := val.(MessageChannels)
			if ok {
				if !isDisplayRT && tweet.RetweetedStatus != nil {
					return true
				}

				ch.twitter <- tweet
			} else {
				log.Fatalf("Error %v", tweet)
			}
			return true
		})
	}

	for {
		fmt.Printf("取得するハッシュタグは %s です\n", hashTag)
		filterParams := &twitter.StreamFilterParams{Track: []string{hashTag}}
		stream, err := client.Streams.Filter(filterParams)
		if err != nil {
			log.Fatal(err)
		}

		go demux.HandleChan(stream.Messages)

		<-isChanged
		stream.Stop()
		fmt.Println("Twitter Streaming APIを再起動します")
	}
}

func sse(w http.ResponseWriter, r *http.Request) {
	log.Printf("Start sse for %v\n", &w)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	messageChannels := MessageChannels{twitch: make(chan *irc.Event), twitter: make(chan *twitter.Tweet)}
	messageMap.Store(&w, messageChannels)

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
	messageMap.Delete(&w)
}

type SettingsRequest struct {
	HashTag     string `json:"hashTag"`
	Channel     string `json:"channel"`
	IsDisplayRT bool   `json:"isDisplayRT"`
}

func settings(w http.ResponseWriter, r *http.Request) {
	log.Println("settings")

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, PUT")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		setting := SettingsRequest{HashTag: hashTag, Channel: twitchChannel, IsDisplayRT: isDisplayRT}
		json, _ := json.Marshal(setting)
		w.Write(json)

		w.WriteHeader(http.StatusOK)
	case http.MethodPut:
		body := r.Body
		defer body.Close()

		buf := new(bytes.Buffer)
		io.Copy(buf, body)

		var request SettingsRequest
		json.Unmarshal(buf.Bytes(), &request)

		if hashTag != request.HashTag {
			changeHashTag(request.HashTag)
		}
		if twitchChannel != request.Channel {
			changeChannel(request.Channel)
		}
		if isDisplayRT != request.IsDisplayRT {
			changeIsDisplayRT(request.IsDisplayRT)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
