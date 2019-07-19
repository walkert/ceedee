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

var (
	skipList = map[string]int{".git": 1, "/Users/walkert/Library": 1}
)

func (s *server) walker(path string, info os.FileInfo, err error) error {
	if err != nil {
		log.Debug(err)
		return nil
	}
	if !info.IsDir() {
		return nil
	}
	base := filepath.Base(path)
	_, baseMatch := skipList[base]
	_, fullMatch := skipList[path]
	if baseMatch || fullMatch {
		log.Debug("Skipping", path)
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

func (s *server) buildDirStructure(path string) {
	start := time.Now()
	filepath.Walk(path, s.walker)
	delta := time.Now().Sub(start)
	log.Debugf("Indexing of %s took %s\n", path, delta)
}

type server struct {
	dirData map[string][]string
}

func (s *server) Get(ctx context.Context, Directory *pb.Directory) (*pb.Dlist, error) {
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
	path string
	port int
	l    net.Listener
	s    *grpc.Server
}

func New(port int, path string) (*Server, error) {
	svr := &Server{path: path, port: port}
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return &Server{}, fmt.Errorf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	dirData := make(map[string][]string)
	ceedeeServer := &server{dirData: dirData}
	ceedeeServer.buildDirStructure(path)
	pb.RegisterCeeDeeServer(s, &server{dirData: dirData})
	svr.s = s
	svr.l = lis
	return svr, nil
}

func (s *Server) Start() error {
	log.Debugf("grpc server listening on: %d\n", s.port)
	if err := s.s.Serve(s.l); err != nil {
		return fmt.Errorf("unable to server: %v", err)
	}
	return nil
}

func (s *Server) Stop() {
	s.s.Stop()
}
