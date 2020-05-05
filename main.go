package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/kelseyhightower/envconfig"
	irc "github.com/thoj/go-ircevent"
)

const serverssl = "irc.chat.twitch.tv:6697"

type TwitchConfig struct {
	Nick     string
	Password string
}

//type TwitterConfig struct {
//	ConsumerKey       string `envconfig:"CONSUMER_KEY"`
//	ConsumerSecret    string `envconfig:"CONSUMER_SECRET"`
//	AccessToken       string `envconfig:"ACCESS_TOKEN"`
//	AccessTokenSecret string `envconfig:"ACCESS_TOKEN_SECRET"`
//}

func sse(w http.ResponseWriter, r *http.Request) {
	flusher, _ := w.(http.Flusher)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ircInstance := getIrcConnectionInstance()
	ircInstance.con.AddCallback("PRIVMSG", func(e *irc.Event) {
		format := "data: {\"user\": \"%s\", \"text\": \"%s\", \"platform\": \"%s\"}\n\n"
		fmt.Fprintf(w, format, e.User, e.Arguments[1], "twitch")
		flusher.Flush()

	})
	go ircInstance.con.Loop()
	// go startTwitterHashTagStream("#mogra", w, flusher)

	<-r.Context().Done()
	log.Println("コネクションが閉じました")
}

type IrcConnection struct {
	con *irc.Connection
}

var ircConnectionInstance *IrcConnection = newIrcConnection()

func newIrcConnection() *IrcConnection {
	var config TwitchConfig
	envconfig.Process("TWITCH", &config)

	nick := config.Nick
	con := irc.IRC(nick, nick)

	con.Password = config.Password
	con.UseTLS = true
	con.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	con.AddCallback("001", func(e *irc.Event) { con.Join("#mogra") })
	err := con.Connect(serverssl)
	if err != nil {
		log.Fatal(err)
	}
	return &IrcConnection{con: con}
}

func getIrcConnectionInstance() *IrcConnection {
	return ircConnectionInstance
}

//func startTwitterHashTagStream(hashTag string, w http.ResponseWriter, flusher http.Flusher) {
//	fmt.Println("started startTwitterHashTagStream")
//
//	var c TwitterConfig
//	envconfig.Process("TWITTER", &c)
//	config := oauth1.NewConfig(c.ConsumerKey, c.ConsumerSecret)
//	token := oauth1.NewToken(c.AccessToken, c.AccessTokenSecret)
//	httpClient := config.Client(oauth1.NoContext, token)
//
//	client := twitter.NewClient(httpClient)
//
//	demux := twitter.NewSwitchDemux()
//	demux.Tweet = func(tweet *twitter.Tweet) {
//		format := "data: {\"user\": \"%s\", \"text\": \"%s\", \"platform\": \"%s\"}\n\n"
//		fmt.Fprintf(w, format, tweet.User.ScreenName, tweet.Text, "twitter")
//		flusher.Flush()
//	}
//
//	filterParams := &twitter.StreamFilterParams{Track: []string{hashTag}}
//	stream, err := client.Streams.Filter(filterParams)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	go demux.HandleChan(stream.Messages)
//}

func main() {
	fmt.Println("start main")
	http.HandleFunc("/events", sse)

	port := os.Getenv("PORT")
	if len(port) == 0 {
		port = "5000"
	}
	http.ListenAndServe(":"+port, nil)
}
