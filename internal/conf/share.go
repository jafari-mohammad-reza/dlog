package conf

import "time"

type StreamOpts struct {
	Name string
	ID   string
}

type TrackedOp string

const (
	RemoveTracked TrackedOp = "remove"
)

type TrackedOption struct {
	ID string
	Op TrackedOp
}

type RecordLog struct {
	ContainerName string
	Log           string
}

type LogMethod string

const (
	Socket LogMethod = "socket"
	File   LogMethod = "file"
)


type StatLog struct {
	CpuUsage uint64 `json:"cpu_usage"`
	MemUsage uint64 `json:"mem_usage"`
	ContainerName string `json:"container_name"`
	ContainerID string `json:"container_id"`
}
type ContainerStats struct {
	Name         string                 `json:"name"`
	ID           string                 `json:"id"`
	Read         time.Time              `json:"read"`
	PreRead      time.Time              `json:"preread"`
	PidsStats    PidsStats              `json:"pids_stats"`
	BlkioStats   BlkioStats             `json:"blkio_stats"`
	NumProcs     int                    `json:"num_procs"`
	StorageStats map[string]interface{} `json:"storage_stats"`
	CPUStats     CPUStats               `json:"cpu_stats"`
	PreCPUStats  CPUStats               `json:"precpu_stats"`
	MemoryStats  MemoryStats            `json:"memory_stats"`
	Networks     map[string]NetworkStats `json:"networks"`
}

type PidsStats struct {
	Current int64 `json:"current"`
	Limit   int64 `json:"limit"`
}

type BlkioStats struct {
	IoServiceBytesRecursive []BlkioStatEntry `json:"io_service_bytes_recursive"`
	IoServicedRecursive     []BlkioStatEntry `json:"io_serviced_recursive"`
	IoQueueRecursive        []BlkioStatEntry `json:"io_queue_recursive"`
	IoServiceTimeRecursive  []BlkioStatEntry `json:"io_service_time_recursive"`
	IoWaitTimeRecursive     []BlkioStatEntry `json:"io_wait_time_recursive"`
	IoMergedRecursive       []BlkioStatEntry `json:"io_merged_recursive"`
	IoTimeRecursive         []BlkioStatEntry `json:"io_time_recursive"`
	SectorsRecursive        []BlkioStatEntry `json:"sectors_recursive"`
}

type BlkioStatEntry struct {
	Major uint64 `json:"major"`
	Minor uint64 `json:"minor"`
	Op    string `json:"op"`
	Value uint64 `json:"value"`
}

type CPUStats struct {
	CPUUsage       CPUUsage       `json:"cpu_usage"`
	SystemCPUUsage uint64         `json:"system_cpu_usage"`
	OnlineCPUs     uint32         `json:"online_cpus"`
	ThrottlingData ThrottlingData `json:"throttling_data"`
}

type CPUUsage struct {
	TotalUsage        uint64   `json:"total_usage"`
	UsageInKernelMode uint64   `json:"usage_in_kernelmode"`
	UsageInUserMode   uint64   `json:"usage_in_usermode"`
	PercpuUsage       []uint64 `json:"percpu_usage,omitempty"`
}

type ThrottlingData struct {
	Periods          uint64 `json:"periods"`
	ThrottledPeriods uint64 `json:"throttled_periods"`
	ThrottledTime    uint64 `json:"throttled_time"`
}

type MemoryStats struct {
	Usage uint64            `json:"usage"`
	Stats MemoryStatsDetail `json:"stats"`
	Limit uint64            `json:"limit"`
}

type MemoryStatsDetail struct {
	ActiveAnon              uint64 `json:"active_anon"`
	ActiveFile              uint64 `json:"active_file"`
	Anon                    uint64 `json:"anon"`
	AnonThp                 uint64 `json:"anon_thp"`
	File                    uint64 `json:"file"`
	FileDirty               uint64 `json:"file_dirty"`
	FileMapped              uint64 `json:"file_mapped"`
	FileWriteback           uint64 `json:"file_writeback"`
	InactiveAnon            uint64 `json:"inactive_anon"`
	InactiveFile            uint64 `json:"inactive_file"`
	KernelStack             uint64 `json:"kernel_stack"`
	PgActivate              uint64 `json:"pgactivate"`
	PgDeactivate            uint64 `json:"pgdeactivate"`
	PgFault                 uint64 `json:"pgfault"`
	PgLazyFree              uint64 `json:"pglazyfree"`
	PgLazyFreed             uint64 `json:"pglazyfreed"`
	PgMajFault              uint64 `json:"pgmajfault"`
	PgRefill                uint64 `json:"pgrefill"`
	PgScan                  uint64 `json:"pgscan"`
	PgSteal                 uint64 `json:"pgsteal"`
	Shmem                   uint64 `json:"shmem"`
	Slab                    uint64 `json:"slab"`
	SlabReclaimable         uint64 `json:"slab_reclaimable"`
	SlabUnreclaimable       uint64 `json:"slab_unreclaimable"`
	Sock                    uint64 `json:"sock"`
	ThpCollapseAlloc        uint64 `json:"thp_collapse_alloc"`
	ThpFaultAlloc           uint64 `json:"thp_fault_alloc"`
	Unevictable             uint64 `json:"unevictable"`
	WorkingsetActivate      uint64 `json:"workingset_activate"`
	WorkingsetNodeReclaim   uint64 `json:"workingset_nodereclaim"`
	WorkingsetRefault       uint64 `json:"workingset_refault"`
}

type NetworkStats struct {
	RxBytes   uint64 `json:"rx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	RxErrors  uint64 `json:"rx_errors"`
	RxDropped uint64 `json:"rx_dropped"`
	TxBytes   uint64 `json:"tx_bytes"`
	TxPackets uint64 `json:"tx_packets"`
	TxErrors  uint64 `json:"tx_errors"`
	TxDropped uint64 `json:"tx_dropped"`
}
