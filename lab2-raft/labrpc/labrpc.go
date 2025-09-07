package labrpc

import (
	"bytes"
	"encoding/gob"
	"log"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// ClientEnd represents one end of an RPC connection.
type ClientEnd struct {
	endname interface{}   // this end-point's name
	ch      chan reqMsg   // copy of Network.endCh
	done    chan struct{} // closed when Network.Cleanup() is called
}

// send an RPC, wait for the reply.
// the return value indicates success; false means that
// no reply was received from the server.
func (e *ClientEnd) Call(svcMeth string, args interface{}, reply interface{}) bool {
	req := reqMsg{}
	req.endname = e.endname
	req.svcMeth = svcMeth
	req.argsType = reflect.TypeOf(args)
	req.args = args
	req.replyCh = make(chan replyMsg)

	qb := new(bytes.Buffer)
	qe := gob.NewEncoder(qb)
	qe.Encode(args)
	req.args = qb.Bytes()

	select {
	case e.ch <- req:
		// ok
	case <-e.done:
		return false
	}

	select {
	case rep := <-req.replyCh:
		if rep.ok {
			rb := bytes.NewBuffer(rep.reply)
			re := gob.NewDecoder(rb)
			if err := re.Decode(reply); err != nil {
				log.Fatalf("ClientEnd.Call(): decode reply: %v\n", err)
			}
			return true
		} else {
			return false
		}
	case <-e.done:
		return false
	}
}

// Server represents an RPC server.
type Server struct {
	mu       sync.Mutex
	services map[string]*Service
	count    int // incoming RPCs
}

// Service represents a service with methods.
type Service struct {
	name    string
	rcvr    reflect.Value
	typ     reflect.Type
	methods map[string]reflect.Method
}

// MakeServer creates a new server.
func MakeServer() *Server {
	rs := &Server{}
	rs.services = map[string]*Service{}
	return rs
}

// AddService adds a service to the server.
func (rs *Server) AddService(svc interface{}) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.count = 0

	// figure out the type of the service
	sname := reflect.Indirect(reflect.ValueOf(svc)).Type().Name()
	if sname == "" {
		log.Fatalf("AddService: no service name for type %v", reflect.TypeOf(svc))
	}
	if !isExported(sname) {
		log.Fatalf("AddService: service name %v is not exported", sname)
	}

	s := &Service{}
	s.name = sname
	s.rcvr = reflect.ValueOf(svc)
	s.typ = reflect.TypeOf(svc)
	s.methods = map[string]reflect.Method{}

	for m := 0; m < s.typ.NumMethod(); m++ {
		method := s.typ.Method(m)
		mtype := method.Type
		mname := method.Name

		if method.PkgPath != "" || // capitalized?
			mtype.NumIn() != 3 ||
			mtype.In(2).Kind() != reflect.Ptr ||
			mtype.NumOut() != 1 ||
			mtype.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			// the method is not suitable for a handler
			//fmt.Printf("bad method: %v\n", mname)
		} else {
			// the method looks like a handler
			s.methods[mname] = method
		}
	}

	if len(s.methods) == 0 {
		log.Fatalf("AddService: %v has no suitable methods", sname)
	}

	rs.services[sname] = s
}

// dispatch a request to the right method of the right service.
func (rs *Server) dispatch(req reqMsg) replyMsg {
	rs.mu.Lock()

	rs.count += 1

	// split Raft.AppendEntries into service and method
	dot := strings.LastIndex(req.svcMeth, ".")
	serviceName := req.svcMeth[:dot]
	methodName := req.svcMeth[dot+1:]

	service, ok := rs.services[serviceName]

	rs.mu.Unlock()

	if ok {
		return rs.call(service, methodName, req)
	} else {
		choices := []
		for k, _ := range rs.services {
			choices = append(choices, k)
		}
		log.Fatalf("labrpc.Server.dispatch(): unknown service %v in %v.%v; expecting one of %v\n",
			serviceName, serviceName, methodName, choices)
		return replyMsg{false, nil}
	}
}

