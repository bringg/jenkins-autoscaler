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

func mergeFakeTypes[M ~map[K]V, K string, V any](dst, src M) M {
	i := len(dst) - 1
	for _, v := range src {
		i++
		dst[K(fmt.Sprintf("%d", i))] = v
	}

	return dst
}

func MakeFakeNodes(size int, opts ...NodeOption) client.Nodes {
	nodes := make(client.Nodes, size)
	for i := 0; i < size; i++ {
		name := fmt.Sprintf("%d", i)

		nodes[name] = &gojenkins.Node{
			Raw: &gojenkins.NodeResponse{
				DisplayName: name,
				Idle:        true,
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

func TestProvider(t *testing.T) {
	o.RegisterFailHandler(g.Fail)
	g.RunSpecs(t, "Provider Suite")
}
