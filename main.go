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

func sse(w http.ResponseWriter, r *http.Request) {
	fmt.Println("start sse...")
	flusher, _ := w.(http.Flusher)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Twitchのストリーム処理
	//ircInstance := getIrcConnectionInstance()
	//ircInstance.con.AddCallback("PRIVMSG", func(e *irc.Event) {
	//	format := "data: {\"user\": \"%s\", \"text\": \"%s\", \"platform\": \"%s\"}\n\n"
	//	fmt.Fprintf(w, format, e.User, e.Arguments[1], "twitch")
	//	flusher.Flush()
	//
	//})
	//go ircInstance.con.Loop()


	// Twitterのストリーム処理
	streamInstance := getTwitterStreamConnectionInstance()
	streamInstance.demux.Tweet = func(tweet *twitter.Tweet) {
		format := "data: {\"user\": \"%s\", \"text\": \"%s\", \"platform\": \"%s\"}\n\n"
		var replacedTweet string

		if strings.Contains(tweet.Text, "RT") { return }

		replacedTweet = strings.ReplaceAll(tweet.Text, "\n", " ")
		replacedTweet = strings.ReplaceAll(replacedTweet, "\r", " ")

		fmt.Println(replacedTweet)

		fmt.Fprintf(w, format, tweet.User.ScreenName, replacedTweet, "twitter")
		flusher.Flush()
	}
	go streamInstance.demux.HandleChan(streamInstance.con.Messages)

	<-r.Context().Done()

	// Callbackの初期化。これがないとリロード時に同じCallbackが複数登録されてしまう。
	//ircInstance.con.ClearCallback("PRIVMSG")
	log.Println("コネクションが閉じました")
}

type IrcConnection struct {
	con *irc.Connection
}

var ircConnectionInstance *IrcConnection = newIrcConnection()

func newIrcConnection() *IrcConnection {
	fmt.Println("IRCコネクション作成")

	var config TwitchConfig
	envconfig.Process("TWITCH", &config)

	nick := config.Nick
	con := irc.IRC(nick, nick)

	con.Password = config.Password
	con.UseTLS = true
	con.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	con.AddCallback("001", func(e *irc.Event) { con.Join("#viva_h") })
	err := con.Connect(serverssl)
	if err != nil {
		log.Fatal(err)
	}
	return &IrcConnection{con: con}
}

func getIrcConnectionInstance() *IrcConnection {
	return ircConnectionInstance
}

type TwitterConnection struct {
	con *twitter.Stream
	demux twitter.SwitchDemux
}

var TwitterConnectionInstance *TwitterConnection = newTwitterStreamConnection()

func newTwitterStreamConnection() *TwitterConnection {
	fmt.Println("TwitterStreamコネクション作成")

	var c TwitterConfig
	envconfig.Process("TWITTER", &c)
	config := oauth1.NewConfig(c.ConsumerKey, c.ConsumerSecret)
	token := oauth1.NewToken(c.AccessToken, c.AccessTokenSecret)
	httpClient := config.Client(oauth1.NoContext, token)

	client := twitter.NewClient(httpClient)

	filterParams := &twitter.StreamFilterParams{Track: []string{"#恋つづ"}}
	stream, err := client.Streams.Filter(filterParams)
	if err != nil {
		log.Fatal(err)
	}

	return &TwitterConnection{con: stream, demux: twitter.NewSwitchDemux()}
}

func getTwitterStreamConnectionInstance() *TwitterConnection {
	return TwitterConnectionInstance
}

func main() {
	fmt.Println("start main")
	http.HandleFunc("/events", sse)

	port := os.Getenv("PORT")
	if len(port) == 0 {
		port = "5000"
	}
	http.ListenAndServe(":"+port, nil)
}
