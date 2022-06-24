package backend

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
)

// Registry of backends
var Registry []*RegInfo

type (
	Backend interface {
		Name() string
		Resize(size int64) error
		CurrentSize() (int64, error)
		Instances() (Instances, error)
		Terminate(instances Instances) error
	}

	Instance interface {
		Name() string
		Describe() error
		LaunchTime() *time.Time
	}

	Instances map[string]Instance

	RegInfo struct {
		Name       string
		NewBackend func(ctx context.Context, config configmap.Mapper) (Backend, error) `json:"-"`
		Options    fs.Options
	}
)

// Register a backend
func Register(info *RegInfo) {
	setOptValues(info.Options)

	Registry = append(Registry, info)
}

// Names backend names
func Names() []string {
	names := make([]string, len(Registry))
	for i, item := range Registry {
		names[i] = item.Name
	}

	return names
}

// Find looks for a RegInfo object for the name passed in.
func Find(name string) (*RegInfo, error) {
	for _, item := range Registry {
		if item.Name == name {
			return item, nil
		}
	}

	return nil, fmt.Errorf("didn't find backend called %q", name)
}

// MustFind looks for an Info object for the type name passed in
func MustFind(name string) *RegInfo {
	bk, err := Find(name)
	if err != nil {
		log.Fatalf("%v", err)
	}

	return bk
}

// Set the default values for the options
func setOptValues(os fs.Options) {
	for i := range os {
		o := &os[i]
		if o.Default == nil {
			o.Default = ""
		}
	}
}

// IsExist check if instance with this name is exist
func (i Instances) IsExist(name string) (Instance, bool) {
	if ins, ok := i[name]; ok {
		return ins, true
	}

	return nil, false
}

// Add adds an instance to Instances map
func (i Instances) Add(instance Instance) Instances {
	i[instance.Name()] = instance

	return i
}

// Iterate over the instances
func (i Instances) Itr(fn func(Instance) bool) Instances {
	for _, ins := range i {
		if ok := fn(ins); ok {
			break
		}
	}

	return i
}

// Len of instances
func (i Instances) Len() int64 {
	return int64(len(i))
}

// NewInstances create new map of instances
func NewInstances() Instances {
	return make(Instances)
}
