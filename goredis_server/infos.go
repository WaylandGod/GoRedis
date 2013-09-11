package goredis_server

//  连接状态
type LinkStatus int

const (
	LinkStatusPending = iota
	LinkStatusDown
	LinkStatusUp
)

type ReplicationInfo struct {
	IsMaster         bool
	IsSlave          bool
	MasterHost       string
	MasterPort       string
	MasterLinkStatus LinkStatus // pending|up|down
}
