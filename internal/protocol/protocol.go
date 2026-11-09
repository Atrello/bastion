// Package protocol is what the panel and the agents say to each other over the agent's websocket.
// docs/protocol.md describes it in words; these are the types both sides use.
package protocol

import "encoding/json"

// Version is the protocol's version. The agent sends it in its hello; the panel refuses agents too old for
// it, with a message to update them.
const Version = 1

// Message is every message, either way. Which fields are set says what it is:
//
//	panel → agent: a request (ID, Op, Args), or input to a stream (ID, Op "stream.input" and the like)
//	agent → panel: an answer (ID, OK, Data or Error), part of a stream (ID, Stream), or an event (Event, Data)
type Message struct {
	ID     string          `json:"id,omitempty"`
	Op     string          `json:"op,omitempty"`
	Args   json.RawMessage `json:"args,omitempty"`
	OK     *bool           `json:"ok,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
	Stream string          `json:"stream,omitempty"`
	Event  string          `json:"event,omitempty"`
}

// Requests the panel can make. Nothing else is accepted.
const (
	OpStackFiles      = "stack.files"      // StackArgs → StackFiles
	OpStackSave       = "stack.save"       // StackSaveArgs → nothing; checked by Docker before writing
	OpStackCreate     = "stack.create"     // StackSaveArgs → ActionResult; makes the folder and starts it
	OpStackAction     = "stack.action"     // StackActionArgs → ActionResult
	OpStackLogs       = "stack.logs"       // LogsArgs → LogLines (the last lines, once)
	OpContainerAction = "container.action" // ContainerActionArgs → ActionResult
	OpCheckUpdates    = "updates.check"    // nothing → nothing; checks registries now, then sends state
	OpLogsFollow      = "logs.follow"      // LogsArgs → a stream of log lines until stopped
	OpExec            = "exec"             // ExecArgs → a terminal stream until stopped
	OpStreamInput     = "stream.input"     // StreamInput: keys typed into a terminal
	OpStreamResize    = "stream.resize"    // StreamResize: the terminal's new size
	OpStreamStop      = "stream.stop"      // ends a stream
)

// Stream parts.
const (
	StreamData = "data"
	StreamEnd  = "end"
)

// Events the agent sends without being asked.
const (
	EventHello   = "hello"   // Hello, once on connecting
	EventMetrics = "metrics" // Metrics, every 10 seconds
	EventState   = "state"   // State, on connecting, when Docker reports a change, and every minute
)

type Hello struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	CPUs     int    `json:"cpus"`
	Docker   string `json:"docker"`
}

type Metrics struct {
	CPUPercent float64 `json:"cpuPercent"`
	MemUsed    uint64  `json:"memUsed"`
	MemTotal   uint64  `json:"memTotal"`
	DiskUsed   uint64  `json:"diskUsed"`
	DiskTotal  uint64  `json:"diskTotal"`
	UptimeSecs uint64  `json:"uptimeSecs"`
}

// State is everything Docker is running on the server, sent whole each time (it's small).
type State struct {
	Stacks     []Stack     `json:"stacks"`
	Containers []Container `json:"containers"`
}

type Stack struct {
	Name string `json:"name"`
	// The compose file Docker uses for it.
	ConfigFile string `json:"configFile"`
	HasEnv     bool   `json:"hasEnv"`
	Running    int    `json:"running"`
	Total      int    `json:"total"`
	// How many of its containers have a newer image in their registry.
	Updates int `json:"updates"`
}

type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`  // running, exited, restarting, paused, created, dead
	Status  string `json:"status"` // as Docker says it: "Up 3 days (healthy)"
	Ports   string `json:"ports"`
	Stack   string `json:"stack"`
	Service string `json:"service"`
	// The registry has a newer image for its tag.
	Update bool `json:"update"`
}

type StackArgs struct {
	Name string `json:"name"`
}

type StackFiles struct {
	Compose string `json:"compose"`
	// Nil when the stack has no .env file.
	Env *string `json:"env"`
}

type StackSaveArgs struct {
	Name    string  `json:"name"`
	Compose string  `json:"compose"`
	Env     *string `json:"env"` // nil leaves the .env file as it is (or absent)
}

type StackActionArgs struct {
	Name   string `json:"name"`
	Action string `json:"action"` // up, down, start, stop, restart, pull, update
}

type ContainerActionArgs struct {
	ID     string `json:"id"`
	Action string `json:"action"` // start, stop, restart
}

type ActionResult struct {
	Output string `json:"output"`
}

// LogsArgs picks the logs of a whole stack, or of one container.
type LogsArgs struct {
	Stack     string `json:"stack,omitempty"`
	Container string `json:"container,omitempty"`
	Tail      int    `json:"tail"`
}

type LogLines struct {
	Lines []string `json:"lines"`
}

type ExecArgs struct {
	Container string `json:"container"`
	Cols      uint16 `json:"cols"`
	Rows      uint16 `json:"rows"`
}

type StreamInput struct {
	Data string `json:"data"` // base64: terminals send bytes
}

type StreamResize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Enrolment: the agent swaps a one-time join token for its own ID and secret (POST /api/agent/enrol).
type EnrolRequest struct {
	Token    string `json:"token"`
	Hostname string `json:"hostname"`
}

type EnrolReply struct {
	AgentID string `json:"agentId"`
	Secret  string `json:"secret"`
}
