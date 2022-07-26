package client

import (
	"testing"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
)

func TestClient(t *testing.T) {
	o.RegisterFailHandler(g.Fail)
	g.RunSpecs(t, "Client Suite")
}
