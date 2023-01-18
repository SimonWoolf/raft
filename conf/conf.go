package conf

type Node struct {
	NodeId      int
	Port        int
	StartLeader bool
}

var Nodes = []Node{
	{NodeId: 0, Port: 10000, StartLeader: true},
	{NodeId: 1, Port: 10001, StartLeader: false},
	{NodeId: 2, Port: 10002, StartLeader: false},
	{NodeId: 3, Port: 10003, StartLeader: false},
	{NodeId: 4, Port: 10004, StartLeader: false},
}
