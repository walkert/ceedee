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
	cdpath                 = regexp.MustCompile(`cd\s+([~\/]\/?.*[^\/]$)`)
	defaultMonitorInterval = 10
	defaultDirWalkInterval = 1
)

type directory struct {
	path           string
	histCandidates []candidate
	pathCandidates []candidate
	tracker        map[string]struct{}
}

func (d *directory) addPathCandidate(path string) {
	if _, ok := d.tracker[path]; ok {
		return
	}
	log.Debugf("Adding a new candidate path %s to base %s\n", path, d.path)
	d.tracker[path] = struct{}{}
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
	var exists bool
	for idx, c := range d.histCandidates {
		if c.path == path {
			d.histCandidates[idx].count += 1
			exists = true
		}
	}
	if !exists {
		c := candidate{path: path, count: count}
		d.histCandidates = append(d.histCandidates, c)
	}
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
			path = strings.Replace(path, "~", s.home, 1)
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
			// TODO: Run addHistCandidate here to create a new entry even if it hasn't been
			//		 seen by the filepath walker.
			continue
		}
		log.Debugf("Adding/updating a hist path link %s->%s\n", base, path)
		s.dirData[base].addHistCandidate(path, count)
	}
}

func (s *ceedeeServer) watchHistory() error {
	w, err := watcher.New(s.histFile, watcher.WithChannelMonitor(s.monitorInterval))
	if err != nil {
		return err
	}
	log.Debugln("Launching history watcher for file", s.histFile)
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
		log.Debugln("Creating new directory reference for", base)
		d := &directory{path: base, tracker: make(map[string]struct{})}
		d.addPathCandidate(path)
		s.dirData[base] = d
	} else {
		s.dirData[base].addPathCandidate(path)
	}
	return nil
}

func (s *ceedeeServer) backGroundDir() {
	go func() {
		for range time.Tick(time.Duration(s.dirInterval) * time.Hour) {
			log.Debugln("Kicking off directory walk..")
			s.buildDirStructure()
		}
	}()
}

func (s *ceedeeServer) buildDirStructure() {
	s.mux.Lock()
	defer s.mux.Unlock()
	start := time.Now()
	filepath.Walk(s.root, s.walker)
	delta := time.Now().Sub(start)
	log.Debugf("Indexing of %s took %s\n", s.root, delta)
}

type ceedeeServer struct {
	dirData         map[string]*directory
	dirInterval     int
	histFile        string
	home            string
	monitorInterval int
	mux             sync.Mutex
	root            string
	skipList        map[string]int
}

func (s *ceedeeServer) getPartial(name string) []string {
	start := time.Now()
	var matches []string
	for path, _ := range s.dirData {
		if strings.Index(path, name) > -1 {
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
	histFile        string
	home            string
	root            string
	monitorInterval int
	dirInterval     int
	port            int
	skipList        map[string]int
	l               net.Listener
	s               *grpc.Server
}

type ServerOpt func(s *Server)

func New(opts ...ServerOpt) (*Server, error) {
	svr := &Server{}
	for _, opt := range opts {
		opt(svr)
	}
	if svr.monitorInterval == 0 {
		svr.monitorInterval = defaultMonitorInterval
	}
	if svr.dirInterval == 0 {
		svr.dirInterval = defaultDirWalkInterval
	}
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", svr.port))
	if err != nil {
		return &Server{}, fmt.Errorf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	dirData := make(map[string]*directory)
	cServer := &ceedeeServer{
		dirData:         dirData,
		dirInterval:     svr.dirInterval,
		histFile:        svr.histFile,
		home:            svr.home,
		monitorInterval: svr.monitorInterval,
		mux:             sync.Mutex{},
		root:            svr.root,
	}
	if svr.skipList != nil {
		cServer.skipList = svr.skipList
	}
	cServer.buildDirStructure()
	cServer.backGroundDir()
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

func WithHistFile(name string) ServerOpt {
	return func(s *Server) {
		s.histFile = name
	}
}

func WithPort(port int) ServerOpt {
	return func(s *Server) {
		s.port = port
	}
}

func WithRoot(root string) ServerOpt {
	return func(s *Server) {
		s.root = root
	}
}

func WithHome(home string) ServerOpt {
	return func(s *Server) {
		s.home = home
	}
}

func WithMonitorInterval(interval int) ServerOpt {
	return func(s *Server) {
		s.monitorInterval = interval
	}
}

func WithDirInterval(interval int) ServerOpt {
	return func(s *Server) {
		s.dirInterval = interval
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
