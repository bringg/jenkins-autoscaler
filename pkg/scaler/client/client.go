package client

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httputil"

	"github.com/bndr/gojenkins"
)

type (
	JenkinsAccessor interface {
		GetCurrentUsage(ctx context.Context) (int64, error)
		DeleteNode(ctx context.Context, name string) (bool, error)
		GetAllNodes(ctx context.Context) (Nodes, error)
		GetNode(ctx context.Context, name string) (*gojenkins.Node, error)
	}

	WrapperClient struct {
		*gojenkins.Jenkins

		opt *Options
	}

	Nodes map[string]*gojenkins.Node

	Options struct {
		JenkinsURL         string `config:"jenkins_url" validate:"required"`
		JenkinsUser        string `config:"jenkins_user" validate:"required"`
		JenkinsToken       string `config:"jenkins_token" validate:"required"`
		ControllerNodeName string `config:"controller_node_name"`
		NodeNumExecutors   int64  `config:"node_num_executors"`
	}
)

// New returns a new Client.
func New(opt *Options) *WrapperClient {
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

	nodes := c.getNodes(computers).
		ExcludeNode(c.opt.ControllerNodeName).
		ExcludeOffline()

	currentUsage := (float64(computers.BusyExecutors) / float64(nodes.Len()*c.opt.NodeNumExecutors)) * 100

	if math.IsNaN(currentUsage) || math.IsInf(currentUsage, 0) {
		return 0, errors.New("can't calculate usage, wrong data")
	}

	return int64(currentUsage), nil
}

func (c *WrapperClient) getNodes(computers *gojenkins.Computers) Nodes {
	nodes := make(Nodes, len(computers.Computers))
	for _, node := range computers.Computers {
		nodes[node.DisplayName] = &gojenkins.Node{Jenkins: c.Jenkins, Raw: node, Base: "/computer/" + node.DisplayName}
	}

	return nodes
}

func (c *WrapperClient) GetAllNodes(ctx context.Context) (Nodes, error) {
	computers, err := c.computers(ctx)
	if err != nil {
		return nil, err
	}

	return c.getNodes(computers), nil
}

func (n Nodes) Len() int64 {
	return int64(len(n))
}

func (n Nodes) ExcludeOffline() Nodes {
	nodes := make(Nodes, 0)
	for name, node := range n {
		if node.Raw.Offline == true {
			continue
		}

		nodes[name] = node
	}

	return nodes
}

func (n Nodes) ExcludeNode(name string) Nodes {
	nodes := make(Nodes, 0)
	for i, node := range n {
		if name == node.Raw.DisplayName {
			continue
		}

		nodes[i] = node
	}

	return nodes
}

func (c *WrapperClient) computers(ctx context.Context) (*gojenkins.Computers, error) {
	computers := new(gojenkins.Computers)

	qr := map[string]string{
		"depth": "1",
	}

	res, err := c.Requester.GetJSON(ctx, "/computer", computers, qr)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		body, _ := httputil.DumpResponse(res, true)

		return nil, fmt.Errorf("api response status code %d, body dump: %q", res.StatusCode, body)
	}

	return computers, nil
}

func (o *Options) Name() string {
	return "jenkins client"
}