func (rs *Server) call(svc *Service, mname string, req reqMsg) replyMsg {
	method, ok := svc.methods[mname]
	if !ok {
		choices := []
		for k, _ := range svc.methods {
			choices = append(choices, k)
		}
		log.Fatalf("labrpc.Server.call(): unknown method %v in %v; expecting one of %v\n",
			mname, req.svcMeth, choices)
		return replyMsg{false, nil}
	}

	// decode the argument.
	argsType := method.Type.In(1)
	args := reflect.New(argsType.Elem())

	// decode argument
	ab := bytes.NewBuffer(req.args)
	ad := gob.NewDecoder(ab)
	if ad.Decode(args.Interface()) != nil {
		log.Fatalf("labrpc.Server.call(): decode args: %v\n", req.args)
		return replyMsg{false, nil}
	}

	// allocate space for the reply.
	replyType := method.Type.In(2)
	reply := reflect.New(replyType.Elem())

	// call the method.
	function := method.Func
	returnValues := function.Call([]reflect.Value{svc.rcvr, args, reply})

	// encode the reply.
	rb := new(bytes.Buffer)
	re := gob.NewEncoder(rb)
	if re.Encode(reply.Interface()) != nil {
		log.Fatalf("labrpc.Server.call(): encode reply: %v\n", reply)
		return replyMsg{false, nil}
	}

	// did the handler return an error?
	if returnValues[0].Interface() != nil {
		return replyMsg{false, nil}
	}

	return replyMsg{true, rb.Bytes()}
}

// GetCount returns the number of incoming RPCs.
func (rs *Server) GetCount() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.count
}

// Network simulates a network.
type Network struct {
	mu             sync.Mutex
	reliable       bool
	longDelays     bool                        // pause a long time on send on disabled connection
	longReordering bool                        // sometimes delay replies a long time
	endCh          map[interface{}]chan reqMsg // ends, by name
	enabled        map[interface{}]bool        // by end name
	servers        map[interface{}]*Server     // by end name
	connections    map[interface{}]interface{} // end name -> server name
	endnames       []interface{}               // all names of ends
	count          int                         // total RPC count, for statistics
	bytes          int64                       // total bytes sent, for statistics
}

func MakeNetwork() *Network {
	rn := &Network{}
	rn.reliable = true
	rn.endCh = map[interface{}]chan reqMsg{}
	rn.enabled = map[interface{}]bool{}
	rn.servers = map[interface{}]*Server{}
	rn.connections = map[interface{}]interface{}{}
	rn.endnames = []interface{}{}

	// single goroutine to handle all ClientEnd.Call()s
	go func() {
		for {
			select {
			case xreq := <-rn.GetTotalCh():
				atomic := false
				for _, ch := range rn.endCh {
					select {
					case req := <-ch:
						atomic = true
						go rn.ProcessReq(req)
					default:
					}
				}
				if !atomic {
					time.Sleep(1 * time.Millisecond)
				}
			}
		}
	}()

	return rn
}

func (rn *Network) GetTotalCh() chan reqMsg {
	ch := make(chan reqMsg)
	go func() {
		for {
			ch <- reqMsg{}
			time.Sleep(1 * time.Millisecond)
		}
	}()
	return ch
}

func (rn *Network) Cleanup() {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	for _, ch := range rn.endCh {
		close(ch)
	}
}

func (rn *Network) Reliable(yes bool) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.reliable = yes
}

func (rn *Network) LongReordering(yes bool) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.longReordering = yes
}

func (rn *Network) LongDelays(yes bool) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.longDelays = yes
}

func (rn *Network) ReadEndnameInfo(endname interface{}) (enabled bool,
	servername interface{}, server *Server, reliable bool, longreordering bool,
) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	enabled = rn.enabled[endname]
	servername = rn.connections[endname]
	server = rn.servers[servername]
	reliable = rn.reliable
	longreordering = rn.longReordering
	return
}

