package main

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"github.com/walkert/ceedee/client"
	"github.com/walkert/ceedee/server"
)

func main() {
	asServer := flag.Bool("server", false, "run in server mode")
	list := flag.BoolP("list", "l", false, "list all matching directories")
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
	if !*asServer {
		if len(flag.Args()) == 0 {
			log.Fatal("No directory supplied")
		}
		c, err := client.New(2020)
		if err != nil {
			log.Fatal(err)
		}
		values, err := c.Get(flag.Args()[0])
		if err != nil {
			log.Fatal(err)
		}
		if len(values) == 0 {
			os.Exit(1)
		}
		if *list {
			for _, entry := range values {
				fmt.Println(strings.Split(entry, ";")[1])
			}
			os.Exit(0)
		}
		if strings.HasPrefix(values[0], "e") {
			fmt.Println(strings.Split(values[0], ";")[1])
			os.Exit(0)
		}
		if strings.HasPrefix(values[0], "p") {
			for _, partial := range values {
				fmt.Println(strings.Split(partial, ";")[1])
			}
			os.Exit(0)
		}
	}
	if *asServer {
		if *root == "" {
			log.Fatalln("You must enter a root path")
		}
		s, _ := server.New(2020, *root, server.WithSkipList([]string{".git", "/Users/walkert/Library"}))
		s.Start()
	}
}
