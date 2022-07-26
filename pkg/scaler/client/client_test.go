package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/bndr/gojenkins"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
)

var _ = g.Describe("Client", func() {
	var mux *http.ServeMux
	var ts *httptest.Server
	var wc *WrapperClient
	var opts *Options

	g.BeforeEach(func() {
		mux = http.NewServeMux()
		ts = httptest.NewServer(mux)

		opts = &Options{
			NodeNumExecutors:   4,
			ControllerNodeName: "Built-In Node",
		}

		wc = &WrapperClient{
			opt: opts,
			Jenkins: gojenkins.CreateJenkins(
				ts.Client(),
				ts.URL,
			),
		}
	})

	g.AfterEach(func() {
		ts.Close()
	})

	g.Describe("GetCurrentUsage", func() {
		g.It("jenkins server not available", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {})

			_, err := wc.GetCurrentUsage(context.Background())
			o.Expect(err.Error()).To(o.ContainSubstring("can't calculate usage, wrong data"))
		})

		g.It("jenkins server available", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(gojenkins.Computers{
					BusyExecutors: 5,
					Computers: []*gojenkins.NodeResponse{
						{
							DisplayName: "node1",
						},
						{
							DisplayName: "node2",
						},
					},
				})
			})

			usage, err := wc.GetCurrentUsage(context.Background())

			o.Expect(err).To(o.Not(o.HaveOccurred()))
			o.Expect(usage).To(o.Equal(int64(62)))
		})

	})

	g.Describe("getNodes", func() {
		g.It("check ExcludeNode function", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(gojenkins.Computers{
					Computers: []*gojenkins.NodeResponse{
						{
							DisplayName: "node1",
						},
						{
							DisplayName: "node2",
						},
						{
							DisplayName: opts.ControllerNodeName,
						},
					},
				})
			})

			computers, err := wc.computers(context.Background())

			o.Expect(err).To(o.Not(o.HaveOccurred()))

			nodes := wc.getNodes(computers).
				ExcludeNode(opts.ControllerNodeName)

			o.Expect(nodes).ShouldNot(o.HaveKey(opts.ControllerNodeName))
		})

		g.It("check ExcludeOffline function", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(gojenkins.Computers{
					Computers: []*gojenkins.NodeResponse{
						{
							DisplayName: "node1",
						},
						{
							DisplayName: "node2",
							Offline:     true,
						},
						{
							DisplayName: "node3",
						},
						{
							DisplayName: "node4",
						},
						{
							DisplayName: "node5",
							Offline:     true,
						},
					},
				})
			})

			computers, err := wc.computers(context.Background())

			o.Expect(err).To(o.Not(o.HaveOccurred()))

			nodes := wc.getNodes(computers).
				ExcludeOffline()

			o.Expect(nodes).To(o.HaveLen(3))
			o.Expect(nodes).To(o.HaveEach(o.HaveField("Raw.Offline", false)))
		})
	})
})
