package starx

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/chrislonng/starx/cluster"
	"github.com/chrislonng/starx/cluster/rpc"
	routelib "github.com/chrislonng/starx/route"
	"github.com/chrislonng/starx/session"
	"github.com/chrislonng/starx/log"
)

var (
	ErrRPCLocal     = errors.New("RPC object must location in different server type")
	ErrSidNotExists = errors.New("sid not exists")
)

// Agent corresponding a user, used for store raw socket information
// only used in package internal, can not accessible by other package
type agent struct {
	id       int64
	socket   net.Conn
	status   networkStatus
	session  *session.Session
	lastTime int64 // last heartbeat unix time stamp
}

// Create new agent instance
func newAgent(conn net.Conn) *agent {
	a := &agent{
		socket:   conn,
		status:   statusStart,
		lastTime: time.Now().Unix()}
	s := session.NewSession(a)
	a.session = s
	a.id = s.ID
	return a
}

// String, implementation for Stringer interface
func (a *agent) String() string {
	return fmt.Sprintf("id: %d, remote address: %s, last time: %d",
		a.id,
		a.socket.RemoteAddr().String(),
		a.lastTime)
}

func (a *agent) heartbeat() {
	a.lastTime = time.Now().Unix()
}

func (a *agent) close() {
	a.status = statusClosed
	defaultNetService.closeSession(a.session)
	a.socket.Close()
}

func (a *agent) ID() int64 {
	return a.id
}

func (a *agent) Send(data []byte) error {
	_, err := a.socket.Write(data)
	return err
}

func (a *agent) Push(session *session.Session, route string, v interface{}) error {
	data, err := serializeOrRaw(v)
	if err != nil {
		return err
	}

	log.Debugf("Type=Push, UID=%d, Route=%s, Data=%+v", session.Uid, route, v)

	return defaultNetService.push(session, route, data)
}

// Response message to session
func (a *agent) Response(session *session.Session, v interface{}) error {
	data, err := serializeOrRaw(v)
	if err != nil {
		return err
	}

	log.Debugf("Type=Response, UID=%d, Data=%+v", session.Uid, v)

	return defaultNetService.response(session, data)
}

func (a *agent) Call(session *session.Session, route string, reply interface{}, args ...interface{}) error {
	r, err := routelib.Decode(route)
	if err != nil {
		return err
	}

	if App.Config.Type == r.ServerType {
		return ErrRPCLocal
	}

	data, err := gobEncode(args...)
	if err != nil {
		return err
	}

	ret, err := cluster.Call(rpc.User, r, session, data)
	if err != nil {
		return err
	}

	return gobDecode(reply, ret)
}
