package conf

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
