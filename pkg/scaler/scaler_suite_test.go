package scaler

import (
	"fmt"
	"testing"
	"time"

	"github.com/bndr/gojenkins"
	"github.com/bringg/jenkins-autoscaler/pkg/backend"
	"github.com/bringg/jenkins-autoscaler/pkg/scaler/client"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
)

type (
	fakeInstance struct {
		name       string
		launchTime *time.Time
	}

	NodeOption func(c *gojenkins.Node)
)

func (i fakeInstance) Describe() error {
	return nil
}

func (i fakeInstance) Name() string {
	return i.name
}

func (i fakeInstance) LaunchTime() *time.Time {
	return i.launchTime
}

func MakeFakeNodes(size int, opts ...NodeOption) client.Nodes {
	nodes := make(client.Nodes, size)
	for i := 0; i < size; i++ {
		name := fmt.Sprintf("%d", i)

		nodes[name] = &gojenkins.Node{
			Raw: &gojenkins.NodeResponse{
				AssignedLabels: []map[string]string{
					{
						"name": name,
					},
				},
				DisplayName: name,
				Idle:        true,
				Executors: []*gojenkins.NodeExecutor{
					{
						Idle: true,
					},
					{
						Idle: true,
					},
					{
						Idle: true,
					},
					{
						Idle: true,
					},
				},
			},
		}

		for _, opt := range opts {
			opt(nodes[name])
		}
	}

	return nodes
}

func MakeFakeInstances(size int) backend.Instances {
	ins := backend.NewInstances()
	for i := 0; i < size; i++ {
		name := fmt.Sprintf("%d", i)
		now := time.Now().Add(-30 * time.Minute)
		ins.Add(&fakeInstance{name: name, launchTime: &now})
	}

	return ins
}

func MergeFakeTypes[M ~map[K]V, K string, V any](dst, src M) M {
	i := len(dst) - 1
	for _, v := range src {
		i++
		dst[K(fmt.Sprintf("%d", i))] = v
	}

	return dst
}

func WithOffline() NodeOption {
	return func(n *gojenkins.Node) {
		n.Raw.Offline = true
	}
}

func WithLabels(labels []string) NodeOption {
	return func(n *gojenkins.Node) {
		for _, label := range labels {
			n.Raw.AssignedLabels = append(n.Raw.AssignedLabels, map[string]string{
				"name": label,
			})
		}
	}
}

func WithBusyExecutors(num int) NodeOption {
	return func(n *gojenkins.Node) {
		if num > 4 {
			num = 4
		}

		for i := 0; i < num; i++ {
			n.Raw.Executors[i].Idle = false
		}
	}
}

func WithoutIdle() NodeOption {
	return func(n *gojenkins.Node) {
		n.Raw.Idle = false
	}
}

func TestProvider(t *testing.T) {
	o.RegisterFailHandler(g.Fail)
	g.RunSpecs(t, "Provider Suite")
}
