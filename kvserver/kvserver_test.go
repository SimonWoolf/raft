package kvserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGet(t *testing.T) {
	// check it can cope with funky whitespace
	cmdStr := "GET    k  "
	cmd, err := ParseCmd(cmdStr)
	assert.Equal(t, nil, err)
	assert.Equal(t, GET, cmd.Action)
	assert.Equal(t, "k", cmd.Key)
	assert.Equal(t, "", cmd.Value)

	assert.Equal(t, "GET k", StringifyCmd(*cmd))
}

func TestSet(t *testing.T) {
	cmdStr := "SET   k  v  "
	cmd, err := ParseCmd(cmdStr)
	assert.Equal(t, nil, err)
	assert.Equal(t, SET, cmd.Action)
	assert.Equal(t, "k", cmd.Key)
	assert.Equal(t, "v", cmd.Value)
	assert.Equal(t, "SET k v", StringifyCmd(*cmd))
}

func TestDel(t *testing.T) {
	cmdStr := "DEL  k  "
	cmd, err := ParseCmd(cmdStr)
	assert.Equal(t, nil, err)
	assert.Equal(t, DELETE, cmd.Action)
	assert.Equal(t, "k", cmd.Key)
	assert.Equal(t, "", cmd.Value)
	assert.Equal(t, "DEL k", StringifyCmd(*cmd))
}
