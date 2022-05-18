package scaler

import (
	"context"

	"github.com/bndr/gojenkins"
)

type (
	WrapperClient struct {
		*gojenkins.Jenkins

		opt *Options
	}

	Nodes map[string]*gojenkins.Node
)

// NewClient returns a new Client.
func NewClient(opt *Options) *WrapperClient {
	return &WrapperClient{
		opt: opt,
		Jenkins: gojenkins.CreateJenkins(
			nil,
			opt.JenkinsURL,
			opt.JenkinsUser,
			opt.JenkinsToken,
		),
	}
}

// GetCurrentUsage return the current usage of jenkins nodes.
func (c *WrapperClient) GetCurrentUsage(ctx context.Context) (int64, error) {
	computers, err := c.computers(ctx)
	if err != nil {
		return 0, err
	}

	currentUsage := int64((float64(computers.BusyExecutors) / float64(computers.TotalExecutors)) * 100)

	return currentUsage, nil
}

func (c *WrapperClient) GetAllNodes(ctx context.Context, withBuildInNode bool) (Nodes, error) {
	computers, err := c.computers(ctx)
	if err != nil {
		return nil, err
	}

	nodes := make(Nodes, len(computers.Computers))
	for _, node := range computers.Computers {
		// skip master node
		if !withBuildInNode && c.opt.ControllerNodeName == node.DisplayName {
			continue
		}

		nodes[node.DisplayName] = &gojenkins.Node{Jenkins: c.Jenkins, Raw: node, Base: "/computer/" + node.DisplayName}
	}

	return nodes, nil
}

func (n Nodes) IsExist(name string) bool {
	if _, ok := n[name]; ok {
		return true
	}

	return false
}

func (c *WrapperClient) computers(ctx context.Context) (*gojenkins.Computers, error) {
	computers := new(gojenkins.Computers)

	qr := map[string]string{
		"depth": "1",
	}

	_, err := c.Requester.GetJSON(ctx, "/computer", computers, qr)
	if err != nil {
		return nil, err
	}

	return computers, nil
}
