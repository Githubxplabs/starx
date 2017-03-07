package starx

import (
	"fmt"
	"net"
	"time"

	"github.com/chrislonng/starx/cluster"
	"github.com/chrislonng/starx/cluster/rpc"
	"github.com/chrislonng/starx/log"
	routelib "github.com/chrislonng/starx/route"
	"github.com/chrislonng/starx/session"
)

// Acceptor corresponding a front server, used for store raw socket
// information.
// only used in package internal, can not accessible by other package
type acceptor struct {
	id         int64
	socket     net.Conn
	status     networkStatus
	sessionMap map[int64]*session.Session // backend sessions
	f2bMap     map[int64]int64            // frontend session id -> backend session id map
	b2fMap     map[int64]int64            // backend session id -> frontend session id map
	lastTime   int64                      // last heartbeat unix time stamp
}

// Create new backend session instance
func newAcceptor(id int64, conn net.Conn) *acceptor {
	return &acceptor{
		id:         id,
		socket:     conn,
		status:     statusStart,
		sessionMap: make(map[int64]*session.Session),
		f2bMap:     make(map[int64]int64),
		b2fMap:     make(map[int64]int64),
		lastTime:   time.Now().Unix(),
	}
}

// String implement Stringer interface
func (a *acceptor) String() string {
	return fmt.Sprintf("id: %d, remote address: %s, last time: %d",
		a.id,
		a.socket.RemoteAddr().String(),
		a.lastTime)
}

func (a *acceptor) heartbeat() {
	a.lastTime = time.Now().Unix()
}

func (a *acceptor) Session(sid int64) *session.Session {
	if bsid, ok := a.f2bMap[sid]; ok && bsid > 0 {
		return a.sessionMap[bsid]
	}
	s := session.NewSession(a)
	a.sessionMap[s.ID] = s
	a.f2bMap[sid] = s.ID
	a.b2fMap[s.ID] = sid
	return s
}

func (a *acceptor) close() {
	a.status = statusClosed
	for _, s := range a.sessionMap {
		defaultNetService.closeSession(s)
	}
	defaultNetService.removeAcceptor(a)
	a.socket.Close()
}

func (a *acceptor) ID() int64 {
	return a.id
}

func (a *acceptor) Send(data []byte) error {
	_, err := a.socket.Write(data)
	return err
}

func (a *acceptor) Push(session *session.Session, route string, v interface{}) error {
	data, err := serializeOrRaw(v)
	if err != nil {
		return err
	}

	log.Debugf("UID=%d, Type=Push, Route=%s, Data=%+v", session.Uid, route, v)

	rs, err := defaultNetService.acceptor(session.Entity.ID())
	if err != nil {
		log.Errorf(err.Error())
		return err
	}

	sid, ok := rs.b2fMap[session.ID]
	if !ok {
		log.Errorf("sid not exists")
		return ErrSidNotExists
	}

	resp := &rpc.Response{
		Route: route,
		Kind:  rpc.HandlerPush,
		Data:  data,
		Sid:   sid,
	}
	return rpc.WriteResponse(a.socket, resp)
}

// Response message to session
func (a *acceptor) Response(session *session.Session, v interface{}) error {
	data, err := serializeOrRaw(v)
	if err != nil {
		return err
	}

	log.Debugf("UID=%d, Type=Response, Data=%+v", session.Uid, v)

	rs, err := defaultNetService.acceptor(session.Entity.ID())
	if err != nil {
		log.Errorf(err.Error())
		return err
	}

	sid, ok := rs.b2fMap[session.ID]
	if !ok {
		log.Errorf("sid not exists")
		return ErrSidNotExists
	}
	resp := &rpc.Response{
		Kind: rpc.HandlerResponse,
		Data: data,
		Sid:  sid,
	}
	return rpc.WriteResponse(a.socket, resp)
}

func (a *acceptor) Call(session *session.Session, route string, reply interface{}, args ...interface{}) error {
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
