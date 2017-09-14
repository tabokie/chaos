package history

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/anishathalye/porcupine"
)

// Operation action
const (
	InvokeOperation = "call"
	ReturnOperation = "return"
)

type operation struct {
	Action string          `json:"action"`
	Proc   int             `json:"proc"`
	Data   json.RawMessage `json:"data"`
}

// Recorder records operation histogry.
type Recorder struct {
	f *os.File
}

// NewRecorder creates a recorder to log the history to the file.
func NewRecorder(name string) (*Recorder, error) {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	return &Recorder{f: f}, nil
}

// Close closes the recorder.
func (r *Recorder) Close() {
	r.f.Close()
}

// RecordRequest records the request.
func (r *Recorder) RecordRequest(proc int, op interface{}) error {
	return r.record(proc, InvokeOperation, op)
}

// RecordResponse records the response.
func (r *Recorder) RecordResponse(proc int, op interface{}) error {
	return r.record(proc, ReturnOperation, op)
}

func (r *Recorder) record(proc int, action string, op interface{}) error {
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}

	v := operation{
		Action: action,
		Proc:   proc,
		Data:   json.RawMessage(data),
	}

	data, err = json.Marshal(v)
	if err != nil {
		return err
	}

	if _, err = r.f.Write(data); err != nil {
		return err
	}

	if _, err = r.f.WriteString("\n"); err != nil {
		return err
	}

	return nil
}

// RecordParser is to parses the operation data.
type RecordParser interface {
	// OnRequest parses the request record.
	OnRequest(data json.RawMessage) (interface{}, error)
	// OnResponse parses the response record. Return nil means
	// the operation has an infinite end time.
	// E.g, we meet timeout for a operation.
	OnResponse(data json.RawMessage) (interface{}, error)
	// If we have some infinite operations, we should return a
	// noop response to complete the operation.
	OnNoopResponse() interface{}
}

// VerifyHistory checks the history file with model.
// False means the history is not linearizable.
func VerifyHistory(name string, m porcupine.Model, p RecordParser) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()

	procID := map[int]uint{}
	id := uint(0)

	events := make([]porcupine.Event, 0, 1024)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var op operation
		if err = json.Unmarshal(scanner.Bytes(), &op); err != nil {
			return false, err
		}

		var value interface{}
		if op.Action == InvokeOperation {
			if value, err = p.OnRequest(op.Data); err != nil {
				return false, err
			}

			event := porcupine.Event{
				Kind:  porcupine.CallEvent,
				Id:    id,
				Value: value,
			}
			events = append(events, event)
			procID[op.Proc] = id
			id++
		} else {
			if value, err = p.OnResponse(op.Data); err != nil {
				return false, err
			}

			if value == nil {
				continue
			}

			matchID := procID[op.Proc]
			delete(procID, op.Proc)
			event := porcupine.Event{
				Kind:  porcupine.ReturnEvent,
				Id:    matchID,
				Value: value,
			}
			events = append(events, event)
		}
	}

	if err = scanner.Err(); err != nil {
		return false, err
	}

	for _, id := range procID {
		response := p.OnNoopResponse()
		event := porcupine.Event{
			Kind:  porcupine.ReturnEvent,
			Id:    id,
			Value: response,
		}
		events = append(events, event)
	}

	return porcupine.CheckEvents(m, events), nil
}