package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	pb "github.com/walkert/ceedee/ceedeeproto"
	"github.com/walkert/watcher"
	"google.golang.org/grpc"
)

var (
	cdpath = regexp.MustCompile(`cd\s+([~\/]\/?.*[^\/]$)`)
)

func (s *ceedeeServer) processBytes(b []byte) {
	if len(b) == 0 {
		return
	}
	s.mux.Lock()
	defer s.mux.Unlock()
	var command string
	pathMap := make(map[string]int)
	for _, line := range strings.Split(string(b), "\n") {
		parts := strings.Split(line, ";")
		if len(parts) == 1 {
			command = parts[0]
		} else {
			command = parts[len(parts)-1]
		}
		match := cdpath.FindStringSubmatch(command)
		if len(match) != 2 {
			continue
		}
		path := match[1]
		if strings.HasPrefix(path, "~") {
			path = strings.Replace(path, "~", "/Users/walkert", 1)
		}
		if _, ok := pathMap[path]; ok {
			pathMap[path] += 1
		} else {
			pathMap[path] = 1
		}
	}
	// Now that we have our paths with the counts, see if they're already in
	// the directory map and if they are, ensure that they're first in the list of options
	// if appropriate
	for path, _ := range pathMap {
		base := filepath.Base(path)
		vals, ok := s.dirData[base]
		if !ok {
			continue
		}
		if len(vals) == 1 && vals[0] == path {
			log.Debugf("Path '%s' for base '%s' already present and correct\n", path, base)
			continue
		}
		if vals[0] != path {
			log.Debugf("First entry for '%s' is '%s' instead of '%s', rewriting\n", base, vals[0], path)
			newList := []string{path}
			for _, v := range vals {
				if v != path {
					newList = append(newList, v)
				}
			}
			log.Debugf("Replacing vals list for '%s'\n", base)
			s.dirData[base] = newList
		}
	}
}

func (s *ceedeeServer) watchHistory() error {
	w, err := watcher.New("/Users/walkert/.zhistfile", watcher.WithChannelMonitor(10))
	if err != nil {
		return err
	}
	log.Debugln("Launching history watcher")
	go func() {
		for {
			select {
			case bytes := <-w.ByteChannel:
				log.Debugf("Processing %d received bytes from history\n", len(bytes))
				s.processBytes(bytes)
			case err := <-w.ErrChannel:
				log.Debugf("Received error from watcher: %v\n", err)
				return
			}
		}
	}()
	return nil
}
func (s *ceedeeServer) walker(path string, info os.FileInfo, err error) error {
	if err != nil {
		log.Debug(err)
		return nil
	}
	if !info.IsDir() {
		return nil
	}
	base := filepath.Base(path)
	_, baseMatch := s.skipList[base]
	_, fullMatch := s.skipList[path]
	if baseMatch || fullMatch {
		log.Debugln("Skipping", path)
		return filepath.SkipDir
	}
	_, ok := s.dirData[base]
	if !ok {
		s.dirData[base] = []string{path}
	} else {
		s.dirData[base] = append(s.dirData[base], path)
	}
	return nil
}

func (s *ceedeeServer) buildDirStructure(path string) {
	s.mux.Lock()
	defer s.mux.Unlock()
	start := time.Now()
	filepath.Walk(path, s.walker)
	delta := time.Now().Sub(start)
	log.Debugf("Indexing of %s took %s\n", path, delta)
}

type ceedeeServer struct {
	dirData  map[string][]string
	mux      sync.Mutex
	skipList map[string]int
}

func (s *ceedeeServer) Get(ctx context.Context, Directory *pb.Directory) (*pb.Dlist, error) {
	dirs, ok := s.dirData[Directory.Name]
	if !ok {
		return &pb.Dlist{}, fmt.Errorf("No entry for directory %s", Directory.Name)
	}
	var dlist string
	if len(dirs) == 1 {
		dlist = dirs[0]
	} else {
		dlist = strings.Join(dirs, ":")
	}
	return &pb.Dlist{Dirs: dlist}, nil
}

type Server struct {
	path     string
	port     int
	skipList map[string]int
	l        net.Listener
	s        *grpc.Server
}

type ServerOpt func(s *Server)

func New(port int, path string, opts ...ServerOpt) (*Server, error) {
	svr := &Server{path: path, port: port}
	for _, opt := range opts {
		opt(svr)
	}
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return &Server{}, fmt.Errorf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	dirData := make(map[string][]string)
	cServer := &ceedeeServer{dirData: dirData, mux: sync.Mutex{}}
	if svr.skipList != nil {
		cServer.skipList = svr.skipList
	}
	cServer.buildDirStructure(path)
	err = cServer.watchHistory()
	if err != nil {
		log.Fatalln(err)
	}
	pb.RegisterCeeDeeServer(s, cServer)
	svr.s = s
	svr.l = lis
	return svr, nil
}

func WithSkipList(dirs []string) ServerOpt {
	var skips = make(map[string]int)
	for _, dir := range dirs {
		skips[dir] = 1
	}
	return func(s *Server) {
		s.skipList = skips
	}
}

func (s *Server) Start() error {
	log.Debugf("grpc ceedeeServer listening on: %d\n", s.port)
	if err := s.s.Serve(s.l); err != nil {
		return fmt.Errorf("unable to ceedeeServer: %v", err)
	}
	return nil
}

func (s *Server) Stop() {
	s.s.Stop()
}
