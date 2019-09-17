/*
 * Copyright 2019-present Open Networking Foundation

 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at

 * http://www.apache.org/licenses/LICENSE-2.0

 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package afrouter

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/opencord/voltha-go/common/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"io"
	"strconv"
	"sync"
)

const (
	txnProcessedMetadataKey = "txn_processed"
)

type request struct {
	mutex                    sync.Mutex
	activeResponseStreamOnce sync.Once
	setResponseHeaderOnce    sync.Once
	responseStreamMutex      sync.Mutex

	streams map[string]grpc.ClientStream

	requestFrameBacklog [][]byte
	responseErrChan     chan error
	sendClosed          bool

	backend             *backend
	ctx                 context.Context
	serverStream        grpc.ServerStream
	methodInfo          methodDetails
	requestFrame        *requestFrame
	responseFrame       *responseFrame
	isStreamingRequest  bool
	isStreamingResponse bool
}

// catchupRequestStreamThenForwardResponseStream must be called with request.mutex pre-locked
func (r *request) catchupRequestStreamThenForwardResponseStream(connName string, stream grpc.ClientStream) {
	r.streams[connName] = stream

	// prime new streams with any traffic they might have missed (non-streaming requests only)
	frame := *r.requestFrame // local copy of frame
	for _, payload := range r.requestFrameBacklog {
		frame.payload = payload
		if err := stream.SendMsg(&frame); err != nil {
			log.Debugf("Error on SendMsg: %s", err.Error())
			break
		}
	}
	if r.sendClosed {
		stream.CloseSend()
	}

	r.mutex.Unlock()

	r.forwardResponseStream(connName, stream)
}

//extractProcessMetaData returns the processing returned value from the metadata
func extractProcessMetaData(md metadata.MD) (bool, error) {
	if md != nil && md.Len() > 0 {
		pVal := md.Get(txnProcessedMetadataKey)
		if len(pVal) > 0 {
			log.Debugf("Processed metadata: %s", pVal)
			if res, err := strconv.ParseBool(pVal[0]); err == nil {
				return res, nil
			}
		}
	}
	return false, status.Error(codes.NotFound, "processed-key-absent")
}

// forwardResponseStream forwards the response stream
func (r *request) forwardResponseStream(connName string, stream grpc.ClientStream) {
	var queuedFrames [][]byte
	var streamHeaderOnce sync.Once
	frame := *r.responseFrame
	var err error
	activeStream := false
	for {
		err = stream.RecvMsg(&frame)
		if err != nil {
			// this can be io.EOF which is the success case
			log.Debugf("Error received: %s", err.Error())
			break
		}

		if r.isStreamingResponse {
			// streaming response - send immediately
			if err = r.sendResponseFrame(stream, frame); err != nil {
				break
			}
		} else { // !r.isStreamingResponse
			if r.isStreamingRequest { // && !r.isStreamingResponse
				// queue the frame (only send response when the last stream closes)
				queuedFrames = append(queuedFrames, frame.payload)
			} else { // !r.isStreamingRequest && !r.isStreamingResponse
				// extract the transaction processing meta key from the grpc stream to
				// ensure the response from the backend that processed the request is actually
				// processed.
				streamHeaderOnce.Do(func() {
					if md, err := stream.Header(); err != nil {
						log.Warnf("Error extracting grpc header: %s", err.Error())
					} else {
						if activeStream, err = extractProcessMetaData(md); err != nil {
							log.Warn("Metadata processing key not found")
						}
					}
				})
				log.Debugf("Received processing metadata: %v", activeStream)
				if activeStream {
					if err = r.sendResponseFrame(stream, frame); err != nil {
						break
					}
				}
			}
		}
	}

	log.Debugf("Closing stream to %s", connName)

	// io.EOF is the success case
	if err == io.EOF {
		err = nil
	}

	// this double-lock sets off alarm bells in my head
	r.backend.mutex.Lock()
	r.mutex.Lock()
	delete(r.streams, connName)
	streamsLeft := len(r.streams)

	// if this the active stream (for non-streaming requests), or this is the last stream (for streaming requests)
	if (activeStream && !r.isStreamingRequest && !r.isStreamingResponse) || (streamsLeft == 0 && (r.isStreamingRequest || r.isStreamingResponse)) {
		// request is complete, cleanup
		delete(r.backend.activeRequests, r)
		r.mutex.Unlock()
		r.backend.mutex.Unlock()

		// send any queued frames we have (streaming request & !streaming response only, but no harm trying in other cases)
		for _, payload := range queuedFrames {
			if err != nil {
				// if there's been an error, don't try to send anymore
				break
			}
			frame.payload = payload
			err = r.sendResponseFrame(stream, frame)
		}

		// We may have received Trailers as part of the call.
		r.serverStream.SetTrailer(stream.Trailer())

		// response stream complete
		r.responseErrChan <- err
	} else {
		r.mutex.Unlock()
		r.backend.mutex.Unlock()
	}
}

func (r *request) sendResponseFrame(stream grpc.ClientStream, f responseFrame) error {
	r.responseStreamMutex.Lock()
	defer r.responseStreamMutex.Unlock()

	// the header should only be set once, even if multiple streams can respond.
	setHeader := false
	r.setResponseHeaderOnce.Do(func() { setHeader = true })
	if setHeader {
		// This is a bit of a hack, but client to server headers are only readable after first client msg is
		// received but must be written to server stream before the first msg is flushed.
		// This is the only place to do it nicely.
		md, err := stream.Header()
		if err != nil {
			return err
		}

		//// Remove any transaction processing key from the MD first
		//delete(md, txnProcessedMetadataKey)

		// Update the metadata for the response.
		if f.metaKey != NoMeta {
			if f.metaVal == "" {
				// We could also always just do this
				md.Set(f.metaKey, f.backend.name)
			} else {
				md.Set(f.metaKey, f.metaVal)
			}
		}

		if err := r.serverStream.SendHeader(md); err != nil {
			log.Errorw("error-sending-error", log.Fields{"error": err})
			return err
		}
	}

	log.Debugf("Response frame %s", hex.EncodeToString(f.payload))

	return r.serverStream.SendMsg(&f)
}

func (r *request) sendAll(frame *requestFrame) error {
	r.mutex.Lock()
	if !r.isStreamingRequest {
		// save frames of non-streaming requests, so we can catchup new streams
		r.requestFrameBacklog = append(r.requestFrameBacklog, frame.payload)
	}

	// send to all existing streams
	streams := make(map[string]grpc.ClientStream, len(r.streams))
	for n, s := range r.streams {
		streams[n] = s
	}
	r.mutex.Unlock()

	var rtrn error
	atLeastOne := false
	atLeastOneSuccess := false
	for _, stream := range streams {
		if err := stream.SendMsg(frame); err != nil {
			log.Debugf("Error on SendMsg: %s", err.Error())
			rtrn = err
		} else {
			atLeastOneSuccess = true
		}
		atLeastOne = true
	}
	// If one of the streams succeeded, declare success
	// if none did pick an error and return it.
	if atLeastOne {
		if atLeastOneSuccess {
			return nil
		} else {
			return rtrn
		}
	} else {
		err := errors.New("unable to send, all streams have closed")
		log.Error(err)
		return err
	}
}

func (r *request) forwardRequestStream(src grpc.ServerStream) error {
	// The frame buffer already has the results of a first
	// RecvMsg in it so the first thing to do is to
	// send it to the list of client streams and only
	// then read some more.
	frame := *r.requestFrame // local copy of frame
	var rtrn error
	for {
		// Send the message to each of the backend streams
		if err := r.sendAll(&frame); err != nil {
			log.Debugf("SendAll failed %s", err.Error())
			rtrn = err
			break
		}
		log.Debugf("Request frame %s", hex.EncodeToString(frame.payload))
		if err := src.RecvMsg(&frame); err != nil {
			rtrn = err // this can be io.EOF which is happy case
			break
		}
	}

	r.mutex.Lock()
	log.Debug("Closing southbound streams")
	r.sendClosed = true
	for _, stream := range r.streams {
		stream.CloseSend()
	}
	r.mutex.Unlock()

	if rtrn != io.EOF {
		log.Debugf("s2cErr reporting %v", rtrn)
		return rtrn
	}
	log.Debug("s2cErr reporting EOF")
	// this is the successful case where the sender has encountered io.EOF, and won't be sending anymore.
	// the clientStream>serverStream may continue sending though.
	return nil
}
