package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

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

		opts = &Options{}

		wc = New(opts)
		wc.Jenkins = gojenkins.CreateJenkins(
			ts.Client(),
			ts.URL,
		)
	})

	g.AfterEach(func() {
		ts.Close()
	})

	g.Describe("GetAllNodes", func() {
		g.It("jenkins server not available", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			})

			wc.opt.LastErrBackoff = 1 * time.Minute

			nodes, err := wc.GetAllNodes(context.Background())
			o.Expect(err).To(o.HaveOccurred())
			o.Expect(nodes).To(o.BeNil())

			nodes, err = wc.GetAllNodes(context.Background())
			o.Expect(err).To(o.HaveOccurred())
			o.Expect(nodes).To(o.BeNil())
			o.Expect(err.Error()).To(o.ContainSubstring("request rejected because jenkins API was in-accessible"))
		})

		g.It("jenkins server available", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(gojenkins.Computers{
					BusyExecutors: 5,
					Computers: []*gojenkins.NodeResponse{
						{
							DisplayName: "Built-In Node",
							AssignedLabels: []map[string]string{
								{
									"name": "Built-In Node",
								},
							},
						},
						{
							DisplayName: "node1",
							AssignedLabels: []map[string]string{
								{
									"name": "node1",
								},
							},
						},
						{
							DisplayName: "node3",
							AssignedLabels: []map[string]string{
								{
									"name": "node3",
								},
								{
									"name": "mac-ios",
								},
							},
						},
						{
							DisplayName: "node2",
							AssignedLabels: []map[string]string{
								{
									"name": "node2",
								},
							},
							Offline: true,
						},
					},
				})
			})

			nodes, err := wc.GetAllNodes(context.Background())

			nodes = nodes.
				ExcludeNode("Built-In Node").
				ExcludeOffline()

			o.Expect(err).To(o.Not(o.HaveOccurred()))
			o.Expect(nodes.Len()).To(o.Equal(int64(2)))
		})

	})

	g.Describe("getNodes", func() {
		g.It("check KeepWithLabel function", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(gojenkins.Computers{
					Computers: []*gojenkins.NodeResponse{
						{
							DisplayName: "node1",
							AssignedLabels: []map[string]string{
								{
									"name": "node1",
								},
							},
						},
						{
							DisplayName: "node2",
							AssignedLabels: []map[string]string{
								{
									"name": "node2",
								},
								{
									"name": "JAS",
								},
							},
						},
						{
							DisplayName: "node7",
							AssignedLabels: []map[string]string{
								{
									"name": "node7",
								},
							},
						},
						{
							DisplayName: "node44",
							AssignedLabels: []map[string]string{
								{
									"name": "node44",
								},
								{
									"name": "JAS",
								},
							},
						},
					},
				})
			})

			computers, err := wc.computers(context.Background())

			o.Expect(err).To(o.Not(o.HaveOccurred()))

			nodes := wc.getNodes(computers).
				KeepWithLabel("JAS")

			o.Expect(nodes.Len()).To(o.Equal(int64(2)))
			o.Expect(nodes).ShouldNot(o.HaveKey("node7"))
			o.Expect(nodes).ShouldNot(o.HaveKey("node1"))
		})

		g.It("check ExcludeNode function", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(gojenkins.Computers{
					Computers: []*gojenkins.NodeResponse{
						{
							DisplayName: "node1",
							AssignedLabels: []map[string]string{
								{
									"name": "node1",
								},
							},
						},
						{
							DisplayName: "node2",
							AssignedLabels: []map[string]string{
								{
									"name": "node2",
								},
							},
						},
						{
							DisplayName: "node7",
							AssignedLabels: []map[string]string{
								{
									"name": "node7",
								},
							},
						},
					},
				})
			})

			computers, err := wc.computers(context.Background())

			o.Expect(err).To(o.Not(o.HaveOccurred()))

			nodes := wc.getNodes(computers).
				ExcludeNode("node2")

			o.Expect(nodes).ShouldNot(o.HaveKey("node2"))
		})

		g.It("check ExcludeOffline function", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(gojenkins.Computers{
					Computers: []*gojenkins.NodeResponse{
						{
							DisplayName: "node1",
							AssignedLabels: []map[string]string{
								{
									"name": "node1",
								},
							},
						},
						{
							DisplayName: "node2",
							Offline:     true,
							AssignedLabels: []map[string]string{
								{
									"name": "node2",
								},
							},
						},
						{
							DisplayName: "node3",
							AssignedLabels: []map[string]string{
								{
									"name": "node3",
								},
							},
						},
						{
							DisplayName: "node4",
							AssignedLabels: []map[string]string{
								{
									"name": "node4",
								},
							},
						},
						{
							DisplayName: "node5",
							Offline:     true,
							AssignedLabels: []map[string]string{
								{
									"name": "node5",
								},
							},
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

	g.Describe("Nodes", func() {
		g.It("", func() {})
	})
})
