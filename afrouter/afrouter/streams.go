package afrouter

import (
	"context"
	"errors"
	"github.com/opencord/voltha-go/common/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"sort"
	"sync"
)

type streams struct {
	mutex         sync.Mutex
	activeStream  *stream
	streams       map[string]*stream
	sortedStreams []*stream
}

type stream struct {
	stream    grpc.ClientStream
	ctxt      context.Context
	cancel    context.CancelFunc
	ok2Close  chan struct{}
	c2sReturn chan error
	s2cReturn error
}

func (s *streams) clientCancel() {
	for _, strm := range s.streams {
		if strm != nil {
			strm.cancel()
		}
	}
}

func (s *streams) closeSend() {
	for _, strm := range s.streams {
		if strm != nil {
			<-strm.ok2Close
			log.Debug("Closing southbound stream")
			strm.stream.CloseSend()
		}
	}
}

func (s *streams) trailer() metadata.MD {
	return s.activeStream.stream.Trailer()
}

func (s *streams) getActive() *stream {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.activeStream
}

func (s *streams) setThenGetActive(strm *stream) *stream {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.activeStream == nil {
		s.activeStream = strm
	}
	return s.activeStream
}

func (s *streams) forwardClientToServer(dst grpc.ServerStream, f *sbFrame) chan error {
	fc2s := func(srcS *stream) {
		for i := 0; ; i++ {
			if err := srcS.stream.RecvMsg(f); err != nil {
				if s.setThenGetActive(srcS) == srcS {
					srcS.c2sReturn <- err // this can be io.EOF which is the success case
				} else {
					srcS.c2sReturn <- nil // Inactive responder
				}
				close(srcS.ok2Close)
				break
			}
			if s.setThenGetActive(srcS) != srcS {
				srcS.c2sReturn <- nil
				continue
			}
			if i == 0 {
				// This is a bit of a hack, but client to server headers are only readable after first client msg is
				// received but must be written to server stream before the first msg is flushed.
				// This is the only place to do it nicely.
				md, err := srcS.stream.Header()
				if err != nil {
					srcS.c2sReturn <- err
					break
				}
				// Update the metadata for the response.
				if f.metaKey != NoMeta {
					if f.metaVal == "" {
						// We could also alsways just do this
						md.Set(f.metaKey, f.backend.name)
					} else {
						md.Set(f.metaKey, f.metaVal)
					}
				}
				if err := dst.SendHeader(md); err != nil {
					srcS.c2sReturn <- err
					break
				}
			}
			log.Debugf("Northbound frame %v", f.payload)
			if err := dst.SendMsg(f); err != nil {
				srcS.c2sReturn <- err
				break
			}
		}
	}

	// There should be AT LEAST one open stream at this point
	// if there isn't its a grave error in the code and it will
	// cause this thread to block here so check for it and
	// don't let the lock up happen but report the error
	ret := make(chan error, 1)
	agg := make(chan *stream)
	atLeastOne := false
	for _, strm := range s.streams {
		if strm != nil {
			go fc2s(strm)
			go func(s *stream) { // Wait on result and aggregate
				r := <-s.c2sReturn // got the return code
				if r == nil {
					return // We're the redundat stream, just die
				}
				s.c2sReturn <- r // put it back to pass it along
				agg <- s         // send the stream to the aggregator
			}(strm)
			atLeastOne = true
		}
	}
	if atLeastOne == true {
		go func() { // Wait on aggregated result
			s := <-agg
			ret <- <-s.c2sReturn
		}()
	} else {
		err := errors.New("There are no open streams. Unable to forward message.")
		log.Error(err)
		ret <- err
	}
	return ret
}

func (s *streams) sendAll(f *nbFrame) error {
	var rtrn error

	atLeastOne := false
	for _, strm := range s.sortedStreams {
		if strm != nil {
			if err := strm.stream.SendMsg(f); err != nil {
				log.Debugf("Error on SendMsg: %s", err.Error())
				strm.s2cReturn = err
			}
			atLeastOne = true
		} else {
			log.Debugf("Nil stream")
		}
	}
	// If one of the streams succeeded, declare success
	// if none did pick an error and return it.
	if atLeastOne == true {
		for _, strm := range s.sortedStreams {
			if strm != nil {
				rtrn = strm.s2cReturn
				if rtrn == nil {
					return rtrn
				}
			}
		}
		return rtrn
	} else {
		rtrn = errors.New("There are no open streams, this should never happen")
		log.Error(rtrn)
	}
	return rtrn
}

func (s *streams) forwardServerToClient(src grpc.ServerStream, f *nbFrame) chan error {
	ret := make(chan error, 1)
	go func() {
		// The frame buffer already has the results of a first
		// RecvMsg in it so the first thing to do is to
		// send it to the list of client streams and only
		// then read some more.
		for i := 0; ; i++ {
			// Send the message to each of the backend streams
			if err := s.sendAll(f); err != nil {
				ret <- err
				log.Debugf("SendAll failed %s", err.Error())
				break
			}
			log.Debugf("Southbound frame %v", f.payload)
			if err := src.RecvMsg(f); err != nil {
				ret <- err // this can be io.EOF which is happy case
				break
			}
		}
	}()
	return ret
}

func (s *streams) sortStreams() {
	var tmpKeys []string
	for k := range s.streams {
		tmpKeys = append(tmpKeys, k)
	}
	sort.Strings(tmpKeys)
	for _, v := range tmpKeys {
		s.sortedStreams = append(s.sortedStreams, s.streams[v])
	}
}
