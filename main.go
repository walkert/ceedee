package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"github.com/walkert/ceedee/client"
	"github.com/walkert/ceedee/server"
)

func main() {
	asClient := flag.String("client", "", "run in client mode")
	asServer := flag.Bool("server", false, "run in server mode")
	root := flag.String("root", "", "the path to index")
	verbose := flag.Bool("verbose", false, "enable verbose logging")
	flag.Parse()
	if *verbose {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:          true,
		TimestampFormat:        "2006-01-02 15:04:05",
		DisableLevelTruncation: true,
	})
	if *asClient != "" {
		c, err := client.New(2020)
		if err != nil {
			log.Fatal(err)
		}
		value, err := c.Get(*asClient)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(value)
	}
	if *asServer {
		if *root == "" {
			log.Fatalln("You must enter a root path")
		}
		s, _ := server.New(2020, *root)
		s.Start()
	}
}
