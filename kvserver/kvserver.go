package kvserver

import (
	"errors"
	"fmt"
	"log"
	"raft/kvserver/kvstore"
	"strings"
)

var InvalidCommandErr = errors.New("Invalid command")

type action string

const (
	GET    action = "GET"
	SET    action = "SET"
	DELETE action = "DEL"
)

type Command struct {
	Action action
	Key    string
	Value  string
}

type KVServer struct {
	store kvstore.KVStore
}

func NewKvServer() *KVServer {
	store := kvstore.NewKvStore()
	return &KVServer{
		store: store,
	}
}

func (k *KVServer) Apply(item string) string {
	cmd, err := ParseCmd(item)
	if err != nil {
		log.Println("KVServer: Error parsing line", item, err)
		return "unparseable"
	}

	switch cmd.Action {
	case GET:
		value, found := k.store.Get(cmd.Key)
		return ResponseToGet(value, found)

	case SET:
		found := k.store.Set(cmd.Key, cmd.Value)
		return ResponseToSet(found)

	case DELETE:
		found := k.store.Delete(cmd.Key)
		return ResponseToDelete(found)

	default:
		panic(fmt.Sprintf("Unexpected action: %v", cmd.Action))
	}
}

func ResponseToGet(value string, found bool) string {
	if found {
		return "ok " + value
	} else {
		return "notfound"
	}
}

func ResponseToSet(found bool) string {
	// for now discard info on whether key was previously
	// present
	return "ok"
}

func ResponseToDelete(found bool) string {
	if found {
		return "ok"
	} else {
		return "notfound"
	}
}

func ParseCmd(commandStr string) (*Command, error) {
	action := commandStr[:4]

	switch strings.ToUpper(action) {
	case "GET ":
		key := strings.TrimSpace(commandStr[4:])
		if !validateKey(key) {
			return nil, InvalidCommandErr
		}
		return &Command{
			Action: GET,
			Key:    key,
		}, nil

	case "SET ":
		// in general value can contain whitespace, but we trim
		// off leading and trailing whitespace in the first protocol
		// version so we can reuse it as a cli. hacky but effective
		key, value, found := strings.Cut(strings.TrimSpace(commandStr[4:]), " ")
		if !found {
			return nil, InvalidCommandErr
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !validateKey(key) {
			return nil, InvalidCommandErr
		}
		return &Command{
			Action: SET,
			Key:    key,
			Value:  value,
		}, nil

	case "DEL ":
		key := strings.TrimSpace(commandStr[4:])
		if !validateKey(key) {
			return nil, InvalidCommandErr
		}
		return &Command{
			Action: DELETE,
			Key:    key,
		}, nil

	default:
		return nil, InvalidCommandErr
	}
}

func validateKey(key string) bool {
	// keys may not contain spaces
	if strings.Contains(key, " ") {
		return false
	}
	return true
}

func StringifyCmd(cmd Command) string {
	if cmd.Action == SET {
		return fmt.Sprintf("%s %s %s", cmd.Action, cmd.Key, cmd.Value)
	} else {
		return fmt.Sprintf("%s %s", cmd.Action, cmd.Key)
	}
}
