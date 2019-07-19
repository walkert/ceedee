package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	pb "github.com/walkert/ceedee/ceedeeproto"
	"google.golang.org/grpc"
)

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
	start := time.Now()
	filepath.Walk(path, s.walker)
	delta := time.Now().Sub(start)
	log.Debugf("Indexing of %s took %s\n", path, delta)
}

type ceedeeServer struct {
	dirData  map[string][]string
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
	cServer := &ceedeeServer{dirData: dirData}
	if svr.skipList != nil {
		cServer.skipList = svr.skipList
	}
	cServer.buildDirStructure(path)
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
