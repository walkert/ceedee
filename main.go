package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"github.com/walkert/ceedee/client"
	"github.com/walkert/ceedee/server"
)

const (
	zhistDefault = ".zhistfile"
)

func main() {
	home, err := homedir.Dir()
	if err != nil {
		log.Fatalln("Unable to determine home directory")
	}
	asServer := flag.Bool("server", false, "run in server mode")
	daemonMode := flag.BoolP("daemon", "d", false, "deamonize when running in server mode")
	histFile := flag.String("hist-file", filepath.Join(home, zhistDefault), "the history file to search")
	list := flag.BoolP("list", "l", false, "list all matching directories")
	port := flag.Int("port", 2020, "connect/listen to this port")
	skipDirs := flag.String("skip-dirs", ".git,.hg", "a comma-separated list of directories to skip while indexing")
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
		c, err := client.New(*port)
		if err != nil {
			log.Fatal(err)
		}
		values, err := c.Get(flag.Args()[0])
		if err != nil {
			if strings.Contains(err.Error(), "refused") {
				log.Fatalln("There is no server listening on port", *port)
			}
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
		if *daemonMode {
			prog := path.Base(os.Args[0])
			binary, _ := exec.LookPath(os.Args[0])
			args := []string{binary}
			for _, arg := range os.Args[1:] {
				if arg == "--daemon" {
					continue
				}
				args = append(args, arg)
			}
			cmdEnv := os.Environ()
			pid, err := syscall.ForkExec(binary, args, &syscall.ProcAttr{Env: cmdEnv})
			if err != nil {
				log.Fatalf("ERROR: unable to start %s in daemon mode: %v\n", prog, err)
			}
			fmt.Printf("Started %s in daemon mode with pid %d\n", prog, pid)
			os.Exit(0)
		}
		s, err := server.New(
			server.WithPort(*port),
			server.WithRoot(*root),
			server.WithSkipList(strings.Split(*skipDirs, ",")),
			server.WithHistFile(*histFile),
			server.WithHome(home),
		)
		if err != nil {
			log.Fatalln("Unable to create a new server instance:", err)
		}
		s.Start()
	}
}
