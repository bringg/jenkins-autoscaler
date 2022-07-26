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

	g.BeforeEach(func() {
		mux = http.NewServeMux()
		ts = httptest.NewServer(mux)

		wc = &WrapperClient{
			opt: &Options{
				NodeNumExecutors: 4,
			},
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
})
