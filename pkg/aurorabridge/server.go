// Copyright (c) 2019 Uber Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package aurorabridge

import (
	"path"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/uber/peloton/pkg/common"
	"github.com/uber/peloton/pkg/common/leader"

	"github.com/docker/libkv/store"
	"github.com/docker/libkv/store/zookeeper"
	"github.com/pkg/errors"
)

const _dummyNode = "_aurorabridge_node"

// Server contains all structs necessary to run a aurorabrdige server.
// This struct also implements leader.Node interface so that it can
// perform leader election among multiple aurorabridge server
// instances.
type Server struct {
	sync.Mutex

	ID       string
	role     string
	zkClient store.Store
	zkRoot   string
}

// NewServer creates a aurorabridge Server instance.
func NewServer(
	httpPort int,
	cfg leader.ElectionConfig,
	role string,
) (*Server, error) {
	endpoint := leader.NewEndpoint(httpPort)
	additionalEndpoints := make(map[string]leader.Endpoint)
	additionalEndpoints["http"] = endpoint

	zkClient, err := zookeeper.New(cfg.ZKServers, nil)
	if err != nil {
		return nil, err
	}

	return &Server{
		ID:       leader.NewServiceInstance(endpoint, additionalEndpoints),
		role:     common.PelotonAuroraBridgeRole,
		zkClient: zkClient,
		zkRoot:   strings.TrimPrefix(path.Join(cfg.Root, role), "/"),
	}, nil
}

// GainedLeadershipCallback is the callback when the current node
// becomes the leader
func (s *Server) GainedLeadershipCallback() error {
	log.WithFields(log.Fields{"role": s.role}).Info("Gained leadership")

	// Re-create a dummy node under /peloton/aurora/scheduler
	// so that the client can pick up the leadership change when it's
	// doing a watch. If it fails to re-create the node, throw the
	// error to give up the leadership.
	key := path.Join(s.zkRoot, _dummyNode)
	err := s.zkClient.Delete(key)
	if err != nil && err != store.ErrKeyNotFound {
		return errors.Wrap(err, "failed to delete dummy node")
	}

	err = s.zkClient.Put(key, []byte{}, nil)
	if err != nil {
		return errors.Wrap(err, "failed to create dummy node")
	}

	return nil
}

// LostLeadershipCallback is the callback when the current node lost
// leadership
func (s *Server) LostLeadershipCallback() error {
	log.WithField("role", s.role).Info("Lost leadership")

	// Remove the dummy node under /peloton/aurora/scheduler
	// to trigger a watch event, ignore the deletion error here
	// since the node is no longer the leader.
	key := path.Join(s.zkRoot, _dummyNode)
	s.zkClient.Delete(key)

	return nil
}

// ShutDownCallback is the callback to shut down gracefully if possible
func (s *Server) ShutDownCallback() error {
	log.WithFields(log.Fields{"role": s.role}).Info("Quitting election")

	return nil
}

// GetID function returns jobmgr app address.
// This implements leader.Nomination.
func (s *Server) GetID() string {
	return s.ID
}
