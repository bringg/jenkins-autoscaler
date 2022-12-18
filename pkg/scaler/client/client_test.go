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

		opts = &Options{
			NodeNumExecutors:   4,
			ControllerNodeName: "Built-In Node",
		}

		wc = New(opts)
		wc.Jenkins = gojenkins.CreateJenkins(
			ts.Client(),
			ts.URL,
		)
	})

	g.AfterEach(func() {
		ts.Close()
	})

	g.Describe("GetCurrentUsage", func() {
		g.It("jenkins server not available", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			})

			wc.opt.LastErrBackoff = 1 * time.Minute

			usage, err := wc.GetCurrentUsage(context.Background())
			o.Expect(err).To(o.HaveOccurred())
			o.Expect(usage).To(o.BeZero())

			nodes, err := wc.GetAllNodes(context.Background())
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

		g.It("check exclude nodes by labels: node with label exist", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(gojenkins.Computers{
					Computers: []*gojenkins.NodeResponse{
						{
							DisplayName: "node1",
							AssignedLabels: []map[string]string{
								{
									"name": "mac-android",
								},
							},
						},
						{
							DisplayName: "node2",
							Offline:     true,
						},
						{
							DisplayName: "node3",
							AssignedLabels: []map[string]string{
								{
									"name": "mac-ios",
								},
							},
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

			wc.opt.ExcludeNodesByLabel = []string{"mac-ios"}
			computers, err := wc.computers(context.Background())
			o.Expect(err).To(o.Not(o.HaveOccurred()))

			nodes := wc.getNodes(computers).
				ExcludeOffline()

			o.Expect(nodes).To(o.HaveLen(2))
		})

		g.It("check exclude nodes by labels: node with label missing", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(gojenkins.Computers{
					Computers: []*gojenkins.NodeResponse{
						{
							DisplayName: "node1",
							AssignedLabels: []map[string]string{
								{
									"name": "mac-android",
								},
							},
						},
						{
							DisplayName: "node2",
							Offline:     true,
						},
						{
							DisplayName: "node3",
							AssignedLabels: []map[string]string{
								{
									"name": "mac-ios",
								},
							},
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

			wc.opt.ExcludeNodesByLabel = []string{"missing"}
			computers, err := wc.computers(context.Background())
			o.Expect(err).To(o.Not(o.HaveOccurred()))

			nodes := wc.getNodes(computers).
				ExcludeOffline()

			o.Expect(nodes).To(o.HaveLen(3))
		})

		g.It("check exclude nodes by labels: should ne zero nodes", func() {
			mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(gojenkins.Computers{
					Computers: []*gojenkins.NodeResponse{
						{
							DisplayName: "node1",
							AssignedLabels: []map[string]string{
								{
									"name": "mac-android",
								},
								{
									"name": "mac-mini",
								},
							},
						},
						{
							DisplayName: "node4",
							AssignedLabels: []map[string]string{
								{
									"name": "mac-ios",
								},
								{
									"name": "mac-mini",
								},
							},
						},
					},
				})
			})

			wc.opt.ExcludeNodesByLabel = []string{"mac-mini"}
			computers, err := wc.computers(context.Background())

			o.Expect(err).To(o.Not(o.HaveOccurred()))
			o.Expect(wc.getNodes(computers)).To(o.HaveLen(0))
		})
	})
})
