package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

type directory struct {
	path           string
	histCandidates []candidate
	pathCandidates []candidate
}

func (d *directory) addPathCandidate(path string) {
	c := candidate{path: path, depth: len(strings.Split(path, "/"))}
	d.pathCandidates = append(d.pathCandidates, c)
	sort.Slice(d.pathCandidates, func(i, j int) bool {
		if d.pathCandidates[i].depth < d.pathCandidates[j].depth {
			return true
		}
		return false
	})
}

func (d *directory) addHistCandidate(path string, count int) {
	c := candidate{path: path, count: count}
	d.histCandidates = append(d.histCandidates, c)
	sort.Slice(d.histCandidates, func(i, j int) bool {
		if d.histCandidates[i].count > d.histCandidates[j].count {
			return true
		}
		return false
	})
}

func (d *directory) candidateString() string {
	var list []string
	for _, h := range d.histCandidates {
		list = append(list, fmt.Sprintf("e;%s", h.path))
	}
	for _, p := range d.pathCandidates {
		list = append(list, fmt.Sprintf("e;%s", p.path))
	}
	return strings.Join(list, ":")
}

type candidate struct {
	count int
	depth int
	path  string
}

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
	for path, count := range pathMap {
		base := filepath.Base(path)
		_, ok := s.dirData[base]
		if !ok {
			continue
		}
		s.dirData[base].addHistCandidate(path, count)
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
		d := &directory{}
		d.addPathCandidate(path)
		s.dirData[base] = d
	} else {
		s.dirData[base].addPathCandidate(path)
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
	dirData  map[string]*directory
	mux      sync.Mutex
	skipList map[string]int
}

func (s *ceedeeServer) getPartial(name string) []string {
	start := time.Now()
	r := regexp.MustCompile(fmt.Sprintf(`(%s\w+)`, name))
	var matches []string
	for path, _ := range s.dirData {
		if len(r.FindStringSubmatch(path)) == 2 {
			log.Debugln("Found a match for name:", name)
			matches = append(matches, fmt.Sprintf("p;%s", path))
		}
	}
	sort.Strings(matches)
	log.Debugln("Time taken to find partial:", time.Now().Sub(start))
	return matches
}

func (s *ceedeeServer) Get(ctx context.Context, Directory *pb.Directory) (*pb.Dlist, error) {
	dir, ok := s.dirData[Directory.Name]
	if !ok {
		log.Debugf("No direct match for %s, starting partial check..\n", Directory.Name)
		results := s.getPartial(Directory.Name)
		if len(results) == 0 {
			return &pb.Dlist{}, fmt.Errorf("No entry for directory %s", Directory.Name)
		}
		return &pb.Dlist{Dirs: strings.Join(results, ":")}, nil
	}
	return &pb.Dlist{Dirs: dir.candidateString()}, nil
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
	dirData := make(map[string]*directory)
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
