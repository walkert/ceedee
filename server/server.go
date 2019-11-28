package server

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/karrick/godirwalk"
	log "github.com/sirupsen/logrus"
	pb "github.com/walkert/ceedee/ceedeeproto"
	"github.com/walkert/watcher"
	"google.golang.org/grpc"
)

var (
	cdpath                 = regexp.MustCompile(`cd\s+([~\/]\/?.*[^\/]$)`)
	defaultMonitorInterval = 10
	defaultDirWalkInterval = 60 * 60
)

type directory struct {
	path           string
	histCandidates []candidate
	pathCandidates []candidate
	tracker        map[string]struct{}
}

// addPathCandidate creates a new pathCandidates entry and sorts the list
// according to the directory depth
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

// addHistCandidate createa a new histCandidates entry and sorts the list
// by how many times the entry has appeared in the history
func (d *directory) addHistCandidate(path string, count int) {
	var exists bool
	for idx, c := range d.histCandidates {
		if c.path == path {
			d.histCandidates[idx].count++
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

// rmPath removes a dangling directory reference
func (d *directory) rmPath(path string) {
	if _, ok := d.tracker[path]; ok {
		delete(d.tracker, path)
	}
	for i := 0; i < len(d.histCandidates); i++ {
		if d.histCandidates[i].path == path {
			d.histCandidates = append(d.histCandidates[:i], d.histCandidates[i+1:]...)
		}
	}
	for i := 0; i < len(d.pathCandidates); i++ {
		if d.pathCandidates[i].path == path {
			d.pathCandidates = append(d.pathCandidates[:i], d.pathCandidates[i+1:]...)
		}
	}
	log.Debugf("Removed directory references for %s in base %s", path, d.path)
}

// isEmpty returns true if there are no more references to the directory
func (d *directory) isEmpty() bool {
	if len(d.histCandidates) == 0 && len(d.pathCandidates) == 0 {
		return true
	}
	return false
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

// processBytes iterates over new data from the history file to determine
// if any new history candidates should be created.
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
			pathMap[path]++
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

// watchHistory passes newly discovered history entres to processBytes
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

// walker is the func passed to filepath.Walk for creating new pathCandidates
func (s *ceedeeServer) walker(path string, de *godirwalk.Dirent) error {
	if !de.IsDir() {
		return nil
	}
	base := filepath.Base(path)
	_, baseMatch := s.skipList[base]
	_, fullMatch := s.skipList[path]
	if baseMatch || fullMatch {
		log.Debugln("Skipping", path)
		return filepath.SkipDir
	}
	s.walkTracker[path] = true
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
		for range time.Tick(time.Duration(s.dirInterval) * time.Second) {
			log.Debugln("Kicking off directory walk..")
			s.buildDirStructure()
		}
	}()
}

// buildDirStructure finds directories in s.root and adds them to the
// pathCandidates list
func (s *ceedeeServer) buildDirStructure() error {
	s.mux.Lock()
	defer s.mux.Unlock()
	start := time.Now()
	err := godirwalk.Walk(s.root, &godirwalk.Options{
		Callback: s.walker,
		ErrorCallback: func(osPathname string, err error) godirwalk.ErrorAction {
			return godirwalk.SkipNode
		},
		Unsorted: true,
	})
	if err != nil {
		return err
	}
	delta := time.Now().Sub(start)
	log.Debugf("Indexing of %s took %s\n", s.root, delta)
	// now that we've completed the run, find any directories that are missing
	// and remove references to them
	for k, v := range s.walkTracker {
		if !v {
			log.Debugln("Directory has been removed", k)
			base := filepath.Base(k)
			s.dirData[base].rmPath(k)
			if s.dirData[base].isEmpty() {
				log.Debugf("No more references for %s, removing..", base)
				delete(s.dirData, base)
			}
			delete(s.walkTracker, k)
		} else {
			s.walkTracker[k] = false
		}
	}
	return nil
}

// ceedeeServer represents a server object that implements the ceedeeproto
// server interface
type ceedeeServer struct {
	dirData         map[string]*directory
	dirInterval     int
	histFile        string
	home            string
	monitorInterval int
	mux             sync.Mutex
	root            string
	skipList        map[string]int
	walkTracker     map[string]bool
}

func (s *ceedeeServer) getPartial(name string) []string {
	start := time.Now()
	var matches []string
	for path := range s.dirData {
		if strings.Index(path, name) > -1 {
			log.Debugln("Found a match for name:", name)
			matches = append(matches, fmt.Sprintf("p;%s", path))
		}
	}
	sort.Strings(matches)
	log.Debugln("Time taken to find partial:", time.Now().Sub(start))
	return matches
}

// Get a path match (or not) from dirData. Partial matches result in a colon-separarted list
// being sent back while an explicit match returns a colon-separarted list of full paths
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

// Server is an exported struct which represents the grpc server process and takes various
// options
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

// Opt defines a functional option that operates on a Server receiver
type Opt func(s *Server)

// New returns a configured Server object which runs the grpc server
func New(opts ...Opt) (*Server, error) {
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
		return nil, fmt.Errorf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	cServer := &ceedeeServer{
		dirData:         make(map[string]*directory),
		dirInterval:     svr.dirInterval,
		histFile:        svr.histFile,
		home:            svr.home,
		monitorInterval: svr.monitorInterval,
		mux:             sync.Mutex{},
		root:            svr.root,
		walkTracker:     make(map[string]bool),
	}
	if svr.skipList != nil {
		cServer.skipList = svr.skipList
	}
	err = cServer.buildDirStructure()
	if err != nil {
		return nil, err
	}
	cServer.backGroundDir()
	err = cServer.watchHistory()
	if err != nil {
		return nil, err
	}
	pb.RegisterCeeDeeServer(s, cServer)
	svr.s = s
	svr.l = lis
	return svr, nil
}

// WithSkipList accepts directories which should be skipped during the
// directory walk
func WithSkipList(dirs []string) Opt {
	var skips = make(map[string]int)
	for _, dir := range dirs {
		skips[dir] = 1
	}
	return func(s *Server) {
		s.skipList = skips
	}
}

// WithHistFile sets the history file to watch
func WithHistFile(name string) Opt {
	return func(s *Server) {
		s.histFile = name
	}
}

// WithPort sets the port the grpc server will listen on
func WithPort(port int) Opt {
	return func(s *Server) {
		s.port = port
	}
}

// WithRoot sets the root directory that the directory walk
// will operate on
func WithRoot(root string) Opt {
	return func(s *Server) {
		s.root = root
	}
}

// WithHome sets the home directory to use for '~' substitutions
func WithHome(home string) Opt {
	return func(s *Server) {
		s.home = home
	}
}

// WithMonitorInterval sets the watcher interval for the history file
func WithMonitorInterval(interval int) Opt {
	return func(s *Server) {
		s.monitorInterval = interval
	}
}

// WithDirInterval sets the interval for the directory walk
func WithDirInterval(interval int) Opt {
	return func(s *Server) {
		s.dirInterval = interval
	}
}

// Start the grpc server process
func (s *Server) Start() error {
	log.Debugf("grpc ceedeeServer listening on: %d\n", s.port)
	if err := s.s.Serve(s.l); err != nil {
		return fmt.Errorf("unable to serve ceedeeServer: %v", err)
	}
	return nil
}

// Stop the grpc server process gracefully
func (s *Server) Stop() {
	s.s.Stop()
}