func (rn *Network) IsServerDead(endname interface{}, servername interface{}, server *Server) bool {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	if rn.enabled[endname] == false || rn.servers[servername] != server {
		return true
	}
	return false
}

func (rn *Network) ProcessReq(req reqMsg) {
	enabled, servername, server, reliable, longreordering := rn.ReadEndnameInfo(req.endname)

	if enabled && servername != nil && server != nil {
		if reliable == false {
			// short delay
			ms := (rand.Int() % 27)
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}

		if reliable == false && (rand.Int()%1000) < 100 {
			// drop the request, return as if timeout
			req.replyCh <- replyMsg{false, nil}
			return
		}

		if reliable == false && (rand.Int()%1000) < 200 {
			// delay the request a while, as if a slow network
			ms := 200 + rand.Int()%1+rand.Int()%2000
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}

		if longreordering == true && rand.Int()%900 < 600 {
			// delay the request a long while
			ms := 200 + rand.Int()%2000
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}

		// execute the request (call the RPC handler).
		// in a separate thread so that we can periodically check
		// if the server has been killed and the RPC should get a
		// failure reply.
		etype := req.argsType
		reply := make(chan replyMsg)
		go func() {
			r := server.dispatch(req)
			reply <- r
		}()

		// wait for handler to return,
		// but stop waiting if DeleteServer() has been called,
		// and return an error.
		replyOK := false
		serverDead := false
		for replyOK == false && serverDead == false {
			select {
			case replyMsg := <-reply:
				replyOK = true
				req.replyCh <- replyMsg
			case <-time.After(100 * time.Millisecond):
				serverDead = rn.IsServerDead(req.endname, servername, server)
				if serverDead {
					req.replyCh <- replyMsg{false, nil}
				}
			}
		}
		serverDead = rn.IsServerDead(req.endname, servername, server)
		if replyOK == false || serverDead == true {
			// server was killed while we were waiting; return error.
			req.replyCh <- replyMsg{false, nil}
		}
	} else {
		// simulate no reply and eventual timeout.
		ms := 0
		if rn.longDelays {
			// let Raft tests check that leader doesn't send
			// RPCs synchronously.
			ms = (rand.Int() % 7000)
		} else {
			// many kv tests require the client to try each
			// server in fairly rapid succession.
			ms = (rand.Int() % 100)
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		req.replyCh <- replyMsg{false, nil}
	}

	rn.mu.Lock()
	rn.count += 1
	rn.bytes += int64(len(req.args))
	rn.mu.Unlock()
}

func (rn *Network) MakeEnd(endname interface{}) *ClientEnd {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	if _, ok := rn.endCh[endname]; ok {
		log.Fatalf("MakeEnd: %v already exists\n", endname)
	}

	ch := make(chan reqMsg)
	rn.endCh[endname] = ch
	rn.enabled[endname] = false
	rn.endnames = append(rn.endnames, endname)

	e := &ClientEnd{}
	e.endname = endname
	e.ch = ch
	e.done = make(chan struct{})
	return e
}

func (rn *Network) AddServer(servername interface{}, rs *Server) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	rn.servers[servername] = rs
}

func (rn *Network) DeleteServer(servername interface{}) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	rn.servers[servername] = nil
}

func (rn *Network) Connect(endname interface{}, servername interface{}) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	rn.connections[endname] = servername
}

func (rn *Network) Enable(endname interface{}, enabled bool) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	rn.enabled[endname] = enabled
}

func (rn *Network) GetCount(servername interface{}) int {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	if rn.servers[servername] != nil {
		return rn.servers[servername].GetCount()
	} else {
		return 0
	}
}

func (rn *Network) GetTotalCount() int {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.count
}

func (rn *Network) GetTotalBytes() int64 {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.bytes
}

// message types
type reqMsg struct {
	endname  interface{}
	svcMeth  string
	argsType reflect.Type
	args     []byte
	replyCh  chan replyMsg
}

type replyMsg struct {
	ok    bool
	reply []byte
}

// helper function
func isExported(name string) bool {
	rune, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(rune)
}